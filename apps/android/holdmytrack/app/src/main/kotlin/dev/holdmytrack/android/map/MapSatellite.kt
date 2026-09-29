package dev.holdmytrack.android.map

import android.content.Context
import org.json.JSONObject
import org.maplibre.android.maps.Style
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory

/**
 * The map's Satellite toggle (`docs/SPEC.md` FR-4.14): shows the served style's imagery layer
 * and hides the basemap's background and area fills over it, so roads, boundaries and labels
 * stay on top — the web's Base map switch. The served style has the imagery only when the
 * deployment configures some ([isAvailable]); which fills to hide comes from the style's own
 * metadata, so the list is defined once, in apps/web/src/map/style.ts. A per-device choice, off
 * until turned on, kept in SharedPreferences like [MapPaths].
 */
object MapSatellite {

    /** Must match `SATELLITE_LAYER_ID`/`SATELLITE_SOURCE` and `SATELLITE_HIDES_METADATA` in
     *  apps/web/src/map/style.ts. */
    private const val LAYER_ID = "satellite"
    private const val SOURCE_ID = "satellite"
    private const val HIDES_METADATA = "holdmytrack:satellite-hides"

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
        val fills = PropertyFactory.visibility(if (on) Property.NONE else Property.VISIBLE)
        hiddenLayerIds(style).forEach { id -> style.getLayer(id)?.setProperties(fills) }
    }

    private fun hiddenLayerIds(style: Style): List<String> {
        val ids = runCatching { JSONObject(style.json).optJSONObject("metadata")?.optJSONArray(HIDES_METADATA) }.getOrNull()
            ?: return emptyList()
        return (0 until ids.length()).map(ids::getString)
    }
}
