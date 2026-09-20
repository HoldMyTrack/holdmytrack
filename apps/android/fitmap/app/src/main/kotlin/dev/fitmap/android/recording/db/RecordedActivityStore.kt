package dev.fitmap.android.recording.db

import android.content.ContentValues
import android.content.Context
import android.database.Cursor
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Plain `SQLiteOpenHelper`, not Room: this project's Kotlin toolchain (AGP's built-in Kotlin,
 * 2.4.0) is newer than any published KSP release at the time this was written, so Room's
 * annotation processor cannot run here — confirmed by trying it, not assumed. A hand-written
 * table is a handful of queries and no new build-tool dependency, which is what this app
 * already prefers where a platform API covers the need (`LocationManager` over Play Services'
 * `FusedLocationProviderClient`, plain `OkHttp` over Retrofit).
 */
private class RecordingDbHelper(context: Context) :
    SQLiteOpenHelper(context.applicationContext, "fitmap-recordings.db", null, 1) {

    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """
            CREATE TABLE $TABLE (
                id TEXT PRIMARY KEY,
                name TEXT NOT NULL,
                description TEXT NOT NULL,
                activity_type TEXT NOT NULL,
                started_at_ms INTEGER NOT NULL,
                distance_meters REAL NOT NULL,
                duration_seconds INTEGER NOT NULL,
                points_json TEXT NOT NULL,
                sync_status TEXT NOT NULL,
                created_at_ms INTEGER NOT NULL
            )
            """.trimIndent(),
        )
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) {
        db.execSQL("DROP TABLE IF EXISTS $TABLE")
        onCreate(db)
    }

    companion object {
        const val TABLE = "recorded_activities"
    }
}

/** All local GPS Logger recordings — insert on Stop, edited from `RecordedActivitiesActivity`,
 *  drained by `SyncActivity`'s "Sync Now" for whatever's [SyncStatus.QUEUED]. One instance per
 *  caller is fine; `SQLiteOpenHelper` itself keeps the single underlying connection. */
class RecordedActivityStore(context: Context) {

    private val helper = RecordingDbHelper(context)

    suspend fun insert(record: RecordedActivityRecord) = withContext(Dispatchers.IO) {
        helper.writableDatabase.insertOrThrow(RecordingDbHelper.TABLE, null, record.toContentValues())
        Unit
    }

    /** Full-row replace, matching `handleUpdateActivity`'s own convention
     *  (`services/server/internal/httpapi/activities.go`) for the server-side equivalent —
     *  one Save commits every field at once. */
    suspend fun update(record: RecordedActivityRecord) = withContext(Dispatchers.IO) {
        helper.writableDatabase.update(RecordingDbHelper.TABLE, record.toContentValues(), "id = ?", arrayOf(record.id))
        Unit
    }

    suspend fun get(id: String): RecordedActivityRecord? = withContext(Dispatchers.IO) {
        helper.readableDatabase.query(RecordingDbHelper.TABLE, null, "id = ?", arrayOf(id), null, null, null).use {
            if (it.moveToFirst()) it.toRecord() else null
        }
    }

    suspend fun all(): List<RecordedActivityRecord> = withContext(Dispatchers.IO) {
        helper.readableDatabase.query(RecordingDbHelper.TABLE, null, null, null, null, null, "started_at_ms DESC").use { cursor ->
            buildList { while (cursor.moveToNext()) add(cursor.toRecord()) }
        }
    }

    /** What "Sync Now" walks — every row the list's checkbox has queued, regardless of which
     *  filter happened to be showing when it was checked. */
    suspend fun queued(): List<RecordedActivityRecord> = withContext(Dispatchers.IO) {
        helper.readableDatabase.query(
            RecordingDbHelper.TABLE, null, "sync_status = ?", arrayOf(SyncStatus.QUEUED), null, null, null,
        ).use { cursor ->
            buildList { while (cursor.moveToNext()) add(cursor.toRecord()) }
        }
    }

    suspend fun setSyncStatus(id: String, status: String) = withContext(Dispatchers.IO) {
        val values = ContentValues().apply { put("sync_status", status) }
        helper.writableDatabase.update(RecordingDbHelper.TABLE, values, "id = ?", arrayOf(id))
        Unit
    }

    private fun RecordedActivityRecord.toContentValues() = ContentValues().apply {
        put("id", id)
        put("name", name)
        put("description", description)
        put("activity_type", activityType)
        put("started_at_ms", startedAtMs)
        put("distance_meters", distanceMeters)
        put("duration_seconds", durationSeconds)
        put("points_json", points.toJson())
        put("sync_status", syncStatus)
        put("created_at_ms", createdAtMs)
    }

    private fun Cursor.toRecord() = RecordedActivityRecord(
        id = getString(getColumnIndexOrThrow("id")),
        name = getString(getColumnIndexOrThrow("name")),
        description = getString(getColumnIndexOrThrow("description")),
        activityType = getString(getColumnIndexOrThrow("activity_type")),
        startedAtMs = getLong(getColumnIndexOrThrow("started_at_ms")),
        distanceMeters = getDouble(getColumnIndexOrThrow("distance_meters")),
        durationSeconds = getLong(getColumnIndexOrThrow("duration_seconds")),
        points = getString(getColumnIndexOrThrow("points_json")).toRecordedPoints(),
        syncStatus = getString(getColumnIndexOrThrow("sync_status")),
        createdAtMs = getLong(getColumnIndexOrThrow("created_at_ms")),
    )
}
