package dev.fitmap.android

import android.content.Intent
import android.content.res.Configuration
import android.graphics.Typeface
import android.os.Bundle
import android.util.Log
import android.view.View
import android.view.WindowInsets
import android.widget.Button
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import dev.fitmap.android.map.MapMode
import dev.fitmap.android.map.MapOverlays
import dev.fitmap.android.net.ApiException
import dev.fitmap.android.net.FitMapApi
import dev.fitmap.android.net.Session
import org.maplibre.android.camera.CameraPosition
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.geometry.LatLngBounds
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style

/**
 * The map, and everything that hangs off it: the served basemap, the session the user layers
 * need, and the Normal / Fog of War / Heatmap toggle between them.
 *
 * The basemap draws whether or not anyone is signed in — the style endpoint is unauthenticated
 * and the archive it points at is a plain `pmtiles://` URL — so a signed-out FitMap is a
 * working map rather than a login screen. Signing in is what adds the three user layers, all
 * of which sit behind `requireAuth` server-side.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var mapView: MapView
    private lateinit var status: TextView
    private lateinit var accountLabel: TextView
    private lateinit var accountAction: Button
    private lateinit var syncAction: Button
    private lateinit var modeBar: View
    private lateinit var modeButtons: Map<MapMode, Button>

    private var map: MapLibreMap? = null
    private var style: Style? = null
    private var mode = MapMode.NORMAL

    /** Whether the user layers are currently on the style — see `syncSession`. */
    private var overlaysAttached = false

    /** Whether this session's activity extent has already framed the camera. Once per session:
     *  re-framing on every resume would fight the user for control of the camera. */
    private var framed = false

    /** Guards against a second `GET /v1/auth/me` while the first is still in flight — every
     *  `onResume` calls `syncSession`, and returning from the sign-in screen is one. */
    private var verifying = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        status = findViewById(R.id.status)
        accountLabel = findViewById(R.id.account_label)
        accountAction = findViewById(R.id.account_action)
        syncAction = findViewById(R.id.sync_action)
        modeBar = findViewById(R.id.mode_bar)
        modeButtons = mapOf(
            MapMode.NORMAL to findViewById(R.id.mode_normal),
            MapMode.FOG to findViewById(R.id.mode_fog),
            MapMode.HEATMAP to findViewById(R.id.mode_heatmap),
        )
        modeButtons.forEach { (value, button) -> button.setOnClickListener { setMode(value) } }
        accountAction.setOnClickListener { onAccountAction() }
        syncAction.setOnClickListener { startActivity(Intent(this, SyncActivity::class.java)) }
        setMode(mode)

        insetSystemBars()

        mapView = findViewById(R.id.map_view)
        mapView.onCreate(savedInstanceState)

        // Failures are reported here rather than only in logcat: a style or tile fetch that
        // 404s leaves a plausible-looking blank map behind, with nothing on screen to say so.
        mapView.addOnDidFailLoadingMapListener { error -> reportFailure(error) }

        mapView.getMapAsync { instance ->
            map = instance
            // A whole-world view is the honest starting camera while nothing is signed in and
            // there is no activity extent to frame; `frameActivities` replaces it with the
            // user's own once a session exists.
            instance.cameraPosition = CameraPosition.Builder()
                .target(LatLng(20.0, 0.0))
                .zoom(1.0)
                .build()
            // Attribution is not decoration here — the Protomaps basemap is an ODbL Produced
            // Work, and MapLibre's own attribution control renders the credit the style's
            // source already carries, so it must stay enabled.
            instance.setStyle(Style.Builder().fromUri(styleUrl())) { loaded ->
                style = loaded
                status.visibility = View.GONE
                syncSession()
            }
        }
    }

    /**
     * Keeps the two pieces of chrome clear of the status bar and the gesture navigation pill.
     *
     * Not optional at this target SDK: from API 35 the system draws every app edge to edge and
     * ignores the old opt-out, so a bar laid out at the bottom of the window sits *under* the
     * navigation pill rather than above it — found exactly that way, with the mode buttons half
     * covered on a real device. The map itself is left alone on purpose: it should fill the
     * whole screen, and MapLibre keeps its own attribution and logo out of the corners anyway.
     *
     * The insets are added to the padding each view was laid out with rather than replacing it,
     * and the listener returns them unconsumed so nothing else that wants them misses out.
     */
    private fun insetSystemBars() {
        val bottomBar: View = findViewById(R.id.bottom_bar)
        val barPadding = bottomBar.paddingBottom
        val statusPadding = status.paddingTop
        findViewById<View>(R.id.map_root).setOnApplyWindowInsetsListener { _, insets ->
            val bars = insets.getInsets(WindowInsets.Type.systemBars())
            bottomBar.setPadding(
                bottomBar.paddingLeft,
                bottomBar.paddingTop,
                bottomBar.paddingRight,
                barPadding + bars.bottom,
            )
            status.setPadding(status.paddingLeft, statusPadding + bars.top, status.paddingRight, status.paddingBottom)
            insets
        }
    }

    /**
     * Brings the map in line with whatever credential the app currently holds — called both
     * when the style finishes loading and on every resume, since returning from the sign-in
     * screen is a resume and nothing else would notice the change.
     *
     * A token restored from disk is not trusted until the server confirms it. An expired or
     * revoked one would otherwise surface as a wall of 401s on tile requests, which show up
     * only in logcat: on screen it would look like an ordinary blank map.
     */
    private fun syncSession() {
        val signedIn = Session.isSignedIn
        if (signedIn && !Session.verified) {
            accountLabel.setText(R.string.checking_session)
            accountAction.isEnabled = false
            verifyStoredSession()
            return
        }

        accountAction.isEnabled = true
        accountAction.setText(if (signedIn) R.string.sign_out else R.string.sign_in)
        accountLabel.text = when {
            !signedIn -> getString(R.string.signed_out)
            Session.email.isEmpty() -> getString(R.string.signed_in_demo)
            else -> getString(R.string.signed_in_as, Session.email)
        }
        modeBar.visibility = if (signedIn) View.VISIBLE else View.GONE
        // Health Connect sync needs somewhere to sync to, so it appears with the session
        // rather than sitting there inert for a signed-out visitor.
        syncAction.visibility = if (signedIn) View.VISIBLE else View.GONE

        val loaded = style ?: return
        if (signedIn && !overlaysAttached) {
            MapOverlays.attach(loaded, mode)
            overlaysAttached = true
            frameActivities()
        } else if (!signedIn && overlaysAttached) {
            MapOverlays.detach(loaded)
            overlaysAttached = false
            framed = false
        }
    }

    private fun verifyStoredSession() {
        if (verifying) return
        verifying = true
        FitMapApi.verifySession { result ->
            verifying = false
            result.onSuccess { Session.markVerified() }.onFailure { failure ->
                // Only a 401 means the token itself is dead. Anything else — no network, a
                // stopped dev stack — says nothing about the credential, so it survives and
                // gets re-checked on the next resume rather than silently signing the user out.
                if (failure is ApiException && failure.code == 401) {
                    Session.clear()
                } else {
                    Log.w(TAG, "could not verify the stored session", failure)
                }
            }
            syncSession()
        }
    }

    private fun onAccountAction() {
        if (!Session.isSignedIn) {
            startActivity(Intent(this, SignInActivity::class.java))
            return
        }
        accountAction.isEnabled = false
        FitMapApi.signOut {
            // The map keeps whatever mode was selected; it just stops having layers to show it
            // on, until someone signs in again.
            syncSession()
        }
    }

    private fun setMode(next: MapMode) {
        mode = next
        modeButtons.forEach { (value, button) ->
            val active = value == next
            button.setTypeface(null, if (active) Typeface.BOLD else Typeface.NORMAL)
            button.alpha = if (active) 1f else INACTIVE_MODE_ALPHA
        }
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setMode(it, next) }
    }

    /**
     * Moves the camera to cover everything the account has uploaded, once per session.
     *
     * The zoom is capped because a single short activity — or one trimmed to almost nothing by
     * the privacy trim — has a near-zero extent, and fitting the camera to that box lands well
     * past the basemap's z14 data, on a grey rectangle. An account with no geometry yet is left
     * at the world view, which is the truthful thing to show for a history that is empty.
     */
    private fun frameActivities() {
        if (framed) return
        framed = true
        FitMapApi.activityBounds { result ->
            val box = result.getOrNull() ?: return@activityBounds
            val instance = map ?: return@activityBounds
            val bounds = LatLngBounds.from(box[3], box[2], box[1], box[0])
            val fitted = instance.getCameraForLatLngBounds(bounds, IntArray(4) { FRAME_PADDING_PX })
                ?: return@activityBounds
            val target = CameraPosition.Builder(fitted)
                .zoom(minOf(fitted.zoom, MAX_FRAME_ZOOM))
                .build()
            instance.animateCamera(CameraUpdateFactory.newCameraPosition(target), FRAME_DURATION_MS)
        }
    }

    /**
     * Flavor follows the system's day/night setting. The API serves five (`light`, `dark`,
     * `white`, `black`, `grayscale`); picking between the two general-purpose ones keeps this
     * shell from inventing a theme preference that the design pass has not decided yet.
     */
    private fun styleUrl(): String {
        val night = resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK ==
            Configuration.UI_MODE_NIGHT_YES
        val flavor = if (night) "dark" else "light"
        return "${BuildConfig.API_BASE_URL}/v1/map/style/$flavor"
    }

    private fun reportFailure(error: String) {
        Log.e(TAG, "map failed to load: $error")
        status.text = getString(R.string.map_load_failed, BuildConfig.API_BASE_URL, error)
        status.visibility = View.VISIBLE
    }

    // MapLibre's MapView holds a native renderer and a GL surface, so every lifecycle
    // callback has to be forwarded by hand — a missed one leaks the surface or crashes on
    // rotation. This is the whole set the SDK expects.
    override fun onStart() {
        super.onStart()
        mapView.onStart()
    }

    override fun onResume() {
        super.onResume()
        mapView.onResume()
        syncSession()
    }

    override fun onPause() {
        mapView.onPause()
        super.onPause()
    }

    override fun onStop() {
        mapView.onStop()
        super.onStop()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        mapView.onSaveInstanceState(outState)
    }

    override fun onLowMemory() {
        super.onLowMemory()
        mapView.onLowMemory()
    }

    override fun onDestroy() {
        mapView.onDestroy()
        super.onDestroy()
    }

    private companion object {
        const val TAG = "FitMap"
        const val INACTIVE_MODE_ALPHA = 0.6f
        const val FRAME_PADDING_PX = 64
        const val MAX_FRAME_ZOOM = 15.0
        const val FRAME_DURATION_MS = 900
    }
}
