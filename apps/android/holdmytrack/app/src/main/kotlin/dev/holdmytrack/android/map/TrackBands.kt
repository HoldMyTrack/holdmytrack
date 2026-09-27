package dev.holdmytrack.android.map

import dev.holdmytrack.android.net.TrackMetricPoint
import kotlin.math.floor

/** The lowest and highest speed along one track — the bands are relative to the
 *  activity itself, not to any fixed zones. */
data class BandScale(val min: Double, val max: Double)

/** Points [startIndex]..[endIndex] (inclusive) all in [band]; neighbouring runs share their
 *  boundary point, so the coloured pieces join up without a gap. */
data class BandRun(val band: Int, val startIndex: Int, val endIndex: Int)

/**
 * The selected activity's pace bands — a port of the web's `apps/web/src/map/trackBands.ts`,
 * rule for rule (`docs/SPEC.md` FR-4.8): five colours from blue (slowest) to red (fastest),
 * over the activity's own range of speeds. Drawn by the map's band layer
 * (`MapOverlays.setTrackBands`).
 */
object TrackBands {

    const val COUNT = 5
    val COLORS = listOf("#2b6cb0", "#38a169", "#d69e2e", "#dd6b20", "#c53030")

    fun scale(points: List<TrackMetricPoint>): BandScale {
        val values = points.map { it.speedMps }
        if (values.isEmpty()) return BandScale(0.0, 1.0)
        return BandScale(values.min(), values.max())
    }

    fun bandIndex(value: Double, scale: BandScale): Int {
        val span = (scale.max - scale.min).takeIf { it != 0.0 } ?: 1.0
        return floor((value - scale.min) / span * COUNT).toInt().coerceIn(0, COUNT - 1)
    }

    /** Point *i*'s band comes from its own value, and its run starts at point *i − 1*: the
     *  stretch leading into a point is coloured by how fast it was covered. */
    fun runs(points: List<TrackMetricPoint>, scale: BandScale): List<BandRun> {
        val runs = mutableListOf<BandRun>()
        if (points.size < 2) return runs
        var current = -1
        var start = 0
        for (i in 1 until points.size) {
            val band = bandIndex(points[i].speedMps, scale)
            if (band != current) {
                if (current != -1 && i - 1 > start) runs += BandRun(current, start, i - 1)
                current = band
                start = i - 1
            }
        }
        if (points.size - 1 > start) runs += BandRun(current, start, points.size - 1)
        return runs
    }
}
