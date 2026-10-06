package dev.holdmytrack.android.map

import android.graphics.PointF
import android.graphics.RectF
import dev.holdmytrack.android.net.TrackPoint
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.Style
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.CircleLayer
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.layers.SymbolLayer
import org.maplibre.android.style.sources.GeoJsonSource
import org.maplibre.geojson.Feature
import org.maplibre.geojson.FeatureCollection
import org.maplibre.geojson.LineString
import org.maplibre.geojson.Point

/** What the editor previews: everything outside the knobs going (Chop, the default), what's
 *  between them (Cut, while its button is held), or the track in two at a knob (Split, held). */
enum class EditPreview { CHOP, CUT, SPLIT }

/**
 * The track being edited, drawn over the map — the web's `apps/web/src/map/trackEdit.ts`: what
 * the edit keeps in the track colour, what it would take out dashed in red, every point as a
 * small ring (faded red where it would go), and the two knobs. One GeoJSON source, four layers
 * filtered on a `role` property, under the basemap's labels. While it's up `MapOverlays` hides
 * every other track, as the web does.
 */
object TrackEditOverlay {

    private const val SOURCE_ID = "track-edit"
    private const val PREVIEW_LAYER_ID = "track-edit-preview"
    private const val LINE_LAYER_ID = "track-edit-line"
    private const val SECOND_LAYER_ID = "track-edit-second"
    private const val POINTS_LAYER_ID = "track-edit-points"
    private const val KNOBS_LAYER_ID = "track-edit-knobs"

    private const val ACCENT = "#b07e2e"
    private const val REMOVED = "#c53030"
    private const val SECOND = "#2b6cb0"

    /** How far from a tap a point still counts as tapped — the web's click tolerance, in dp. */
    private const val TAP_TOLERANCE_DP = 12f

    fun ensure(style: Style) {
        if (style.getSource(SOURCE_ID) == null) style.addSource(GeoJsonSource(SOURCE_ID))
        val beforeId = style.layers.firstOrNull { it is SymbolLayer }?.id
        fun add(layer: org.maplibre.android.style.layers.Layer) {
            if (style.getLayer(layer.id) != null) return
            if (beforeId != null) style.addLayerBelow(layer, beforeId) else style.addLayer(layer)
        }
        fun role(name: String) = Expression.eq(Expression.get("role"), name)
        val removed = Expression.toBool(Expression.coalesce(Expression.get("removed"), Expression.literal(false)))
        add(
            LineLayer(PREVIEW_LAYER_ID, SOURCE_ID).withFilter(role("preview")).withProperties(
                PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
                PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
                PropertyFactory.lineColor(REMOVED),
                PropertyFactory.lineWidth(3f),
                PropertyFactory.lineOpacity(0.55f),
                PropertyFactory.lineDasharray(arrayOf(1.5f, 1.5f)),
            ),
        )
        add(
            LineLayer(LINE_LAYER_ID, SOURCE_ID).withFilter(role("kept")).withProperties(
                PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
                PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
                PropertyFactory.lineColor(ACCENT),
                PropertyFactory.lineWidth(4f),
                PropertyFactory.lineOpacity(0.95f),
            ),
        )
        add(
            LineLayer(SECOND_LAYER_ID, SOURCE_ID).withFilter(role("second")).withProperties(
                PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
                PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
                PropertyFactory.lineColor(SECOND),
                PropertyFactory.lineWidth(4f),
                PropertyFactory.lineOpacity(0.95f),
            ),
        )
        add(
            CircleLayer(POINTS_LAYER_ID, SOURCE_ID).withFilter(role("point")).withProperties(
                PropertyFactory.circleRadius(
                    Expression.interpolate(
                        Expression.linear(), Expression.zoom(),
                        Expression.stop(12, 1.5f), Expression.stop(16, 3f), Expression.stop(19, 5f),
                    ),
                ),
                PropertyFactory.circleColor("#ffffff"),
                PropertyFactory.circleStrokeColor(Expression.switchCase(removed, Expression.literal(REMOVED), Expression.literal(ACCENT))),
                PropertyFactory.circleStrokeWidth(1.2f),
                PropertyFactory.circleOpacity(Expression.switchCase(removed, Expression.literal(0.5f), Expression.literal(1f))),
                PropertyFactory.circleStrokeOpacity(Expression.switchCase(removed, Expression.literal(0.5f), Expression.literal(1f))),
            ),
        )
        add(
            CircleLayer(KNOBS_LAYER_ID, SOURCE_ID).withFilter(role("knob")).withProperties(
                PropertyFactory.circleRadius(7f),
                PropertyFactory.circleColor(ACCENT),
                PropertyFactory.circleStrokeColor("#ffffff"),
                PropertyFactory.circleStrokeWidth(2.5f),
            ),
        )
    }

    /**
     * Draws [visible] — the recorded points as the session's edit leaves them — with the knobs
     * at [lo] and [hi]: with [EditPreview.CHOP] the stretch between them kept and both sides
     * dashed; with [EditPreview.CUT] the stretch between them dashed and a straight kept line
     * joining the knobs, which is what Cut would do. With [split] — a split made, or the one
     * Split would make — the track is its two parts instead, the second in blue, with the one
     * knob between them (`docs/SPEC.md` FR-5.17).
     */
    fun set(style: Style, visible: List<TrackPoint>, lo: Int, hi: Int, preview: EditPreview, split: Int?) {
        ensure(style)
        val source = style.getSourceAs<GeoJsonSource>(SOURCE_ID) ?: return
        val features = mutableListOf<Feature>()
        fun line(role: String, points: List<TrackPoint>) {
            if (points.size < 2) return
            features += Feature.fromGeometry(LineString.fromLngLats(points.map { Point.fromLngLat(it.lon, it.lat) }))
                .apply { addStringProperty("role", role) }
        }
        val last = visible.size - 1
        if (split != null) {
            line("kept", visible.subList(0, split + 1))
            line("second", visible.subList(split, last + 1))
            visible.forEach { p ->
                features += Feature.fromGeometry(Point.fromLngLat(p.lon, p.lat)).apply {
                    addStringProperty("role", "point")
                    addNumberProperty("t", p.t)
                }
            }
            val at = visible[split]
            features += Feature.fromGeometry(Point.fromLngLat(at.lon, at.lat)).apply { addStringProperty("role", "knob") }
            source.setGeoJson(FeatureCollection.fromFeatures(features))
            return
        }
        val cutting = preview == EditPreview.CUT && hi - lo >= 2
        if (cutting) {
            line("kept", visible.subList(0, lo + 1))
            line("kept", listOf(visible[lo], visible[hi]))
            line("kept", visible.subList(hi, last + 1))
            line("preview", visible.subList(lo, hi + 1))
        } else {
            line("preview", visible.subList(0, lo + 1))
            line("kept", visible.subList(lo, hi + 1))
            line("preview", visible.subList(hi, last + 1))
        }
        visible.forEachIndexed { i, p ->
            val removed = if (cutting) i in (lo + 1) until hi else i < lo || i > hi
            features += Feature.fromGeometry(Point.fromLngLat(p.lon, p.lat)).apply {
                addStringProperty("role", "point")
                addNumberProperty("t", p.t)
                addBooleanProperty("removed", removed)
            }
        }
        for (i in if (lo == hi) listOf(lo) else listOf(lo, hi)) {
            val p = visible.getOrNull(i) ?: continue
            features += Feature.fromGeometry(Point.fromLngLat(p.lon, p.lat)).apply { addStringProperty("role", "knob") }
        }
        source.setGeoJson(FeatureCollection.fromFeatures(features))
    }

    fun clear(style: Style) {
        style.getSourceAs<GeoJsonSource>(SOURCE_ID)?.setGeoJson(FeatureCollection.fromFeatures(emptyList()))
    }

    /** The time of the point nearest a tap at [point], within a small tolerance — Delete
     *  point's target — or null for a tap on none. */
    fun pointAt(map: MapLibreMap, point: LatLng, density: Float): Long? =
        pointAt(map, map.projection.toScreenLocation(point), density)

    /** The same for a touch at [screen] on the map view — Move point's press. */
    fun pointAt(map: MapLibreMap, screen: PointF, density: Float): Long? {
        val r = TAP_TOLERANCE_DP * density
        val hits = map.queryRenderedFeatures(RectF(screen.x - r, screen.y - r, screen.x + r, screen.y + r), POINTS_LAYER_ID)
        return hits.mapNotNull { hit ->
            val t = hit.getNumberProperty("t")?.toLong() ?: return@mapNotNull null
            val geometry = hit.geometry() as? Point ?: return@mapNotNull null
            val p: PointF = map.projection.toScreenLocation(LatLng(geometry.latitude(), geometry.longitude()))
            t to (p.x - screen.x) * (p.x - screen.x) + (p.y - screen.y) * (p.y - screen.y)
        }.minByOrNull { it.second }?.first
    }
}
