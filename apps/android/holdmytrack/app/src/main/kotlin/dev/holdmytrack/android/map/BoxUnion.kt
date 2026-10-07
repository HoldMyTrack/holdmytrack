package dev.holdmytrack.android.map

import kotlin.math.floor

/**
 * The smallest box (`[west, south, east, north]`) containing all of [boxes], or null for none —
 * the web's `unionBBox` (`apps/web/src/map/bbox.ts`). Longitude goes the shorter way round, as
 * each activity's own bbox does (`docs/IMPLEMENTATION.md` §4.7): New Zealand and Hawaii make a
 * box across the Pacific, east past 180, which `LatLngBounds.from` takes as it is. Each box's
 * west edge is tried as the start, every box moved to begin at or after it; the narrowest wins.
 */
object BoxUnion {
    fun of(boxes: List<List<Double>>): DoubleArray? {
        if (boxes.isEmpty()) return null
        // Each box's west within −180…180, its width kept: east may pass 180.
        val arcs = boxes.map { b ->
            val shift = 360 * floor((b[0] + 180) / 360)
            b[0] - shift to b[2] - shift
        }
        var best = -180.0 to 180.0
        for ((start, _) in arcs) {
            var end = start
            for ((west, east) in arcs) end = maxOf(end, if (west < start) east + 360 else east)
            if (end - start < best.second - best.first) best = start to end
        }
        return doubleArrayOf(best.first, boxes.minOf { it[1] }, best.second, boxes.maxOf { it[3] })
    }

    /** A view's west and east edges as `GET /v1/spots` takes them. Bounds the map gives as
     *  more than 180° wide, at the zooms Show in this area works at, are a view across the
     *  antimeridian reported the long way round: the edges are swapped, east past 180. */
    fun viewEdges(west: Double, east: Double): Pair<Double, Double> =
        if (east - west > 180) east to west + 360 else west to east
}
