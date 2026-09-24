package dev.holdmytrack.android.recording.db

import dev.holdmytrack.android.recording.RecordedPoint
import java.time.Instant
import org.json.JSONArray
import org.json.JSONObject

/** Sync state a locally recorded activity moves through. Deliberately two values, not more
 *  — a successful submit deletes the row from the device (the activity now lives on the
 *  server), and a failed one simply leaves it [QUEUED] rather than introducing a "failed"
 *  state, so the next "Sync Now" retries it without the user having to notice or re-check
 *  anything (the same resumable-retry posture Health Connect sync already has). */
object SyncStatus {
    const val NOT_SYNCED = "not_synced"
    const val QUEUED = "queued"
}

/**
 * One GPS recording, persisted locally by `RecordingService` the moment it stops rather than
 * submitted immediately — submission is a separate, explicit step (`RecordedActivitiesActivity`'s
 * sync checkbox plus the Sync screen's existing "Sync Now", extended to also walk rows here
 * marked [SyncStatus.QUEUED]), after which the row is deleted from the device.
 *
 * [id] is the same client-generated UUID used as `external_id` on the wire
 * (`docs/IMPLEMENTATION.md` §4.0.4) — minted once at Stop, stable through edits and retries.
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
    val syncStatus: String,
    val createdAtMs: Long,
)

/** The exact shape `POST /v1/sync/activities` accepts for one batch entry
 *  (`docs/IMPLEMENTATION.md` §4.0.3/§4.0.4). */
fun RecordedActivityRecord.toSyncJson(): JSONObject {
    val pointsArray = JSONArray()
    for (point in points) {
        val json = JSONObject()
            .put("lat", point.lat)
            .put("lon", point.lon)
            .put("time", point.time.toString())
        point.elevationM?.let { json.put("elevation_m", it) }
        pointsArray.put(json)
    }
    return JSONObject()
        .put("external_id", id)
        .put("activity_type", activityType)
        .put("name", name)
        .put("description", description)
        .put("points", pointsArray)
}

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

/** `points_json`'s wire format on disk — an array of `{lat, lon, elevation_m?, time}`,
 *  identical to the points array [toSyncJson] sends, so persisting and submitting never
 *  disagree about what a point is. */
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
