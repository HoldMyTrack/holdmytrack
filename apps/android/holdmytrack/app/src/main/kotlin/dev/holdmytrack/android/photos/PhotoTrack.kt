package dev.holdmytrack.android.photos

import kotlin.math.asin
import kotlin.math.cos
import kotlin.math.min
import kotlin.math.roundToLong
import kotlin.math.sin
import kotlin.math.sqrt

/** A display point with its moment ([t], epoch seconds). */
data class TimedPoint(val lon: Double, val lat: Double, val t: Long)

/**
 * A track as the Photos tab's slider walks it, the web's `apps/web/src/ui/photoTrack.ts`
 * (`docs/SPEC.md` FR-16.6): the display points with their moments
 * (`GET /v1/activities/track-metrics/{id}`'s `time_s`), measured along their length. The slider
 * runs by distance, not time, so a long stop doesn't take up half its travel; a photo's place is
 * still kept as a moment (`route_at`), which is what the server stores.
 */
class PhotoTrack(val points: List<TimedPoint>) {

    /** Metres from the first point to each point. */
    val along: DoubleArray = DoubleArray(points.size).also { along ->
        for (i in 1 until points.size) along[i] = along[i - 1] + haversineM(points[i - 1], points[i])
    }

    /** The place [fraction] (0–1) of the way along the track, with its moment. */
    fun pointAt(fraction: Double): TimedPoint {
        require(points.isNotEmpty()) { "empty track" }
        val f = fraction.coerceIn(0.0, 1.0)
        val total = along.last()
        if (points.size == 1 || total == 0.0) {
            // No length to walk: go by time instead, from the first moment to the last.
            val first = points.first()
            val last = points.last()
            return first.copy(t = (first.t + (last.t - first.t) * f).roundToLong())
        }
        val target = f * total
        var i = 1
        while (i < points.size - 1 && along[i] < target) i++
        val a = points[i - 1]
        val b = points[i]
        val span = along[i] - along[i - 1]
        val k = if (span > 0) (target - along[i - 1]) / span else 0.0
        return TimedPoint(a.lon + (b.lon - a.lon) * k, a.lat + (b.lat - a.lat) * k, (a.t + (b.t - a.t) * k).roundToLong())
    }

    /** How far along the track (0–1) the moment [t] is — the inverse of [pointAt]. A moment
     *  before or after the track is at its end. */
    fun fractionAt(t: Long): Double {
        if (points.size < 2) return 0.0
        if (t <= points.first().t) return 0.0
        if (t >= points.last().t) return 1.0
        var i = 1
        while (i < points.size - 1 && points[i].t < t) i++
        val a = points[i - 1]
        val b = points[i]
        val k = if (b.t > a.t) (t - a.t).toDouble() / (b.t - a.t) else 0.0
        val total = along.last()
        if (total == 0.0) return (t - points.first().t).toDouble() / (points.last().t - points.first().t)
        return (along[i - 1] + (along[i] - along[i - 1]) * k) / total
    }

    /**
     * Where a waiting photo's slider starts (FR-16.6): just after the photo picked before it,
     * which is where the next picture of a walk usually is. A previous one that waited is
     * followed by where the user put it ([placed]), or past it to its own predecessor if it was
     * removed (null there). The first of a batch, or one after nothing placed at all, starts at
     * the beginning of the route.
     */
    fun startFraction(anchor: PlaceAnchor?, placed: Map<String, Double?>, anchors: Map<String, PlaceAnchor?>): Double {
        var current = anchor
        var seen = 0
        while (current != null && seen <= anchors.size) {
            when (current) {
                is PlaceAnchor.At -> return min(1.0, fractionAt(current.t) + NEXT_PHOTO_STEP)
                is PlaceAnchor.Waiting -> {
                    val at = placed[current.key]
                    if (at != null) return min(1.0, at + NEXT_PHOTO_STEP)
                    current = anchors[current.key]
                }
            }
            seen++
        }
        return 0.0
    }

    companion object {
        /** How far past the previous photo a waiting one's slider starts: just after it, so
         *  photos picked in the order they were taken walk forward along the route. */
        const val NEXT_PHOTO_STEP = 0.01

        private const val EARTH_RADIUS_M = 6_371_008.8

        private fun haversineM(a: TimedPoint, b: TimedPoint): Double {
            val rad = Math.PI / 180
            val dLat = (b.lat - a.lat) * rad
            val dLon = (b.lon - a.lon) * rad
            val h = sin(dLat / 2).let { it * it } + cos(a.lat * rad) * cos(b.lat * rad) * sin(dLon / 2).let { it * it }
            return 2 * EARTH_RADIUS_M * asin(min(1.0, sqrt(h)))
        }
    }
}

/** What a photo waiting for a place was picked after: a photo the server placed (its moment),
 *  or one that waited too (its key, placed or removed since). Null is nothing — the first of
 *  its batch. */
sealed interface PlaceAnchor {
    data class At(val t: Long) : PlaceAnchor
    data class Waiting(val key: String) : PlaceAnchor
}
