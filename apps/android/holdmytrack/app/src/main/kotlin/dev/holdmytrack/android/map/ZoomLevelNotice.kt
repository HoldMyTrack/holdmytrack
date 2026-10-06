package dev.holdmytrack.android.map

import android.view.View
import android.widget.TextView
import dev.holdmytrack.android.R

/**
 * Names the level Fog and Heatmap draw at (`docs/SPEC.md` FR-4.2, FR-4.3), the web's
 * `ui/ZoomLevelNotice.tsx`: whole countries, then whole states or provinces, then exactly where
 * you've been ([MapOverlays.zoomTier]). Nothing else on the map says which is in view, and a veil
 * lifted from a whole country reads as a bug when you don't know that's the level you're at.
 *
 * Shown on entering either mode and whenever a zoom lands in another level, faded out after
 * [SHOW_MS]. MapFragment passes the zoom when the camera comes to rest ([onCameraIdle]), not
 * mid-gesture, so a pinch through two levels names only where it lands. Never in Normal, which
 * draws tracks at every zoom, nor while the modes are away ([render]'s `active`).
 */
class ZoomLevelNotice(private val view: TextView) {

    private var tier: ZoomTier? = null
    private var mode = MapMode.NORMAL
    private var active = false

    /** What's on screen, or was last — shown again only once it changes. */
    private var shown: Pair<MapMode, ZoomTier>? = null

    private val fadeOut = Runnable { hide() }

    fun onCameraIdle(zoom: Double) {
        tier = MapOverlays.zoomTier(zoom)
        update()
    }

    /** [active]: the session is verified and nothing has put the modes away (a recording). */
    fun render(mode: MapMode, active: Boolean) {
        this.mode = mode
        this.active = active
        update()
    }

    private fun update() {
        val tier = tier
        if (!active || mode == MapMode.NORMAL || tier == null) {
            shown = null
            hide()
            return
        }
        val next = mode to tier
        if (next == shown) return
        shown = next
        view.text = view.context.getString(text(mode, tier))
        view.removeCallbacks(fadeOut)
        view.animate().cancel()
        if (view.visibility != View.VISIBLE) {
            view.alpha = 0f
            view.visibility = View.VISIBLE
        }
        view.animate().alpha(1f).setDuration(FADE_MS)
        view.postDelayed(fadeOut, SHOW_MS)
    }

    private fun hide() {
        view.removeCallbacks(fadeOut)
        if (view.visibility != View.VISIBLE) return
        view.animate().cancel()
        view.animate().alpha(0f).setDuration(FADE_MS).withEndAction { view.visibility = View.GONE }
    }

    private fun text(mode: MapMode, tier: ZoomTier): Int = when (mode) {
        MapMode.FOG -> when (tier) {
            ZoomTier.COUNTRY -> R.string.map_level_fog_country
            ZoomTier.REGION -> R.string.map_level_fog_region
            ZoomTier.CITY -> R.string.map_level_fog_city
        }
        else -> when (tier) {
            ZoomTier.COUNTRY -> R.string.map_level_heatmap_country
            ZoomTier.REGION -> R.string.map_level_heatmap_region
            ZoomTier.CITY -> R.string.map_level_heatmap_city
        }
    }

    private companion object {
        /** The web's `SHOW_MS`, and its 0.4s opacity transition. */
        const val SHOW_MS = 3000L
        const val FADE_MS = 400L
    }
}
