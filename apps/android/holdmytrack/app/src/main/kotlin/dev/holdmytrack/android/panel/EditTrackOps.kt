package dev.holdmytrack.android.panel

import dev.holdmytrack.android.net.TrackEdit
import dev.holdmytrack.android.net.TrackPoint
import kotlin.math.asin
import kotlin.math.cos
import kotlin.math.min
import kotlin.math.sin
import kotlin.math.sqrt

/** One step of a track edit session — what Undo takes back. */
sealed interface EditOp {
    /** Keep only [keep] (inclusive times). */
    data class Chop(val keep: Pair<Long, Long>) : EditOp

    /** Take [remove] (inclusive times) out and join its two ends. */
    data class Cut(val remove: Pair<Long, Long>) : EditOp

    /** Take the one point at [t] out. */
    data class Drop(val t: Long) : EditOp

    /** Put the one point at [t] at [lon]/[lat] instead. */
    data class Move(val t: Long, val lon: Double, val lat: Double) : EditOp

    /** Back to the track as recorded — itself a step, so Undo brings the edits back. */
    data object Reset : EditOp
}

/**
 * The track editor's rules, a port of the web's `apps/web/src/ui/editTrackOps.ts`
 * (`docs/SPEC.md` FR-5.14): the saved edit and the session's steps fold into one [TrackEdit],
 * which is what's sent, and replaying it over the recorded points is what's drawn. Points are
 * named by their time rather than their index, so a deleted point never shifts what an
 * earlier step meant.
 */
object EditTrackOps {

    fun fold(base: TrackEdit?, ops: List<EditOp>): TrackEdit {
        var keep = base?.keep
        val remove = base?.remove.orEmpty().toMutableList()
        val drop = base?.drop.orEmpty().toMutableList()
        val move = base?.move.orEmpty().toMutableMap()
        for (op in ops) {
            when (op) {
                EditOp.Reset -> {
                    keep = null
                    remove.clear()
                    drop.clear()
                    move.clear()
                }
                // Chop narrows what's already kept, never widens it.
                is EditOp.Chop -> keep = keep?.let { maxOf(it.first, op.keep.first) to minOf(it.second, op.keep.second) } ?: op.keep
                is EditOp.Cut -> remove += op.remove
                is EditOp.Drop -> drop += op.t
                // A point moved twice ends where it was let go last.
                is EditOp.Move -> move[op.t] = op.lon to op.lat
            }
        }
        return TrackEdit(keep, remove, drop, move)
    }

    fun apply(points: List<TrackPoint>, edit: TrackEdit): List<TrackPoint> {
        if (edit.isEmpty) return points
        val drop = edit.drop.toHashSet()
        return points.filter { p ->
            val keep = edit.keep
            if (keep != null && (p.t < keep.first || p.t > keep.second)) return@filter false
            if (p.t in drop) return@filter false
            edit.remove.none { (a, b) -> p.t in a..b }
        }.map { p -> edit.move[p.t]?.let { (lon, lat) -> p.copy(lon = lon, lat = lat) } ?: p }
    }

    /** The point at [t] let go at [lon]/[lat], rounded to the 1e-6° (~0.1 m) the server sends
     *  points in. */
    fun move(t: Long, lon: Double, lat: Double): EditOp {
        fun round(v: Double) = Math.round(v * 1e6) / 1e6
        return EditOp.Move(t, round(lon), round(lat))
    }

    /** Keep the knobs' stretch — nothing when the knobs are at both ends already, or would
     *  leave fewer than two points. */
    fun chop(visible: List<TrackPoint>, lo: Int, hi: Int): EditOp? {
        if (lo <= 0 && hi >= visible.size - 1) return null
        if (hi - lo < 1) return null
        return EditOp.Chop(visible[lo].t to visible[hi].t)
    }

    /** Remove what's strictly between the knobs, keeping both knob points and joining them —
     *  nothing when there's no point between them. */
    fun cut(visible: List<TrackPoint>, lo: Int, hi: Int): EditOp? {
        if (hi - lo < 2) return null
        return EditOp.Cut(visible[lo + 1].t to visible[hi - 1].t)
    }

    /** Distance along [points] to each one, in meters (haversine). */
    fun cumulativeDistances(points: List<TrackPoint>): DoubleArray {
        val out = DoubleArray(points.size)
        for (i in 1 until points.size) out[i] = out[i - 1] + haversine(points[i - 1], points[i])
        return out
    }

    private const val EARTH_RADIUS_M = 6371008.8

    private fun haversine(a: TrackPoint, b: TrackPoint): Double {
        val toRad = Math.PI / 180
        val dLat = (b.lat - a.lat) * toRad
        val dLon = (b.lon - a.lon) * toRad
        val h = sin(dLat / 2) * sin(dLat / 2) + cos(a.lat * toRad) * cos(b.lat * toRad) * sin(dLon / 2) * sin(dLon / 2)
        return 2 * EARTH_RADIUS_M * asin(min(1.0, sqrt(h)))
    }
}
