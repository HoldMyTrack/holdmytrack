package dev.fitmap.android.recording.db

import android.content.ContentValues
import android.content.Context
import android.database.Cursor
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import dev.fitmap.android.net.Session
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
    SQLiteOpenHelper(context.applicationContext, "fitmap-recordings.db", null, 2) {

    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """
            CREATE TABLE $TABLE (
                id TEXT PRIMARY KEY,
                account TEXT NOT NULL,
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

    // Version 2 added `account` (see RecordedActivityStore's class doc for why it was
    // missing). No real users predate this yet, so a plain drop-and-recreate is the same
    // "no migration tooling needed pre-launch" call the server side already makes rather than
    // hand-writing an ALTER TABLE this table will never need again.
    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) {
        db.execSQL("DROP TABLE IF EXISTS $TABLE")
        onCreate(db)
    }

    companion object {
        const val TABLE = "recorded_activities"
    }
}

/**
 * All local GPS Logger recordings — insert on Stop, edited from `RecordedActivitiesActivity`,
 * drained by `SyncActivity`'s "Sync Now" for whatever's [SyncStatus.QUEUED]. One instance per
 * caller is fine; `SQLiteOpenHelper` itself keeps the single underlying connection.
 *
 * **Every read and write is scoped to the currently signed-in account.** Recording works
 * whether or not anyone is signed in, and the app supports switching accounts on one device
 * (sign out, sign into a different one, or the demo) — without this, one account's recordings
 * would show up in another's Recorded Activities list. Scoped by [Session.email], the same
 * per-account key `sync/SyncCursor.kt` already uses for exactly this reason: "signing into a
 * different account must not inherit a position through someone else's history." A demo
 * account shares the same empty-string key across every demo session on this device, matching
 * `SyncCursor`'s own "harmless — its data is deleted within the day either way" reasoning —
 * except a demo account can never sync (`SyncActivity`'s own demo gate), so nothing recorded
 * under it ever reaches the server regardless of which demo session sees it locally.
 */
class RecordedActivityStore(context: Context) {

    private val helper = RecordingDbHelper(context)

    private fun account() = Session.email

    suspend fun insert(record: RecordedActivityRecord) = withContext(Dispatchers.IO) {
        helper.writableDatabase.insertOrThrow(RecordingDbHelper.TABLE, null, record.toContentValues())
        Unit
    }

    /** Full-row replace, matching `handleUpdateActivity`'s own convention
     *  (`services/server/internal/httpapi/activities.go`) for the server-side equivalent —
     *  one Save commits every field at once. */
    suspend fun update(record: RecordedActivityRecord) = withContext(Dispatchers.IO) {
        helper.writableDatabase.update(
            RecordingDbHelper.TABLE, record.toContentValues(), "id = ? AND account = ?", arrayOf(record.id, account()),
        )
        Unit
    }

    suspend fun get(id: String): RecordedActivityRecord? = withContext(Dispatchers.IO) {
        helper.readableDatabase.query(
            RecordingDbHelper.TABLE, null, "id = ? AND account = ?", arrayOf(id, account()), null, null, null,
        ).use { if (it.moveToFirst()) it.toRecord() else null }
    }

    suspend fun all(): List<RecordedActivityRecord> = withContext(Dispatchers.IO) {
        helper.readableDatabase.query(
            RecordingDbHelper.TABLE, null, "account = ?", arrayOf(account()), null, null, "started_at_ms DESC",
        ).use { cursor -> buildList { while (cursor.moveToNext()) add(cursor.toRecord()) } }
    }

    /** What "Sync Now" walks — every row the list's checkbox has queued for *this* account,
     *  regardless of which activity-type filter happened to be showing when it was checked. */
    suspend fun queued(): List<RecordedActivityRecord> = withContext(Dispatchers.IO) {
        helper.readableDatabase.query(
            RecordingDbHelper.TABLE, null, "sync_status = ? AND account = ?", arrayOf(SyncStatus.QUEUED, account()), null, null, null,
        ).use { cursor -> buildList { while (cursor.moveToNext()) add(cursor.toRecord()) } }
    }

    suspend fun setSyncStatus(id: String, status: String) = withContext(Dispatchers.IO) {
        val values = ContentValues().apply { put("sync_status", status) }
        helper.writableDatabase.update(RecordingDbHelper.TABLE, values, "id = ? AND account = ?", arrayOf(id, account()))
        Unit
    }

    /**
     * Removes this device's own copy of the recording — always available, regardless of
     * [SyncStatus], per the decision this shipped with. Deliberately local-only, not a mirror
     * of `handleDeleteActivity`'s server-side purge (§4.7.4's FR-5.11): a [SyncStatus.SYNCED]
     * row already has a real, separate `Activity` on the server (its own map coverage, its
     * own presence in the web app), and this store holds no reference back to that row's
     * server-assigned id to delete it by — only the client-generated `external_id` it was
     * submitted under. Deleting the real activity too would need that id threaded back from
     * the sync response, which nothing here captures today. The confirmation dialog that
     * calls this (`RecordedActivitiesActivity`) says so explicitly for a synced row, rather
     * than letting "Delete" read as a promise this doesn't keep.
     */
    suspend fun delete(id: String) = withContext(Dispatchers.IO) {
        helper.writableDatabase.delete(RecordingDbHelper.TABLE, "id = ? AND account = ?", arrayOf(id, account()))
        Unit
    }

    private fun RecordedActivityRecord.toContentValues() = ContentValues().apply {
        put("id", id)
        put("account", account())
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
