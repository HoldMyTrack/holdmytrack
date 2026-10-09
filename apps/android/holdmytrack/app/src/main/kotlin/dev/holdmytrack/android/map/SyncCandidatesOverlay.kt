package dev.holdmytrack.android.map

import android.graphics.PointF
import android.graphics.RectF
import dev.holdmytrack.android.sync.Candidate
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.Style
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.layers.SymbolLayer
import org.maplibre.android.style.sources.GeoJsonSource
import org.maplibre.geojson.Feature
import org.maplibre.geojson.FeatureCollection
import org.maplibre.geojson.LineString
import org.maplibre.geojson.Point

/**
 * The Sync tab's candidates on the map (`docs/SPEC.md` FR-3.6): every activity listed there,
 * drawn dashed from the phone's own data — none of it is on the server yet — and the one picked
 * on the list or the map solid and wider, over the rest. One GeoJSON source, two layers filtered
 * on a `role` property, under the basemap's labels, as [TrackEditOverlay] is. The lines are the
 * candidates' simplified ones ([Candidate.line]).
 */
object SyncCandidatesOverlay {

    private const val SOURCE_ID = "sync-candidates"
    private const val LINE_LAYER_ID = "sync-candidates-line"
    private const val HIGHLIGHT_LAYER_ID = "sync-candidates-highlight"

    private const val COLOR = MapOverlays.TRACK_COLOR

    /** How far from a tap a line still counts as tapped — the tracks' own tolerance, in dp. */
    private const val TAP_TOLERANCE_DP = 14f

    fun ensure(style: Style) {
        if (style.getSource(SOURCE_ID) == null) style.addSource(GeoJsonSource(SOURCE_ID))
        val beforeId = style.layers.firstOrNull { it is SymbolLayer }?.id
        fun add(layer: LineLayer) {
            if (style.getLayer(layer.id) != null) return
            if (beforeId != null) style.addLayerBelow(layer, beforeId) else style.addLayer(layer)
        }
        fun role(name: String) = Expression.eq(Expression.get("role"), name)
        add(
            LineLayer(LINE_LAYER_ID, SOURCE_ID).withFilter(role("line")).withProperties(
                PropertyFactory.lineCap(Property.LINE_CAP_BUTT),
                PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
                PropertyFactory.lineColor(COLOR),
                PropertyFactory.lineWidth(3f),
                PropertyFactory.lineOpacity(0.85f),
                PropertyFactory.lineDasharray(arrayOf(1.5f, 1.5f)),
            ),
        )
        add(
            LineLayer(HIGHLIGHT_LAYER_ID, SOURCE_ID).withFilter(role("highlight")).withProperties(
                PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
                PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
                PropertyFactory.lineColor(COLOR),
                PropertyFactory.lineWidth(5f),
                PropertyFactory.lineOpacity(1f),
            ),
        )
    }

    /** Draws [candidates], [highlight] (a [Candidate.key]) picked out over the rest. */
    fun set(style: Style, candidates: List<Candidate>, highlight: String?) {
        ensure(style)
        val source = style.getSourceAs<GeoJsonSource>(SOURCE_ID) ?: return
        // The picked one last, so it's drawn over its neighbours.
        val ordered = candidates.sortedBy { it.key == highlight }
        val features = ordered.filter { it.line.size >= 2 }.map { candidate ->
            Feature.fromGeometry(LineString.fromLngLats(candidate.line.map { Point.fromLngLat(it[0], it[1]) })).apply {
                addStringProperty("role", if (candidate.key == highlight) "highlight" else "line")
                addStringProperty("key", candidate.key)
            }
        }
        source.setGeoJson(FeatureCollection.fromFeatures(features))
    }

    fun clear(style: Style) {
        style.getSourceAs<GeoJsonSource>(SOURCE_ID)?.setGeoJson(FeatureCollection.fromFeatures(emptyList()))
    }

    /** The key of the candidate drawn under [screen], if any. */
    fun keyAt(map: MapLibreMap, screen: PointF, density: Float): String? {
        val tolerance = TAP_TOLERANCE_DP * density
        val box = RectF(screen.x - tolerance, screen.y - tolerance, screen.x + tolerance, screen.y + tolerance)
        return map.queryRenderedFeatures(box, HIGHLIGHT_LAYER_ID, LINE_LAYER_ID)
            .firstNotNullOfOrNull { it.getStringProperty("key") }
    }
}
