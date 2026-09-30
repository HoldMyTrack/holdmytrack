package dev.holdmytrack.android.map

import kotlin.math.PI
import kotlin.math.atan2
import kotlin.math.cos
import kotlin.math.hypot
import kotlin.math.sin

/** A position in degrees. MapLibre's own `LatLng` is an Android class; this one keeps the
 *  geometry below testable on the JVM. */
data class GeoPoint(val lat: Double, val lon: Double)

/** A spot's area as `GET /v1/spots/{id}` sends it: polygons, each an outer ring then its holes,
 *  each ring closed (first point repeated last). */
typealias SpotArea = List<List<List<GeoPoint>>>

/**
 * What capture mode measures a position against (`apps/android/docs/IMPLEMENTATION.md` §3.3):
 * inside a spot's area or not, how far from it, and which way. A spot is metres to hundreds of
 * metres across, so each measure is taken on a flat projection centred on the position —
 * equirectangular, x scaled by cos(latitude) — which is well under a metre off at that size.
 */
object SpotGeometry {

    private const val EARTH_RADIUS_M = 6_371_008.8

    /** Inside the outer ring of one of the polygons and none of its holes. */
    fun contains(area: SpotArea, point: GeoPoint): Boolean =
        area.any { polygon ->
            polygon.isNotEmpty() && inRing(polygon[0], point) && polygon.drop(1).none { inRing(it, point) }
        }

    /** Metres from [point] to the area's nearest edge, 0 inside it. */
    fun distanceM(area: SpotArea, point: GeoPoint): Double =
        if (contains(area, point)) 0.0 else nearest(area, point)?.second ?: Double.POSITIVE_INFINITY

    /** The point on the area's edge nearest [point] — where the guide line runs to. Null for an
     *  empty area. */
    fun nearestPoint(area: SpotArea, point: GeoPoint): GeoPoint? = nearest(area, point)?.first

    /** The initial bearing from [from] to [to], degrees clockwise from north, 0 until 360. */
    fun bearing(from: GeoPoint, to: GeoPoint): Double {
        val lat1 = Math.toRadians(from.lat)
        val lat2 = Math.toRadians(to.lat)
        val dLon = Math.toRadians(to.lon - from.lon)
        val y = sin(dLon) * cos(lat2)
        val x = cos(lat1) * sin(lat2) - sin(lat1) * cos(lat2) * cos(dLon)
        return (Math.toDegrees(atan2(y, x)) + 360) % 360
    }

    /** Even-odd ray casting, on raw degrees: fine for a ring that doesn't cross the antimeridian. */
    private fun inRing(ring: List<GeoPoint>, point: GeoPoint): Boolean {
        var inside = false
        var j = ring.lastIndex
        for (i in ring.indices) {
            val a = ring[i]
            val b = ring[j]
            if ((a.lat > point.lat) != (b.lat > point.lat) &&
                point.lon < (b.lon - a.lon) * (point.lat - a.lat) / (b.lat - a.lat) + a.lon
            ) inside = !inside
            j = i
        }
        return inside
    }

    /** The nearest point on any ring's edge, and its distance in metres. */
    private fun nearest(area: SpotArea, point: GeoPoint): Pair<GeoPoint, Double>? {
        val mPerLat = EARTH_RADIUS_M * PI / 180
        val mPerLon = mPerLat * cos(Math.toRadians(point.lat))
        fun x(p: GeoPoint) = (p.lon - point.lon) * mPerLon
        fun y(p: GeoPoint) = (p.lat - point.lat) * mPerLat
        var best: Pair<GeoPoint, Double>? = null
        for (ring in area.flatten()) {
            for (i in 0 until ring.size - 1) {
                val ax = x(ring[i]); val ay = y(ring[i])
                val bx = x(ring[i + 1]); val by = y(ring[i + 1])
                val dx = bx - ax; val dy = by - ay
                val lengthSq = dx * dx + dy * dy
                // The origin is the position itself: project it onto the segment, clamped to it.
                val t = if (lengthSq == 0.0) 0.0 else (-(ax * dx + ay * dy) / lengthSq).coerceIn(0.0, 1.0)
                val px = ax + t * dx
                val py = ay + t * dy
                val d = hypot(px, py)
                if (best == null || d < best.second) {
                    best = GeoPoint(lat = point.lat + py / mPerLat, lon = point.lon + px / mPerLon) to d
                }
            }
        }
        return best
    }
}
