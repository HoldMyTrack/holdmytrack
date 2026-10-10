package dev.holdmytrack.android.map

import android.content.Context
import androidx.core.content.edit
import org.maplibre.android.maps.Style
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory

/**
 * The Layers menu's Trails, Tracks, Bike paths and Shared paths (`docs/SPEC.md` FR-4.13,
 * [LayersMenu]): shows or hides the served style's trail and track layers — the latter dirt,
 * farm and forest roads — and [MapBikePaths]' cycleways and shared paths. The style ships its
 * layers hidden. A per-device choice, each off until turned on, kept in SharedPreferences like
 * the theme (`settings/AppTheme.kt`).
 */
object MapPaths {

    data class Paths(val trails: Boolean, val tracks: Boolean, val bikePaths: Boolean, val sharedPaths: Boolean) {
        /** How many are on: the Layers button's count. */
        val count: Int get() = listOf(trails, tracks, bikePaths, sharedPaths).count { it }
    }

    /** Every kind hidden: what's drawn while Show layers is off ([MapLayersSwitch]). */
    val NONE = Paths(trails = false, tracks = false, bikePaths = false, sharedPaths = false)

    /** Must match `TRAIL_LAYER_IDS` and `TRACK_LAYER_IDS` in apps/web/src/map/style.ts, which
     *  builds the served style. */
    private val TRAIL_LAYER_IDS = listOf("paths_trail", "paths_bridges_trail")
    private val TRACK_LAYER_IDS = listOf("paths_track", "paths_bridges_track")

    private const val PREFS = "map_paths"
    private const val KEY_TRAILS = "trails"
    private const val KEY_TRACKS = "tracks"
    private const val KEY_BIKE_PATHS = "bike_paths"
    private const val KEY_SHARED_PATHS = "shared_paths"

    /** The single Trails & bike paths toggle's key, from before the Layers menu: a phone that had
     *  it on starts with all three on. */
    private const val KEY_LEGACY = "show"

    fun get(context: Context): Paths {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        val legacy = prefs.getBoolean(KEY_LEGACY, false)
        return Paths(
            trails = prefs.getBoolean(KEY_TRAILS, legacy),
            tracks = prefs.getBoolean(KEY_TRACKS, legacy),
            bikePaths = prefs.getBoolean(KEY_BIKE_PATHS, legacy),
            sharedPaths = prefs.getBoolean(KEY_SHARED_PATHS, false),
        )
    }

    fun set(context: Context, paths: Paths) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit {
            putBoolean(KEY_TRAILS, paths.trails)
            putBoolean(KEY_TRACKS, paths.tracks)
            putBoolean(KEY_BIKE_PATHS, paths.bikePaths)
            putBoolean(KEY_SHARED_PATHS, paths.sharedPaths)
            remove(KEY_LEGACY)
        }
    }

    /** A no-op for a layer the style doesn't have (an older served style, or the bike-path
     *  layers before [MapOverlays.attach] has added them). */
    fun apply(style: Style, paths: Paths) {
        for ((ids, on) in listOf(
            TRAIL_LAYER_IDS to paths.trails,
            TRACK_LAYER_IDS to paths.tracks,
        )) {
            val visibility = PropertyFactory.visibility(if (on) Property.VISIBLE else Property.NONE)
            ids.forEach { id -> style.getLayer(id)?.setProperties(visibility) }
        }
        MapBikePaths.apply(style, paths.bikePaths, paths.sharedPaths)
    }

    /**
     * Moves the trail and track layers from among the basemap's roads to below [beforeId], the
     * overlays' insertion point, so they paint over Fog's veil and Heatmap's heat rather than
     * under them, as on the web (`apps/web/src/map/paths.ts`'s raisePathLayers). MapLibre Native
     * has no moveLayer: each is taken out and put back. Called once per style, by
     * [MapOverlays.attach], after Fog and Heatmap and before the bike paths and the tracks.
     */
    fun raise(style: Style, beforeId: String?) {
        for (id in TRAIL_LAYER_IDS + TRACK_LAYER_IDS) {
            val layer = style.getLayer(id) ?: continue
            style.removeLayer(layer)
            if (beforeId != null) style.addLayerBelow(layer, beforeId) else style.addLayer(layer)
        }
    }
}
