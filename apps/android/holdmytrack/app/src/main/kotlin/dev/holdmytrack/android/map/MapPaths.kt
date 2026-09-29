package dev.holdmytrack.android.map

import android.content.Context
import org.maplibre.android.maps.Style
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory

/**
 * The map's Trails & bike paths toggle (`docs/SPEC.md` FR-4.13): shows or hides the served
 * style's path layers — trails, tracks (dirt, farm and forest roads) and cycleways, which the
 * web's Overlays menu switches separately — all together. The style ships them hidden. A
 * per-device choice, off until turned on, kept in SharedPreferences like the theme
 * (`settings/AppTheme.kt`).
 */
object MapPaths {

    /** Must match `PATH_LAYER_IDS` in apps/web/src/map/style.ts, which builds the served style. */
    private val LAYER_IDS = listOf(
        "paths_cycleway",
        "paths_trail",
        "paths_track",
        "paths_bridges_cycleway",
        "paths_bridges_trail",
        "paths_bridges_track",
    )

    private const val PREFS = "map_paths"
    private const val KEY = "show"

    fun isOn(context: Context): Boolean =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(KEY, false)

    fun set(context: Context, on: Boolean) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putBoolean(KEY, on).apply()
    }

    /** A no-op for a layer the style doesn't have (an older served style). */
    fun apply(style: Style, on: Boolean) {
        val visibility = PropertyFactory.visibility(if (on) Property.VISIBLE else Property.NONE)
        LAYER_IDS.forEach { id -> style.getLayer(id)?.setProperties(visibility) }
    }
}
