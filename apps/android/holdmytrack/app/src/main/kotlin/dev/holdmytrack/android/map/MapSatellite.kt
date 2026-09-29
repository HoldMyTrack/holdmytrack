package dev.holdmytrack.android.map

import android.content.Context
import org.json.JSONObject
import org.maplibre.android.maps.Style
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory

/**
 * The map's Satellite toggle (`docs/SPEC.md` FR-4.14): shows the served style's imagery layer,
 * hides the basemap's background and area fills over it and draws the roads see-through, so
 * roads, boundaries and labels stay on top — the web's Base map switch. The served style has the
 * imagery only when the deployment configures some ([isAvailable]); which fills to hide, which
 * roads to dim and how far all come from the style's own metadata, so they're defined once, in
 * apps/web/src/map/style.ts. A per-device choice, off
 * until turned on, kept in SharedPreferences like [MapPaths].
 */
object MapSatellite {

    /** Must match `SATELLITE_LAYER_ID`/`SATELLITE_SOURCE` and the `SATELLITE_*_METADATA` keys in
     *  apps/web/src/map/style.ts. */
    private const val LAYER_ID = "satellite"
    private const val SOURCE_ID = "satellite"
    private const val HIDES_METADATA = "holdmytrack:satellite-hides"
    private const val DIMS_METADATA = "holdmytrack:satellite-dims"
    private const val ROAD_OPACITY_METADATA = "holdmytrack:satellite-road-opacity"

    private const val PREFS = "map_satellite"
    private const val KEY = "show"

    fun isOn(context: Context): Boolean =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(KEY, false)

    fun set(context: Context, on: Boolean) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putBoolean(KEY, on).apply()
    }

    /** Whether the deployment configures imagery: the server leaves the source out otherwise. */
    fun isAvailable(style: Style): Boolean = style.getSource(SOURCE_ID) != null

    /** A no-op on a style without imagery. */
    fun apply(style: Style, on: Boolean) {
        if (!isAvailable(style)) return
        style.getLayer(LAYER_ID)?.setProperties(PropertyFactory.visibility(if (on) Property.VISIBLE else Property.NONE))
        val metadata = runCatching { JSONObject(style.json).optJSONObject("metadata") }.getOrNull() ?: return
        val fills = PropertyFactory.visibility(if (on) Property.NONE else Property.VISIBLE)
        ids(metadata, HIDES_METADATA).forEach { id -> style.getLayer(id)?.setProperties(fills) }
        // Off is the default opacity, 1: the dimmed roads are the ones that set none of their own.
        val opacity = if (on) metadata.optDouble(ROAD_OPACITY_METADATA, 1.0).toFloat() else 1f
        ids(metadata, DIMS_METADATA).forEach { id -> style.getLayer(id)?.setProperties(PropertyFactory.lineOpacity(opacity)) }
    }

    private fun ids(metadata: JSONObject, key: String): List<String> {
        val ids = metadata.optJSONArray(key) ?: return emptyList()
        return (0 until ids.length()).map(ids::getString)
    }
}
