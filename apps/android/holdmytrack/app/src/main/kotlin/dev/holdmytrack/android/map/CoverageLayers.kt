package dev.holdmytrack.android.map

import android.net.Uri
import android.util.Log
import dev.holdmytrack.android.BuildConfig
import dev.holdmytrack.android.net.Session
import org.maplibre.android.maps.Style
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.BackgroundLayer
import org.maplibre.android.style.layers.CircleLayer
import org.maplibre.android.style.layers.FillLayer
import org.maplibre.android.style.layers.HeatmapLayer
import org.maplibre.android.style.layers.Layer
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.PropertyValue
import org.maplibre.android.style.sources.TileSet
import org.maplibre.android.style.sources.VectorSource
import java.util.WeakHashMap

/**
 * Fog of War and Heatmap on the map, from the overlays the served style carries
 * ([CoverageStyle]): this adds what the style describes and switches its views, and decides
 * nothing about how either mode looks. The web's `coverageLayers.ts`.
 *
 * Each [Style] keeps its own overlays and the view last applied to it, so a satellite switch
 * ([setSatellite]) and a refetch ([refresh]) keep the mode without being told it, and the
 * export's hidden map (`export/MapImageRenderer`) never touches the live map's. Kept by weak
 * reference: a style reload replaces the [Style], and its entry goes with it.
 */
object CoverageLayers {

    private class State(val overlays: CoverageStyle.Overlays) {
        var mode = MapMode.NORMAL
        var satellite = false
    }

    private val states = WeakHashMap<Style, State>()

    /** Reads the overlays of [style], just loaded; a no-op for a style already read. */
    fun load(style: Style) {
        if (style in states) return
        val overlays = CoverageStyle.read(StyleMetadata.of(style))
        if (overlays == null) Log.w(TAG, "the style carries no overlays") else states[style] = State(overlays)
    }

    /** The tier in view at [zoom], by [style]'s tiers. */
    fun tier(style: Style, zoom: Double): ZoomTier = CoverageStyle.tier(states[style]?.overlays?.tiers.orEmpty(), zoom)

    /** Adds the sources and layers not yet on [style], in the style's order, below [beforeId]. */
    fun add(style: Style, beforeId: String?) {
        val overlays = states[style]?.overlays ?: return
        overlays.sources.forEach { (id, source) ->
            if (style.getSource(id) == null) style.addSource(VectorSource(id, tileSet(source)))
        }
        for (spec in overlays.layers) {
            if (style.getLayer(spec.id) != null) continue
            val layer = layer(spec) ?: continue
            if (beforeId != null) style.addLayerBelow(layer, beforeId) else style.addLayer(layer)
        }
    }

    /** Applies [mode]'s view, over imagery when [satellite] says it's drawn, or as last applied. */
    fun apply(style: Style, mode: MapMode, satellite: Boolean? = null) {
        val state = states[style] ?: return
        state.mode = mode
        satellite?.let { state.satellite = it }
        val view = CoverageStyle.view(state.overlays, mode, state.satellite) ?: return
        view.properties.forEach { (id, props) -> style.getLayer(id)?.setProperties(*props.map { (k, v) -> property(k, v) }.toTypedArray()) }
    }

    /** The view for imagery drawn or not, in the mode last applied. */
    fun setSatellite(style: Style, on: Boolean) {
        val state = states[style] ?: return
        apply(style, state.mode, on)
    }

    /**
     * Every overlay tile fetched again at the current tile version, once the server has
     * re-rendered them (`CoverageWatch`). MapLibre Native can't change a source's URL, so the
     * layers and sources are replaced, at the same place in the stack, and the view put back.
     */
    fun refresh(style: Style) {
        val state = states[style] ?: return
        val overlays = state.overlays
        val ids = overlays.layers.map { it.id }.toSet()
        val order = style.layers.map { it.id }
        if (order.none { it in ids }) return
        // Back under whatever sat right over them: the paths, which paint over the veil, or the tracks.
        val beforeId = order.drop(order.indexOfLast { it in ids } + 1).firstOrNull { it !in ids }
        // Every layer before any source: Fog's veil and edge share theirs.
        ids.forEach { id -> style.getLayer(id)?.let(style::removeLayer) }
        overlays.sources.keys.forEach { style.removeSource(it) }
        add(style, beforeId)
        apply(style, state.mode)
    }

    private fun tileSet(source: CoverageStyle.Source): TileSet {
        val version = Session.tileVersion.let { if (it.isEmpty()) "" else "?cv=${Uri.encode(it)}" }
        // "2.2.0" is the TileJSON version this describes, not the tile set's own version.
        return TileSet("2.2.0", "${BuildConfig.API_BASE_URL}${source.tiles}$version").apply {
            minZoom = source.minZoom
            maxZoom = source.maxZoom
            source.attribution?.let { attribution = it }
        }
    }

    private fun layer(spec: CoverageStyle.Layer): Layer? {
        val layer = when (spec.type) {
            "background" -> BackgroundLayer(spec.id)
            "fill" -> FillLayer(spec.id, spec.source).apply {
                spec.sourceLayer?.let(::setSourceLayer)
                spec.filter?.let { setFilter(Expression.raw(it)) }
            }
            "line" -> LineLayer(spec.id, spec.source).apply {
                spec.sourceLayer?.let(::setSourceLayer)
                spec.filter?.let { setFilter(Expression.raw(it)) }
            }
            "heatmap" -> HeatmapLayer(spec.id, spec.source).apply {
                spec.sourceLayer?.let(::setSourceLayer)
                spec.filter?.let { setFilter(Expression.raw(it)) }
            }
            "circle" -> CircleLayer(spec.id, spec.source).apply {
                spec.sourceLayer?.let(::setSourceLayer)
                spec.filter?.let { setFilter(Expression.raw(it)) }
            }
            else -> {
                Log.w(TAG, "overlay ${spec.id}: unsupported type ${spec.type}")
                return null
            }
        }
        spec.minZoom?.let(layer::setMinZoom)
        spec.maxZoom?.let(layer::setMaxZoom)
        layer.setProperties(*spec.properties.map { (k, v) -> property(k, v) }.toTypedArray())
        return layer
    }

    private fun property(name: String, value: CoverageStyle.Value): PropertyValue<*> = when (value) {
        is CoverageStyle.Value.Expression -> PropertyValue(name, Expression.raw(value.json))
        is CoverageStyle.Value.Literal -> PropertyValue(name, value.value)
    }

    private const val TAG = "CoverageLayers"
}
