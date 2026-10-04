package dev.holdmytrack.android.timeline

import java.time.OffsetDateTime
import kotlin.math.asin
import kotlin.math.cos
import kotlin.math.min
import kotlin.math.sin
import kotlin.math.sqrt

/**
 * A Google Maps Timeline export turned into the activities the import sends — the web's
 * `apps/web/src/timeline/reader.ts`, rule for rule (root `docs/IMPLEMENTATION.md` §4.0.5), so a
 * file imported from the phone becomes exactly the activities the website would make of it, and
 * importing it on both sends nothing twice.
 *
 * The export is undocumented. `semanticSegments` holds visits (skipped), activities (a start, an
 * end, a guessed mode and a distance, but no route) and `timelinePath` buckets (the points the
 * phone kept, which don't line up with activities). An activity's route is the path points
 * inside its time span, between its own start and end. [TimelineFile] streams the JSON into the
 * raw entries this takes, so this part runs on the JVM's own tests.
 */
data class TimelinePoint(val lat: Double, val lon: Double, val time: String)

data class TimelineSegment(
    /** `seg-<start>-<end>` in epoch seconds: the same segment in a later export keeps it. */
    val id: String,
    /** The export's own timestamps, with the offset of where it happened. */
    val start: String,
    val end: String,
    /** The local date it started on, YYYY-MM-DD. */
    val day: String,
    /** Timeline's mode, `WALKING`, `IN_PASSENGER_VEHICLE`… */
    val mode: String,
    /** The activity_type it's imported as ([TimelineReader.activityType]). */
    val type: String,
    val distanceM: Double,
    val points: List<TimelinePoint>,
)

data class TimelineRead(
    val segments: List<TimelineSegment>,
    /** Activities with fewer than two distinct places: nothing to draw. */
    val skipped: Int,
)

/** The other location exports Google has written, recognized so the error can name them. */
enum class TimelineFormat { IOS, TAKEOUT }

class TimelineFormatException(val format: TimelineFormat?) :
    Exception(if (format != null) "unsupported Timeline format: $format" else "not a Timeline export")

/** One `timelinePath` entry as it was in the file. */
data class RawPathPoint(val point: String?, val time: String?)

/** One `semanticSegments` activity as it was in the file: its times, its `start`/`end`
 *  `latLng`s and its `topCandidate.type`. */
data class RawActivity(val startTime: String?, val endTime: String?, val from: String?, val to: String?, val mode: String?)

data class ModeSummary(val mode: String, val type: String, val count: Int, val distanceM: Double)

object TimelineReader {

    /** Modes left unticked until chosen: a flight drawn as a line would clear fog across a continent. */
    val MODES_OFF_BY_DEFAULT: Set<String> = setOf("FLYING")

    private val TYPES = mapOf(
        "WALKING" to "walking",
        "RUNNING" to "running",
        "CYCLING" to "cycling",
        "IN_PASSENGER_VEHICLE" to "driving",
        "IN_VEHICLE" to "driving",
        "UNKNOWN_ACTIVITY_TYPE" to "unknown",
    )

    /** Timeline's mode as an activity_type: the names the app's other sources use where there is
     *  one, otherwise the mode in lower case without its `IN_` (`IN_BUS` → `bus`). */
    fun activityType(mode: String): String = TYPES[mode] ?: mode.lowercase().removePrefix("in_")

    /** "41.3929419°, -81.7433579°" (also tolerating a `geo:` prefix or no degree signs). */
    fun parseLatLng(s: String?): Pair<Double, Double>? {
        if (s == null) return null
        val parts = s.removePrefix("geo:").replace("°", "").split(',')
        if (parts.size != 2) return null
        val lat = parts[0].trim().toDoubleOrNull() ?: return null
        val lon = parts[1].trim().toDoubleOrNull() ?: return null
        if (!lat.isFinite() || !lon.isFinite() || kotlin.math.abs(lat) > 90 || kotlin.math.abs(lon) > 180) return null
        return lat to lon
    }

    private class Timed(val ms: Long, val time: String, val lat: Double, val lon: Double)

    private fun epochMs(time: String?): Long? =
        time?.let { runCatching { OffsetDateTime.parse(it).toInstant().toEpochMilli() }.getOrNull() }

    /** Index of the first entry with ms > [t]. */
    private fun upperBound(path: List<Timed>, t: Long): Int {
        var lo = 0
        var hi = path.size
        while (lo < hi) {
            val mid = (lo + hi) ushr 1
            if (path[mid].ms <= t) lo = mid + 1 else hi = mid
        }
        return lo
    }

    private const val EARTH_M = 6371008.8

    private fun metersBetween(a: TimelinePoint, b: TimelinePoint): Double {
        val rad = Math.PI / 180
        val dLat = (b.lat - a.lat) * rad
        val dLon = (b.lon - a.lon) * rad
        val h = sin(dLat / 2) * sin(dLat / 2) + cos(a.lat * rad) * cos(b.lat * rad) * sin(dLon / 2) * sin(dLon / 2)
        return 2 * EARTH_M * asin(min(1.0, sqrt(h)))
    }

    fun read(pathPoints: List<RawPathPoint>, activities: List<RawActivity>): TimelineRead {
        val path = pathPoints.mapNotNull { p ->
            val at = parseLatLng(p.point) ?: return@mapNotNull null
            val ms = epochMs(p.time) ?: return@mapNotNull null
            Timed(ms, p.time!!, at.first, at.second)
        }.sortedBy { it.ms }

        val segments = mutableListOf<TimelineSegment>()
        var skipped = 0
        for (act in activities) {
            val startMs = epochMs(act.startTime)
            val endMs = epochMs(act.endTime)
            val from = parseLatLng(act.from)
            val to = parseLatLng(act.to)
            if (startMs == null || endMs == null || endMs <= startMs || from == null || to == null) {
                skipped++
                continue
            }
            val lo = upperBound(path, startMs)
            val inside = path.subList(lo, maxOf(lo, upperBound(path, endMs - 1)))
            val timed = listOf(Timed(startMs, act.startTime!!, from.first, from.second)) + inside +
                Timed(endMs, act.endTime!!, to.first, to.second)
            // Strictly increasing times: the parser keeps one point per instant anyway.
            val points = mutableListOf<TimelinePoint>()
            var last = Long.MIN_VALUE
            for (p in timed) {
                if (p.ms <= last) continue
                last = p.ms
                points += TimelinePoint(p.lat, p.lon, p.time)
            }
            if (points.map { it.lat to it.lon }.toSet().size < 2) {
                skipped++
                continue
            }
            var distanceM = 0.0
            for (i in 1 until points.size) distanceM += metersBetween(points[i - 1], points[i])
            val mode = act.mode?.takeIf { it.isNotEmpty() } ?: "UNKNOWN_ACTIVITY_TYPE"
            segments += TimelineSegment(
                id = "seg-${Math.floorDiv(startMs, 1000L)}-${Math.floorDiv(endMs, 1000L)}",
                start = act.startTime,
                end = act.endTime,
                day = act.startTime.take(10),
                mode = mode,
                type = activityType(mode),
                distanceM = distanceM,
                points = points,
            )
        }
        segments.sortBy { epochMs(it.start) }
        return TimelineRead(segments, skipped)
    }

    /** The segments a selection imports: started on a day within [from]–[to] (YYYY-MM-DD), in a
     *  chosen mode. */
    fun select(segments: List<TimelineSegment>, from: String, to: String, modes: Set<String>): List<TimelineSegment> =
        segments.filter { it.day >= from && it.day <= to && it.mode in modes }

    /** One row per mode, most activities first. */
    fun summarize(segments: List<TimelineSegment>): List<ModeSummary> =
        segments.groupBy { it.mode }
            .map { (mode, list) -> ModeSummary(mode, list.first().type, list.size, list.sumOf { it.distanceM }) }
            .sortedWith(compareByDescending<ModeSummary> { it.count }.thenBy { it.mode })

    /** The sync endpoint's activities, [size] to a request (its own limit is 100). */
    fun batches(segments: List<TimelineSegment>, size: Int = 100): List<List<TimelineSegment>> = segments.chunked(size)
}
