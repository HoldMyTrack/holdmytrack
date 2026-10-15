package dev.holdmytrack.android.recording.db

import dev.holdmytrack.android.net.SyncActivityJson
import dev.holdmytrack.android.net.SyncPointJson
import dev.holdmytrack.android.recording.RecordedPoint
import java.time.Instant
import org.json.JSONArray
import org.json.JSONObject

/**
 * One GPS recording, persisted locally by `RecordingService` the moment it stops rather than
 * submitted immediately — submission is a separate, explicit step (ticked on the Sync tab and
 * sent), after which the row is deleted from the device. A failed submit leaves the row as it
 * is, still listed, to be sent again.
 *
 * [id] is the same client-generated UUID used as `external_id` on the wire
 * (`docs/IMPLEMENTATION.md` §4.0.4) — minted once at Start (it keys the recording's journal,
 * `LiveRecordingJournal`, too), stable through edits and retries.
 */
data class RecordedActivityRecord(
    val id: String,
    val name: String,
    val description: String,
    val activityType: String,
    val startedAtMs: Long,
    val distanceMeters: Double,
    val durationSeconds: Long,
    val points: List<RecordedPoint>,
    val createdAtMs: Long,
)

/** The recording as one batch entry of `POST /v1/sync/activities` (`docs/IMPLEMENTATION.md`
 *  §4.0.3/§4.0.4). */
internal fun RecordedActivityRecord.toSyncActivity() = SyncActivityJson(
    externalId = id,
    activityType = activityType,
    name = name,
    description = description,
    points = points.map { SyncPointJson(lat = it.lat, lon = it.lon, elevationM = it.elevationM?.toDouble(), time = it.time.toString()) },
)

/**
 * The recording as a GPX 1.1 file — `RecordingActivity`'s Download. GPX rather than the sync
 * payload's JSON because it is what every other mapping tool opens, and HoldMyTrack's own upload
 * accepts it too, so a downloaded track can come straight back in. One `<trkseg>`: a pause
 * leaves no gap in [points] to split on, since nothing is recorded while paused.
 */
fun RecordedActivityRecord.toGpx(): String = buildString {
    append("""<?xml version="1.0" encoding="UTF-8"?>""").append('\n')
    append("""<gpx version="1.1" creator="HoldMyTrack" xmlns="http://www.topografix.com/GPX/1/1">""").append('\n')
    append("  <trk>\n")
    if (name.isNotBlank()) append("    <name>").append(xmlEscape(name)).append("</name>\n")
    if (description.isNotBlank()) append("    <desc>").append(xmlEscape(description)).append("</desc>\n")
    append("    <type>").append(xmlEscape(activityType)).append("</type>\n")
    append("    <trkseg>\n")
    for (point in points) {
        append("""      <trkpt lat="${point.lat}" lon="${point.lon}">""")
        point.elevationM?.let { append("<ele>").append(it).append("</ele>") }
        append("<time>").append(point.time).append("</time></trkpt>\n")
    }
    append("    </trkseg>\n")
    append("  </trk>\n")
    append("</gpx>\n")
}

private fun xmlEscape(text: String): String =
    text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace("\"", "&quot;")

/** `points_json`'s format on disk — an array of `{lat, lon, elevation_m?, time}`, the same
 *  fields as the points [toSyncActivity] sends, so persisting and submitting never disagree
 *  about what a point is. */
internal fun List<RecordedPoint>.toJson(): String {
    val array = JSONArray()
    for (point in this) {
        val json = JSONObject()
            .put("lat", point.lat)
            .put("lon", point.lon)
            .put("time", point.time.toString())
        point.elevationM?.let { json.put("elevation_m", it) }
        array.put(json)
    }
    return array.toString()
}

internal fun String.toRecordedPoints(): List<RecordedPoint> {
    val array = JSONArray(this)
    return List(array.length()) { i ->
        val json = array.getJSONObject(i)
        RecordedPoint(
            lat = json.getDouble("lat"),
            lon = json.getDouble("lon"),
            elevationM = if (json.has("elevation_m")) json.getDouble("elevation_m").toFloat() else null,
            time = Instant.parse(json.getString("time")),
        )
    }
}
