package dev.holdmytrack.android.map

import android.graphics.PointF
import android.graphics.RectF
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.Style
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.CircleLayer
import org.maplibre.android.style.layers.FillLayer
import org.maplibre.android.style.layers.Layer
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.layers.SymbolLayer
import org.maplibre.android.style.sources.GeoJsonSource
import org.maplibre.geojson.Feature
import org.maplibre.geojson.FeatureCollection
import org.maplibre.geojson.Point
import org.maplibre.geojson.Polygon
import kotlin.math.asin
import kotlin.math.atan2
import kotlin.math.cos
import kotlin.math.sin

/** A Private location's circle — a saved one ([id] set) or the one being drawn. */
data class Circle(val id: String?, val lon: Double, val lat: Double, val radiusM: Int)

/** What a tap on the map hit while the Privacy tab is up. */
sealed interface CircleHit {
    /** The circle being edited, or its handle. */
    data object Selected : CircleHit

    data class Saved(val id: String) : CircleHit

    data object Empty : CircleHit
}

/**
 * The Privacy tab's circles on the map — the web's `apps/web/src/map/privateLocations.ts`:
 * every saved one as a light purple fill with an outline and a small dot at its centre, and the
 * one being edited stronger, with a bigger handle at its centre to drag it by. Drawn from the
 * draft, not its saved copy, so the circle moves as it's edited. One GeoJSON source, four
 * layers filtered on a `role` property, under the basemap's labels.
 *
 * Circles are 64-step geodesic polygons ([polygon]) — the server's own haversine distance, so
 * what's drawn is what's clipped.
 */
object PrivateLocationsOverlay {

    private const val SOURCE_ID = "private-locations"
    private const val FILL_LAYER_ID = "private-locations-fill"
    private const val OUTLINE_LAYER_ID = "private-locations-outline"
    private const val CENTER_LAYER_ID = "private-locations-center"
    private const val HANDLE_LAYER_ID = "private-locations-handle"

    private const val COLOR = "#7a4bc2"
    private const val EARTH_RADIUS_M = 6371008.8
    private const val STEPS = 64

    /** Below this zoom Create flies in first — the web's `PRIVATE_LOCATIONS_MIN_PLACE_ZOOM`. */
    const val MIN_PLACE_ZOOM = 12.0

    /** How far from the handle a press still grabs it, and from a circle a tap still hits it. */
    private const val TOUCH_TOLERANCE_DP = 20f

    /** The circle as a closed ring of `[lon, lat]`, [STEPS] steps round. */
    fun polygon(c: Circle): List<Point> {
        val lat1 = Math.toRadians(c.lat)
        val lon1 = Math.toRadians(c.lon)
        val d = c.radiusM / EARTH_RADIUS_M
        return List(STEPS + 1) { i ->
            val bearing = 2 * Math.PI * i / STEPS
            val lat2 = asin(sin(lat1) * cos(d) + cos(lat1) * sin(d) * cos(bearing))
            val lon2 = lon1 + atan2(sin(bearing) * sin(d) * cos(lat1), cos(d) - sin(lat1) * sin(lat2))
            Point.fromLngLat(Math.toDegrees(lon2), Math.toDegrees(lat2))
        }
    }

    private fun ensure(style: Style) {
        if (style.getSource(SOURCE_ID) == null) style.addSource(GeoJsonSource(SOURCE_ID))
        val beforeId = style.layers.firstOrNull { it is SymbolLayer }?.id
        fun add(layer: Layer) {
            if (style.getLayer(layer.id) != null) return
            if (beforeId != null) style.addLayerBelow(layer, beforeId) else style.addLayer(layer)
        }
        fun role(name: String) = Expression.eq(Expression.get("role"), name)
        val selected = Expression.toBool(Expression.coalesce(Expression.get("selected"), Expression.literal(false)))
        add(
            FillLayer(FILL_LAYER_ID, SOURCE_ID).withFilter(role("circle")).withProperties(
                PropertyFactory.fillColor(COLOR),
                PropertyFactory.fillOpacity(Expression.switchCase(selected, Expression.literal(0.3f), Expression.literal(0.16f))),
            ),
        )
        add(
            LineLayer(OUTLINE_LAYER_ID, SOURCE_ID).withFilter(role("circle")).withProperties(
                PropertyFactory.lineColor(COLOR),
                PropertyFactory.lineWidth(Expression.switchCase(selected, Expression.literal(2.5f), Expression.literal(1.5f))),
            ),
        )
        add(
            CircleLayer(CENTER_LAYER_ID, SOURCE_ID).withFilter(role("center")).withProperties(
                PropertyFactory.circleRadius(5f),
                PropertyFactory.circleColor(COLOR),
                PropertyFactory.circleStrokeColor("#ffffff"),
                PropertyFactory.circleStrokeWidth(1.5f),
            ),
        )
        add(
            CircleLayer(HANDLE_LAYER_ID, SOURCE_ID).withFilter(role("handle")).withProperties(
                PropertyFactory.circleRadius(8f),
                PropertyFactory.circleColor(COLOR),
                PropertyFactory.circleStrokeColor("#ffffff"),
                PropertyFactory.circleStrokeWidth(2.5f),
            ),
        )
    }

    /** Draws [circles], with [selected] — the draft — in place of its saved copy. */
    fun set(style: Style, circles: List<Circle>, selected: Circle?) {
        ensure(style)
        val source = style.getSourceAs<GeoJsonSource>(SOURCE_ID) ?: return
        val features = mutableListOf<Feature>()
        fun add(c: Circle, isSelected: Boolean) {
            val id = c.id.orEmpty()
            features += Feature.fromGeometry(Polygon.fromLngLats(listOf(polygon(c)))).apply {
                addStringProperty("role", "circle")
                addStringProperty("id", id)
                addBooleanProperty("selected", isSelected)
            }
            features += Feature.fromGeometry(Point.fromLngLat(c.lon, c.lat)).apply {
                addStringProperty("role", if (isSelected) "handle" else "center")
                addStringProperty("id", id)
                addBooleanProperty("selected", isSelected)
            }
        }
        for (c in circles) if (selected == null || c.id == null || c.id != selected.id) add(c, isSelected = false)
        selected?.let { add(it, isSelected = true) }
        source.setGeoJson(FeatureCollection.fromFeatures(features))
    }

    fun clear(style: Style) {
        style.getSourceAs<GeoJsonSource>(SOURCE_ID)?.setGeoJson(FeatureCollection.fromFeatures(emptyList()))
    }

    /** Whether a press at [screen] grabs the handle — where a drag starts. */
    fun onHandle(map: MapLibreMap, screen: PointF, density: Float): Boolean =
        map.queryRenderedFeatures(box(screen, density), HANDLE_LAYER_ID).isNotEmpty()

    /** What a tap at [screen] hit — the web's click order: the handle, then a centre, then a
     *  fill, the circle being edited winning where they overlap. */
    fun hit(map: MapLibreMap, screen: PointF, density: Float): CircleHit {
        if (map.style?.getLayer(FILL_LAYER_ID) == null) return CircleHit.Empty
        val near = map.queryRenderedFeatures(box(screen, density), HANDLE_LAYER_ID, CENTER_LAYER_ID)
        val inside = map.queryRenderedFeatures(screen, FILL_LAYER_ID)
        val hits = near + inside
        if (hits.any { it.getBooleanProperty("selected") == true }) return CircleHit.Selected
        val id = hits.firstNotNullOfOrNull { it.getStringProperty("id")?.takeIf(String::isNotEmpty) }
        return if (id != null) CircleHit.Saved(id) else CircleHit.Empty
    }

    private fun box(screen: PointF, density: Float): RectF {
        val r = TOUCH_TOLERANCE_DP * density
        return RectF(screen.x - r, screen.y - r, screen.x + r, screen.y + r)
    }
}
