package dev.holdmytrack.android.map

import kotlin.math.cos
import kotlin.math.hypot

/**
 * Douglas–Peucker over a route, in metres: the points a line drawn on the map needs, not the
 * one-a-second thousands a phone records. For drawing only — what is sent is always the route as
 * recorded. Distances are measured on a local equirectangular plane around the route's first
 * point, close enough over the extent of one activity.
 */
object Simplify {

    /** Under this from the line between its neighbours' kept points, a point is dropped. */
    const val TOLERANCE_M = 15.0

    /** At most this many points come back: past it, the tolerance doubles until they fit. */
    const val MAX_POINTS = 500

    /** The indexes of the points of ([lats], [lons]) to keep, first and last always among them. */
    fun indexes(lats: DoubleArray, lons: DoubleArray, toleranceM: Double = TOLERANCE_M, maxPoints: Int = MAX_POINTS): IntArray {
        val n = lats.size
        if (n <= 2) return IntArray(n) { it }
        val metresPerDegree = 111_320.0
        val kx = metresPerDegree * cos(Math.toRadians(lats[0]))
        val xs = DoubleArray(n) { (lons[it] - lons[0]) * kx }
        val ys = DoubleArray(n) { (lats[it] - lats[0]) * metresPerDegree }
        var tolerance = toleranceM
        while (true) {
            val kept = run(xs, ys, tolerance)
            if (kept.size <= maxPoints) return kept
            tolerance *= 2
        }
    }

    /** Iterative, so a long route can't overflow the stack. */
    private fun run(xs: DoubleArray, ys: DoubleArray, tolerance: Double): IntArray {
        val n = xs.size
        val keep = BooleanArray(n)
        keep[0] = true
        keep[n - 1] = true
        val stack = ArrayDeque<Pair<Int, Int>>()
        stack.addLast(0 to n - 1)
        while (stack.isNotEmpty()) {
            val (first, last) = stack.removeLast()
            var farthest = -1
            var farthestDistance = tolerance
            for (i in first + 1 until last) {
                val d = distanceToSegment(xs[i], ys[i], xs[first], ys[first], xs[last], ys[last])
                if (d > farthestDistance) {
                    farthest = i
                    farthestDistance = d
                }
            }
            if (farthest < 0) continue
            keep[farthest] = true
            stack.addLast(first to farthest)
            stack.addLast(farthest to last)
        }
        return keep.indices.filter { keep[it] }.toIntArray()
    }

    private fun distanceToSegment(px: Double, py: Double, ax: Double, ay: Double, bx: Double, by: Double): Double {
        val dx = bx - ax
        val dy = by - ay
        val lengthSquared = dx * dx + dy * dy
        if (lengthSquared == 0.0) return hypot(px - ax, py - ay)
        val t = (((px - ax) * dx + (py - ay) * dy) / lengthSquared).coerceIn(0.0, 1.0)
        return hypot(px - (ax + t * dx), py - (ay + t * dy))
    }
}
