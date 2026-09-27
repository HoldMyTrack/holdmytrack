package dev.holdmytrack.android.recording.db

import android.content.ContentValues
import android.content.Context
import dev.holdmytrack.android.recording.RecordedPoint
import java.time.Instant

/**
 * The recording in progress, written as it happens, so a process death mid-recording — a
 * low-memory kill, a crash, Force stop — leaves it on disk rather than taking it with
 * `RecordingService`'s in-memory list (`apps/android/docs/IMPLEMENTATION.md` §7.9). One row in
 * `live_recording` for the totals, one row per fix in `live_points`: a fix is an append, not a
 * rewrite of the whole track, which over a multi-hour recording `points_json` would be.
 *
 * At most one recording is ever in the journal, and it's there only while one is in progress:
 * Stop moves it into `recorded_activities` and clears the journal in the same transaction
 * ([finish]), so there is no moment when a stopped recording is in neither place. A journal row
 * with no recording running in this process is therefore always a leftover from one that died.
 *
 * Every call blocks — `RecordingService` makes them all from one single-threaded dispatcher,
 * which is also what keeps them in order.
 */
class LiveRecordingJournal(context: Context) {

    private val helper = RecordingDbHelper(context)

    /** What a process death left behind: the account it was recorded under, the moving time
     *  and distance as of its last fix or pause, and every point recorded. */
    data class Leftover(
        val id: String,
        val account: String,
        val movingMs: Long,
        val distanceM: Double,
        val points: List<RecordedPoint>,
    )

    fun begin(id: String, account: String) {
        val values = ContentValues().apply {
            put("id", id)
            put("account", account)
            put("moving_ms", 0L)
            put("distance_meters", 0.0)
        }
        helper.writableDatabase.insertOrThrow(RecordingDbHelper.LIVE_TABLE, null, values)
    }

    /** One fix, with the totals it brings the recording to, in one commit. */
    fun append(id: String, seq: Int, point: RecordedPoint, movingMs: Long, distanceM: Double) {
        val db = helper.writableDatabase
        db.beginTransaction()
        try {
            val values = ContentValues().apply {
                put("recording_id", id)
                put("seq", seq)
                put("lat", point.lat)
                put("lon", point.lon)
                point.elevationM?.let { put("elevation_m", it) }
                put("time_ms", point.time.toEpochMilli())
            }
            db.insertOrThrow(RecordingDbHelper.LIVE_POINTS_TABLE, null, values)
            updateTotals(id, movingMs, distanceM)
            db.setTransactionSuccessful()
        } finally {
            db.endTransaction()
        }
    }

    /** On pause — the moving time run up since the last fix would otherwise be lost with it. */
    fun updateTotals(id: String, movingMs: Long, distanceM: Double) {
        val values = ContentValues().apply {
            put("moving_ms", movingMs)
            put("distance_meters", distanceM)
        }
        helper.writableDatabase.update(RecordingDbHelper.LIVE_TABLE, values, "id = ?", arrayOf(id))
    }

    fun leftover(): Leftover? {
        val db = helper.readableDatabase
        val head = db.query(RecordingDbHelper.LIVE_TABLE, null, null, null, null, null, null, "1").use {
            if (!it.moveToFirst()) return null
            Leftover(
                id = it.getString(it.getColumnIndexOrThrow("id")),
                account = it.getString(it.getColumnIndexOrThrow("account")),
                movingMs = it.getLong(it.getColumnIndexOrThrow("moving_ms")),
                distanceM = it.getDouble(it.getColumnIndexOrThrow("distance_meters")),
                points = emptyList(),
            )
        }
        val points = db.query(
            RecordingDbHelper.LIVE_POINTS_TABLE, null, "recording_id = ?", arrayOf(head.id), null, null, "seq",
        ).use { cursor ->
            val lat = cursor.getColumnIndexOrThrow("lat")
            val lon = cursor.getColumnIndexOrThrow("lon")
            val elevation = cursor.getColumnIndexOrThrow("elevation_m")
            val time = cursor.getColumnIndexOrThrow("time_ms")
            buildList {
                while (cursor.moveToNext()) {
                    add(
                        RecordedPoint(
                            lat = cursor.getDouble(lat),
                            lon = cursor.getDouble(lon),
                            elevationM = if (cursor.isNull(elevation)) null else cursor.getFloat(elevation),
                            time = Instant.ofEpochMilli(cursor.getLong(time)),
                        ),
                    )
                }
            }
        }
        return head.copy(points = points)
    }

    /** Ends recording [id]: saves [record] under [account] when there is one (a recording too
     *  short to keep has none) and clears the journal, both in one transaction. */
    fun finish(id: String, record: RecordedActivityRecord?, account: String) {
        val db = helper.writableDatabase
        db.beginTransaction()
        try {
            record?.let { db.insertOrThrow(RecordingDbHelper.TABLE, null, it.toContentValues(account)) }
            db.delete(RecordingDbHelper.LIVE_POINTS_TABLE, "recording_id = ?", arrayOf(id))
            db.delete(RecordingDbHelper.LIVE_TABLE, "id = ?", arrayOf(id))
            db.setTransactionSuccessful()
        } finally {
            db.endTransaction()
        }
    }
}
