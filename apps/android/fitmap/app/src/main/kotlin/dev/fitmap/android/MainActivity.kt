package dev.fitmap.android

import android.content.res.Configuration
import android.os.Bundle
import android.util.Log
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import org.maplibre.android.MapLibre
import org.maplibre.android.camera.CameraPosition
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style

/**
 * The client shell: one full-screen map, rendered by MapLibre Native from the style document
 * the API serves (`GET /v1/map/style/{flavor}`, docs/ARCHITECTURE.md §2.1).
 *
 * Nothing here is user-specific yet, and that is the point of this phase's ordering. The
 * style endpoint is unauthenticated and the basemap archive it points at is a plain
 * `pmtiles://` URL, so the shell renders before login exists; every *user* layer — tracks,
 * fog, heatmap — is behind `requireAuth` and arrives with the next roadmap item, along with
 * the bearer token MapLibre Native has to attach to its own tile requests.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var mapView: MapView
    private lateinit var status: TextView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Must run before any MapView is inflated: it loads the native library and sets up
        // the module provider the view's constructor immediately asks for.
        MapLibre.getInstance(this)
        setContentView(R.layout.activity_main)

        status = findViewById(R.id.status)
        mapView = findViewById(R.id.map_view)
        mapView.onCreate(savedInstanceState)

        // Failures are reported here rather than only in logcat: a style or tile fetch that
        // 404s leaves a plausible-looking blank map behind, with nothing on screen to say so.
        mapView.addOnDidFailLoadingMapListener { error -> reportFailure(error) }

        mapView.getMapAsync { map ->
            // A whole-world view is the honest starting camera while the app has no activity
            // data to frame; the tracks layer replaces it with the user's own extent.
            map.cameraPosition = CameraPosition.Builder()
                .target(LatLng(20.0, 0.0))
                .zoom(1.0)
                .build()
            // Attribution is not decoration here — the Protomaps basemap is an ODbL Produced
            // Work, and MapLibre's own attribution control renders the credit the style's
            // source already carries, so it must stay enabled.
            map.setStyle(Style.Builder().fromUri(styleUrl())) {
                status.visibility = TextView.GONE
            }
        }
    }

    /**
     * Flavor follows the system's day/night setting. The API serves five (`light`, `dark`,
     * `white`, `black`, `grayscale`); picking between the two general-purpose ones keeps this
     * shell from inventing a theme preference that Phase 3's design pass has not decided yet.
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
        status.visibility = TextView.VISIBLE
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
    }
}
