package dev.holdmytrack.android.map

import android.net.Uri
import dev.holdmytrack.android.BuildConfig
import dev.holdmytrack.android.net.Session
import org.maplibre.android.maps.Style
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.sources.TileSet
import org.maplibre.android.style.sources.VectorSource

/**
 * The Layers menu's Bike paths and Shared paths (`docs/SPEC.md` FR-4.13): OpenStreetMap's
 * cycleways and bike-designated paths from `/tiles/v1/bike-paths` (FR-4.16), drawn from zoom 9
 * where the basemap has none. The web's `apps/web/src/map/bikePaths.ts` draws the same layers.
 *
 * [MapOverlays.attach] adds them after Fog and Heatmap and before the activity tracks, so they
 * paint over the veil and the heat and under the rider's own tracks. [MapPaths.apply] shows them.
 */
object MapBikePaths {

    /** Where the tiles start (internal/httpapi's bikePathsMinZoom). */
    const val MIN_ZOOM = 9f

    /** Past it the tiles serve every zoom above by overzooming. */
    private const val MAX_ZOOM = 14f

    private const val SOURCE_ID = "bike-paths"
    private const val SOURCE_LAYER = "bike_paths"
    const val CYCLEWAY_LAYER_ID = "bike-paths-cycleway"
    const val SHARED_LAYER_ID = "bike-paths-shared"

    /** Must match `PATH_COLORS` in apps/web/src/map/style.ts, for the light and dark flavors. */
    private const val CYCLEWAY_LIGHT = "#1f7fa8"
    private const val SHARED_LIGHT = "#4ba3c9"
    private const val CYCLEWAY_DARK = "#5cbfe0"
    private const val SHARED_DARK = "#93d6ec"

    /**
     * Adds the source and both layers below [beforeId], hidden, if they aren't already there.
     * [night] is the basemap's dark flavor, which takes the lighter blues.
     */
    fun add(style: Style, beforeId: String?, night: Boolean) {
        if (style.getSource(SOURCE_ID) == null) style.addSource(tileSource())
        if (style.getLayer(SHARED_LAYER_ID) == null) {
            insert(style, LineLayer(SHARED_LAYER_ID, SOURCE_ID).withSourceLayer(SOURCE_LAYER).withProperties(
                PropertyFactory.visibility(Property.NONE),
                PropertyFactory.lineColor(if (night) SHARED_DARK else SHARED_LIGHT),
                // Dashed like a trail, since people walk it too; a cycleway is solid.
                PropertyFactory.lineDasharray(arrayOf(2f, 1f)),
                PropertyFactory.lineWidth(width(0.8f, 1f, 3.5f)),
            ).apply {
                setFilter(Expression.eq(Expression.get("kind"), Expression.literal("shared")))
                setMinZoom(MIN_ZOOM)
            }, beforeId)
        }
        if (style.getLayer(CYCLEWAY_LAYER_ID) == null) {
            insert(style, LineLayer(CYCLEWAY_LAYER_ID, SOURCE_ID).withSourceLayer(SOURCE_LAYER).withProperties(
                PropertyFactory.visibility(Property.NONE),
                PropertyFactory.lineColor(if (night) CYCLEWAY_DARK else CYCLEWAY_LIGHT),
                PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
                PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
                PropertyFactory.lineWidth(width(1f, 1.4f, 4f)),
            ).apply {
                setFilter(Expression.eq(Expression.get("kind"), Expression.literal("cycleway")))
                setMinZoom(MIN_ZOOM)
            }, beforeId)
        }
    }

    /** Shows or hides each kind; a no-op before [add]. */
    fun apply(style: Style, bikePaths: Boolean, sharedPaths: Boolean) {
        for ((id, on) in listOf(CYCLEWAY_LAYER_ID to bikePaths, SHARED_LAYER_ID to sharedPaths)) {
            style.getLayer(id)?.setProperties(PropertyFactory.visibility(if (on) Property.VISIBLE else Property.NONE))
        }
    }

    /** The web's widths: [atMin] at zoom 9, [at13] at 13, [at18] at 18, exponential between. */
    private fun width(atMin: Float, at13: Float, at18: Float) = Expression.interpolate(
        Expression.exponential(1.6f), Expression.zoom(),
        Expression.stop(MIN_ZOOM, atMin), Expression.stop(13f, at13), Expression.stop(18f, at18),
    )

    private fun tileSource(): VectorSource {
        val version = Session.tileVersion.let { if (it.isEmpty()) "" else "?cv=${Uri.encode(it)}" }
        // "2.2.0" is the TileJSON version this describes, not the tile set's own version.
        val tiles = TileSet("2.2.0", "${BuildConfig.API_BASE_URL}/tiles/v1/bike-paths/{z}/{x}/{y}.mvt$version").apply {
            minZoom = MIN_ZOOM
            maxZoom = MAX_ZOOM
        }
        return VectorSource(SOURCE_ID, tiles)
    }

    private fun insert(style: Style, layer: LineLayer, beforeId: String?) {
        if (beforeId != null) style.addLayerBelow(layer, beforeId) else style.addLayer(layer)
    }
}