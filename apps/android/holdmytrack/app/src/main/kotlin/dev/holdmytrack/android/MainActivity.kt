package dev.holdmytrack.android

import android.content.Intent
import android.content.res.Configuration
import android.graphics.Typeface
import android.os.Bundle
import android.util.Log
import android.view.View
import android.view.ViewGroup.MarginLayoutParams
import android.view.WindowInsets
import android.widget.Button
import android.widget.PopupMenu
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import dev.holdmytrack.android.map.MapMode
import dev.holdmytrack.android.map.MapOverlays
import dev.holdmytrack.android.net.ApiException
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.RecordedActivitiesActivity
import dev.holdmytrack.android.recording.RecordingActivity
import org.maplibre.android.camera.CameraPosition
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.geometry.LatLngBounds
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style

/**
 * The map, and everything that hangs off it: the served basemap, the session the user layers
 * need, and the Normal / Fog of War / Heatmap toggle between them. The toggle and the burger
 * menu (Profile, Sync) both float over the map top-start, rather than living in a bar of their
 * own, mirroring the web client's own on-map mode control (`apps/web/src/map/MapView.tsx`).
 *
 * Never mounted without a session: a signed-out visitor is handed straight to
 * `SignInActivity`, mirroring web's `AuthGate` (`docs/IMPLEMENTATION.md` §4.13). A bare basemap
 * with none of the three user layers — all behind `requireAuth` server-side — would be a weak
 * first impression next to the Demo account that screen offers one tap away.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var mapView: MapView
    private lateinit var status: TextView
    private lateinit var menuButton: Button
    private lateinit var modeBar: View
    private lateinit var modeButtons: Map<MapMode, Button>

    private var map: MapLibreMap? = null
    private var style: Style? = null
    private var mode = MapMode.NORMAL

    /** The top inset (status bar height), applied to the floating chrome and to MapLibre's own
     *  compass — see `insetSystemBars` and `applyCompassMargin`. Read before either the inset
     *  or the map instance is necessarily available yet, so both paths call the latter once
     *  they have what they need. */
    private var systemBarInsetTop = 0

    /** The compass's own default top margin, captured once so repeated inset callbacks (e.g.
     *  a rotation) add the status bar height on top of it rather than compounding it. */
    private var compassBaseMarginTop = 0
    private var compassBaseMarginCaptured = false

    /** Whether the user layers are currently on the style — see `syncSession`. */
    private var overlaysAttached = false

    /** Whether this session's activity extent has already framed the camera. Once per session:
     *  re-framing on every resume would fight the user for control of the camera. */
    private var framed = false

    /** Guards against a second `GET /v1/auth/me` while the first is still in flight — every
     *  `onResume` calls `syncSession`, and returning from the Profile screen is one. */
    private var verifying = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Before any layout or MapView exists, so a signed-out launch never draws a map at all.
        if (!Session.isSignedIn) {
            openSignIn()
            return
        }
        setContentView(R.layout.activity_main)

        status = findViewById(R.id.status)
        menuButton = findViewById(R.id.menu_button)
        modeBar = findViewById(R.id.mode_bar)
        modeButtons = mapOf(
            MapMode.NORMAL to findViewById(R.id.mode_normal),
            MapMode.FOG to findViewById(R.id.mode_fog),
            MapMode.HEATMAP to findViewById(R.id.mode_heatmap),
        )
        modeButtons.forEach { (value, button) -> button.setOnClickListener { setMode(value) } }
        menuButton.setOnClickListener { showMenu(it) }
        setMode(mode)

        insetSystemBars()

        mapView = findViewById(R.id.map_view)
        mapView.onCreate(savedInstanceState)

        // Failures are reported here rather than only in logcat: a style or tile fetch that
        // 404s leaves a plausible-looking blank map behind, with nothing on screen to say so.
        mapView.addOnDidFailLoadingMapListener { error -> reportFailure(error) }

        mapView.getMapAsync { instance ->
            map = instance
            // A whole-world view is the honest starting camera until the session is verified
            // and the activity extent is known; `frameActivities` replaces it with the user's
            // own, and leaves it for an account with no geometry yet.
            instance.cameraPosition = CameraPosition.Builder()
                .target(LatLng(20.0, 0.0))
                .zoom(1.0)
                .build()
            applyCompassMargin()
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
     * Keeps the floating chrome clear of the status bar and the gesture navigation pill.
     *
     * Not optional at this target SDK: from API 35 the system draws every app edge to edge and
     * ignores the old opt-out. The burger/mode chrome sits at the top in a `layout_margin`ed
     * `LinearLayout`, which has no `fitsSystemWindows` of its own, so without this it renders
     * underneath the status bar's own icons — found exactly that way, the compass included,
     * both behind the clock and battery indicator on a real device. The map itself is left
     * alone on purpose: it should fill the whole screen, and MapLibre keeps its own attribution
     * and logo out of the corners anyway.
     *
     * The insets are added to the padding/margin each view was laid out with rather than
     * replacing it, and the listener returns them unconsumed so nothing else that wants them
     * misses out.
     */
    private fun insetSystemBars() {
        val topStartBar: View = findViewById(R.id.top_start_bar)
        val barTopMargin = (topStartBar.layoutParams as MarginLayoutParams).topMargin
        val statusPadding = status.paddingTop
        findViewById<View>(R.id.map_root).setOnApplyWindowInsetsListener { _, insets ->
            val bars = insets.getInsets(WindowInsets.Type.systemBars())
            (topStartBar.layoutParams as MarginLayoutParams).topMargin = barTopMargin + bars.top
            topStartBar.requestLayout()
            status.setPadding(status.paddingLeft, statusPadding + bars.top, status.paddingRight, status.paddingBottom)
            systemBarInsetTop = bars.top
            applyCompassMargin()
            insets
        }
    }

    /**
     * MapLibre's own compass control defaults to top-end with a small fixed margin, unaware of
     * the status bar — found sitting directly behind the clock/battery indicator on a real
     * device. Called from both `insetSystemBars` and `getMapAsync` because whichever of the
     * inset callback and the map-ready callback fires second is the one that actually has
     * everything it needs.
     */
    private fun applyCompassMargin() {
        val instance = map ?: return
        val settings = instance.uiSettings
        if (!compassBaseMarginCaptured) {
            compassBaseMarginTop = settings.compassMarginTop
            compassBaseMarginCaptured = true
        }
        settings.setCompassMargins(
            settings.compassMarginLeft,
            compassBaseMarginTop + systemBarInsetTop,
            settings.compassMarginRight,
            settings.compassMarginBottom,
        )
    }

    /**
     * Brings the map in line with the credential the app holds — called both when the style
     * finishes loading and on every resume. A session that has gone away by then (the stored
     * token was revoked) sends the user back to `SignInActivity` rather than leaving a map with
     * nothing of theirs on it.
     *
     * A token restored from disk is not trusted until the server confirms it. An expired or
     * revoked one would otherwise surface as a wall of 401s on tile requests, which show up
     * only in logcat: on screen it would look like an ordinary blank map.
     */
    private fun syncSession() {
        if (!Session.isSignedIn) {
            openSignIn()
            return
        }
        if (!Session.verified) {
            verifyStoredSession()
            return
        }

        modeBar.visibility = View.VISIBLE

        val loaded = style ?: return
        if (!overlaysAttached) {
            MapOverlays.attach(loaded, mode)
            overlaysAttached = true
            frameActivities()
        }
    }

    private fun openSignIn() {
        SignInActivity.open(this)
        finish()
    }

    private fun verifyStoredSession() {
        if (verifying) return
        verifying = true
        HoldMyTrackApi.verifySession { result ->
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

    /** The burger menu: the destinations that don't fit on the map itself. */
    private fun showMenu(anchor: View) {
        val menu = PopupMenu(this, anchor)
        menu.menu.add(0, MENU_PROFILE, 0, R.string.menu_profile)
        menu.menu.add(0, MENU_SYNC, 1, R.string.menu_sync)
        menu.menu.add(0, MENU_GPS_LOGGER, 2, R.string.menu_gps_logger)
        menu.menu.add(0, MENU_RECORDED_ACTIVITIES, 3, R.string.menu_recorded_activities)
        menu.setOnMenuItemClickListener { item ->
            when (item.itemId) {
                MENU_PROFILE -> startActivity(Intent(this, ProfileActivity::class.java))
                MENU_SYNC -> startActivity(Intent(this, SyncActivity::class.java))
                MENU_GPS_LOGGER -> startActivity(Intent(this, RecordingActivity::class.java))
                MENU_RECORDED_ACTIVITIES -> startActivity(Intent(this, RecordedActivitiesActivity::class.java))
            }
            true
        }
        menu.show()
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
        HoldMyTrackApi.activityBounds { result ->
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
        if (::mapView.isInitialized) mapView.onLowMemory()
    }

    // A signed-out launch finishes from onCreate before the MapView exists, which skips every
    // callback above but still reaches this one.
    override fun onDestroy() {
        if (::mapView.isInitialized) mapView.onDestroy()
        super.onDestroy()
    }

    private companion object {
        const val TAG = "HoldMyTrack"
        const val INACTIVE_MODE_ALPHA = 0.6f
        const val FRAME_PADDING_PX = 64
        const val MAX_FRAME_ZOOM = 15.0
        const val FRAME_DURATION_MS = 900
        const val MENU_PROFILE = 1
        const val MENU_SYNC = 2
        const val MENU_GPS_LOGGER = 3
        const val MENU_RECORDED_ACTIVITIES = 4
    }
}
