package dev.holdmytrack.android.map

import android.content.Context
import androidx.core.content.edit
import org.maplibre.android.maps.Style

/**
 * The Layers menu's Bike paths, Shared paths and Mountain bike trails (`docs/SPEC.md` FR-4.13,
 * [LayersMenu]), drawn
 * by [MapBikePaths]: a per-device choice, each off until turned on, kept in SharedPreferences
 * like the theme (`settings/AppTheme.kt`). Also lifts the served style's trail and track layers,
 * which are always drawn, over Fog's veil ([raise]).
 */
object MapPaths {

    data class Paths(val bikePaths: Boolean, val sharedPaths: Boolean, val mtbTrails: Boolean) {
        /** How many are on: the Layers button's count. */
        val count: Int get() = listOf(bikePaths, sharedPaths, mtbTrails).count { it }
    }

    /** All hidden: what's drawn while Show layers is off ([MapLayersSwitch]). */
    val NONE = Paths(bikePaths = false, sharedPaths = false, mtbTrails = false)

    /** The trail and track colors the served style draws with, for the Layers menu's legend: must
     *  match `PATH_COLORS` in apps/web/src/map/style.ts, as must their dashes, `pathDash`. */
    private const val TRAIL_LIGHT = "#4f7a3a"
    private const val TRAIL_DARK = "#8fbf6a"
    private const val TRACK_LIGHT = "#8a5a2b"
    private const val TRACK_DARK = "#c9955e"
    val TRAIL_DASH = floatArrayOf(2f, 1f)
    val TRACK_DASH = floatArrayOf(3f, 1.5f)

    fun trailColor(night: Boolean) = if (night) TRAIL_DARK else TRAIL_LIGHT
    fun trackColor(night: Boolean) = if (night) TRACK_DARK else TRACK_LIGHT

    /** Must match `PATH_LAYER_IDS` in apps/web/src/map/style.ts, which builds the served style. */
    private val PATH_LAYER_IDS = listOf("paths_trail", "paths_track", "paths_bridges_trail", "paths_bridges_track")

    private const val PREFS = "map_paths"
    private const val KEY_BIKE_PATHS = "bike_paths"
    private const val KEY_SHARED_PATHS = "shared_paths"
    private const val KEY_MTB_TRAILS = "mtb_trails"

    /** The single Trails & bike paths toggle's key, from before the Layers menu: a phone that had
     *  it on starts with Bike paths on. */
    private const val KEY_LEGACY = "show"

    /** The Trails and Tracks entries' keys, from before trails and tracks were always drawn. */
    private val RETIRED_KEYS = listOf("trails", "tracks")

    fun get(context: Context): Paths {
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        return Paths(
            bikePaths = prefs.getBoolean(KEY_BIKE_PATHS, prefs.getBoolean(KEY_LEGACY, false)),
            sharedPaths = prefs.getBoolean(KEY_SHARED_PATHS, false),
            mtbTrails = prefs.getBoolean(KEY_MTB_TRAILS, false),
        )
    }

    fun set(context: Context, paths: Paths) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit {
            putBoolean(KEY_BIKE_PATHS, paths.bikePaths)
            putBoolean(KEY_SHARED_PATHS, paths.sharedPaths)
            putBoolean(KEY_MTB_TRAILS, paths.mtbTrails)
            remove(KEY_LEGACY)
            RETIRED_KEYS.forEach(::remove)
        }
    }

    /** A no-op before [MapOverlays.attach] has added the bike-path layers. */
    fun apply(style: Style, paths: Paths) {
        MapBikePaths.apply(style, paths.bikePaths, paths.sharedPaths, paths.mtbTrails)
    }

    /**
     * Moves the trail and track layers from among the basemap's roads to below [beforeId], the
     * overlays' insertion point, so they paint over Fog's veil and Heatmap's heat rather than
     * under them, as on the web (`apps/web/src/map/paths.ts`'s raisePathLayers). MapLibre Native
     * has no moveLayer: each is taken out and put back. Called once per style, by
     * [MapOverlays.attach], after Fog and Heatmap and before the bike paths and the tracks.
     */
    fun raise(style: Style, beforeId: String?) {
        for (id in PATH_LAYER_IDS) {
            val layer = style.getLayer(id) ?: continue
            style.removeLayer(layer)
            if (beforeId != null) style.addLayerBelow(layer, beforeId) else style.addLayer(layer)
        }
    }
}
