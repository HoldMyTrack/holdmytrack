package dev.holdmytrack.android.recording.db

import android.content.ContentValues
import android.content.Context
import android.database.Cursor
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import dev.holdmytrack.android.net.Session
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
internal class RecordingDbHelper(context: Context) :
    SQLiteOpenHelper(context.applicationContext, "holdmytrack-recordings.db", null, 5) {

    init {
        // `LiveRecordingJournal` writes on every GPS fix while other screens read the finished
        // recordings — WAL lets those reads run alongside a write and keeps each fix's commit
        // cheap.
        setWriteAheadLoggingEnabled(true)
    }

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
                created_at_ms INTEGER NOT NULL
            )
            """.trimIndent(),
        )
        createJournal(db)
    }

    /** `LiveRecordingJournal`'s two tables: the recording in progress, and its points one row
     *  each so a fix is an append rather than a rewrite of the whole track. */
    private fun createJournal(db: SQLiteDatabase) {
        db.execSQL(
            """
            CREATE TABLE $LIVE_TABLE (
                id TEXT PRIMARY KEY,
                account TEXT NOT NULL,
                moving_ms INTEGER NOT NULL,
                distance_meters REAL NOT NULL
            )
            """.trimIndent(),
        )
        db.execSQL(
            """
            CREATE TABLE $LIVE_POINTS_TABLE (
                recording_id TEXT NOT NULL,
                seq INTEGER NOT NULL,
                lat REAL NOT NULL,
                lon REAL NOT NULL,
                elevation_m REAL,
                time_ms INTEGER NOT NULL,
                PRIMARY KEY (recording_id, seq)
            )
            """.trimIndent(),
        )
    }

    // Version 2 added `account` (see RecordedActivityStore's class doc for why it was
    // missing). No real users predate that, so from version 1 a plain drop-and-recreate is the
    // same "no migration tooling needed pre-launch" call the server side already makes.
    // Version 3 changed no columns: a synced row is now deleted rather than kept with a
    // `synced` status, so upgrading clears the rows that status left behind — and keeps the
    // unsynced ones, which exist nowhere else. Version 4 added the journal's tables. Version 5
    // dropped `sync_status`: a row is deleted once sent, so there is nothing left to mark.
    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) {
        if (oldVersion < 2) {
            db.execSQL("DROP TABLE IF EXISTS $TABLE")
            onCreate(db)
            return
        }
        if (oldVersion < 3) db.execSQL("DELETE FROM $TABLE WHERE sync_status = 'synced'")
        if (oldVersion < 4) createJournal(db)
        if (oldVersion < 5) db.execSQL("ALTER TABLE $TABLE DROP COLUMN sync_status")
    }

    companion object {
        const val TABLE = "recorded_activities"
        const val LIVE_TABLE = "live_recording"
        const val LIVE_POINTS_TABLE = "live_points"
    }
}

/**
 * Every local GPS recording not yet on the server — inserted by `RecordingService` on Stop,
 * listed, edited and sent from the Sync tab (`panel/SyncTab`, `sync/SyncRunner`), each row
 * deleted once the server has it. One instance per
 * caller is fine; `SQLiteOpenHelper` itself keeps the single underlying connection.
 *
 * **Every read and write is scoped to the currently signed-in account.** Recording works
 * whether or not anyone is signed in, and the app supports switching accounts on one device
 * (sign out, sign into a different one, or the demo) — without this, one account's recordings
 * would show up in another's Sync list. Scoped by [Session.email], the same per-account key
 * `sync/HiddenCandidates.kt` uses. A demo account shares the same empty-string key across every
 * demo session on this device, which is harmless — its data is deleted within the day either
 * way, and a demo account can never sync (the Sync tab's own demo gate), so nothing recorded
 * under it ever reaches the server regardless of which demo session sees it locally.
 */
class RecordedActivityStore(context: Context) {

    private val helper = RecordingDbHelper(context)

    private fun account() = Session.email

    suspend fun insert(record: RecordedActivityRecord) = withContext(Dispatchers.IO) {
        helper.writableDatabase.insertOrThrow(RecordingDbHelper.TABLE, null, record.toContentValues(account()))
        Unit
    }

    /** Full-row replace, matching `handleUpdateActivity`'s own convention
     *  (`services/server/internal/httpapi/activities.go`) for the server-side equivalent —
     *  one Save commits every field at once. */
    suspend fun update(record: RecordedActivityRecord) = withContext(Dispatchers.IO) {
        helper.writableDatabase.update(
            RecordingDbHelper.TABLE, record.toContentValues(account()), "id = ? AND account = ?", arrayOf(record.id, account()),
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

    /** Removes every recording of the signed-in account — its account was just deleted
     *  (`SettingsActivity`), so nothing of it should wait here to be synced into a new one
     *  made with the same email. */
    suspend fun deleteAll() = withContext(Dispatchers.IO) {
        helper.writableDatabase.delete(RecordingDbHelper.TABLE, "account = ?", arrayOf(account()))
        Unit
    }

    /** Removes a row — the user's Delete on the Sync tab (the only copy, since nothing here has
     *  synced), or `SyncRunner` once the server has accepted it and the activity lives there
     *  instead. */
    suspend fun delete(id: String) = withContext(Dispatchers.IO) {
        helper.writableDatabase.delete(RecordingDbHelper.TABLE, "id = ? AND account = ?", arrayOf(id, account()))
        Unit
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
        createdAtMs = getLong(getColumnIndexOrThrow("created_at_ms")),
    )
}

/** One `recorded_activities` row, under [account] — the store's signed-in account, or the
 *  journal's own when it finishes a recording made under another (`LiveRecordingJournal`). */
internal fun RecordedActivityRecord.toContentValues(account: String) = ContentValues().apply {
    put("id", id)
    put("account", account)
    put("name", name)
    put("description", description)
    put("activity_type", activityType)
    put("started_at_ms", startedAtMs)
    put("distance_meters", distanceMeters)
    put("duration_seconds", durationSeconds)
    put("points_json", points.toJson())
    put("created_at_ms", createdAtMs)
}
