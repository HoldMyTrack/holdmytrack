package dev.fitmap.android.recording.db

import dev.fitmap.android.recording.RecordedPoint
import java.time.Instant
import org.json.JSONArray
import org.json.JSONObject

/** Sync state a locally recorded activity moves through. Deliberately three values, not more
 *  — a failed submit simply leaves a row [QUEUED] rather than introducing a fourth "failed"
 *  state, so the next "Sync Now" retries it without the user having to notice or re-check
 *  anything (the same resumable-retry posture Health Connect sync already has). */
object SyncStatus {
    const val NOT_SYNCED = "not_synced"
    const val QUEUED = "queued"
    const val SYNCED = "synced"
}

/**
 * One GPS Logger recording, persisted locally the moment Stop is pressed rather than
 * submitted immediately — submission is now a separate, explicit step (`RecordedActivitiesActivity`'s
 * sync checkbox plus the Sync screen's existing "Sync Now", extended to also walk rows here
 * marked [SyncStatus.QUEUED]).
 *
 * [id] is the same client-generated UUID used as `external_id` on the wire
 * (`docs/IMPLEMENTATION.md` §4.0.4) — minted once at Record, stable through edits and retries.
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
) {
    val editable: Boolean get() = syncStatus != SyncStatus.SYNCED
}

/** The exact shape `POST /v1/sync/activities` accepts for one batch entry
 *  (`docs/IMPLEMENTATION.md` §4.0.3/§4.0.4) — reused unchanged from what `RecordingActivity`
 *  used to build inline before submission became deferred. */
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
