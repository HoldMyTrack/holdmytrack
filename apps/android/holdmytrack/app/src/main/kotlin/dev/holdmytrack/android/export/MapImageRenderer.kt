package dev.holdmytrack.android.export

import android.graphics.Bitmap
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.view.TextureView
import android.view.View
import android.view.ViewGroup
import org.maplibre.android.camera.CameraPosition
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.maps.MapLibreMapOptions
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style

/**
 * Draws the export's picture on a second, hidden map, never a crop of the live one
 * (`apps/android/docs/IMPLEMENTATION.md` §3.5): a [MapView] of the image's own pixel size, laid
 * under the live map in [host], at a pixel ratio that makes its logical size the window's — so it
 * shows exactly the framed extent at the live zoom, labels and lines in their on-screen
 * proportions, only sharper.
 *
 * A real map and not MapLibre's `MapSnapshotter`: the snapshotter takes its layers only before
 * its style loads, while the app's overlays are added to a loaded `Style` and then changed in
 * place (`MapOverlays`, `MapPaths`, `MapSatellite`). On a real map [prepare] replays exactly the
 * calls the live map makes, so every mode and filter exports with no rendering code of its own.
 *
 * Under the live map rather than off screen: a texture view that's never drawn never gets a
 * surface, and the live map covers it whole, so it's never seen.
 */
class MapImageRenderer(private val host: ViewGroup) {

    /** What to draw: the served style, the camera at the window's middle, the window's logical
     *  width (its pixels ÷ the screen's density) and the image's pixel size, whose ratio is the
     *  hidden map's pixel ratio. */
    data class Request(
        val styleUrl: String,
        val center: LatLng,
        val zoom: Double,
        val bearing: Double,
        val windowDpW: Float,
        val widthPx: Int,
        val heightPx: Int,
    )

    private val main = Handler(Looper.getMainLooper())
    private var mapView: MapView? = null

    /** Draws [request] and hands back its pixels, or why it couldn't. [prepare] puts the app's
     *  layers on the loaded style. Never calls back twice; [cancel] stops it without a call. */
    fun render(request: Request, prepare: (Style) -> Unit, onResult: (Result<Bitmap>) -> Unit) {
        cancel()
        val context = host.context
        val options = MapLibreMapOptions.createFromAttributes(context)
            .textureMode(true)
            .pixelRatio(request.widthPx / request.windowDpW)
            .camera(CameraPosition.Builder().target(request.center).zoom(request.zoom).bearing(request.bearing).build())
            .attributionEnabled(false)
            .logoEnabled(false)
            .compassEnabled(false)
        val view = MapView(context, options)
        mapView = view
        host.addView(view, 0, ViewGroup.LayoutParams(request.widthPx, request.heightPx))
        view.onCreate(null)
        view.onStart()
        view.onResume()

        val finish = { result: Result<Bitmap> ->
            if (mapView === view) {
                mapView = null
                main.removeCallbacksAndMessages(view)
                // Not from inside the map's own callback that got here.
                main.post { release(view) }
                onResult(result)
            }
        }
        main.postAtTime({ finish(Result.failure(IllegalStateException("export: timed out waiting for the map to render"))) }, view, SystemClock.uptimeMillis() + timeoutMs(request))
        view.addOnDidFailLoadingMapListener { error -> finish(Result.failure(IllegalStateException("export: $error"))) }
        view.getMapAsync { map ->
            map.setStyle(Style.Builder().fromUri(request.styleUrl)) { style ->
                prepare(style)
                // Idle: every source done loading — failed tiles included, which a "fully
                // rendered" never counts as done — and nothing left to draw; then a short settle,
                // as a source just added can report done a frame early (the web export's same
                // `idle` and margin).
                view.addOnDidBecomeIdleListener {
                    main.postDelayed({ snapshot(view, finish) }, SETTLE_MS)
                }
                map.triggerRepaint()
            }
        }
    }

    /**
     * The hidden map's picture, read from its texture view by Android rather than through
     * `MapLibreMap.snapshot`: MapLibre's own read-back never answers for an image as large as
     * a 4K export (measured on the emulator: every idle asked again, and none came back), while
     * the texture view hands over whatever it last showed at any size the GPU drew.
     */
    private fun snapshot(view: MapView, finish: (Result<Bitmap>) -> Unit) {
        if (mapView !== view) return
        val texture = findTexture(view) ?: return finish(Result.failure(IllegalStateException("export: the map has no texture view")))
        val bitmap = texture.getBitmap(texture.width, texture.height)
            ?: return finish(Result.failure(IllegalStateException("export: the map's picture couldn't be read")))
        finish(Result.success(bitmap))
    }

    private fun findTexture(view: View): TextureView? = when (view) {
        is TextureView -> view
        is ViewGroup -> (0 until view.childCount).firstNotNullOfOrNull { findTexture(view.getChildAt(it)) }
        else -> null
    }

    fun cancel() {
        val view = mapView ?: return
        mapView = null
        main.removeCallbacksAndMessages(view)
        release(view)
    }

    private fun release(view: MapView) {
        view.onPause()
        view.onStop()
        view.onDestroy()
        host.removeView(view)
    }

    private companion object {
        /** Generous: a fresh map fetches every tile from nothing and redraws as each arrives,
         *  each redraw the cost of the image's pixels — so more time for a bigger image. */
        const val TIMEOUT_BASE_MS = 30_000L
        const val TIMEOUT_PER_MEGAPIXEL_MS = 15_000L

        fun timeoutMs(request: Request): Long =
            TIMEOUT_BASE_MS + (request.widthPx.toLong() * request.heightPx * TIMEOUT_PER_MEGAPIXEL_MS / 1_000_000)
        const val SETTLE_MS = 150L
    }
}
