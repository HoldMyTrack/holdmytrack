package dev.holdmytrack.android.sync

import android.content.ContentValues
import android.content.Context
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.TrackMetricPoint
import java.nio.ByteBuffer
import java.time.Instant
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** The tables [WaitingStore] keeps, a plain `SQLiteOpenHelper` like the recordings'
 *  (`recording/db/RecordedActivityStore`, which says why not Room). */
internal class WaitingDbHelper(context: Context) :
    SQLiteOpenHelper(context.applicationContext, "holdmytrack-sync.db", null, 1) {

    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """
            CREATE TABLE $TABLE (
                account TEXT NOT NULL,
                key TEXT NOT NULL,
                external_id TEXT NOT NULL,
                started_at_ms INTEGER NOT NULL,
                ended_at_ms INTEGER NOT NULL,
                activity_type TEXT NOT NULL,
                distance_m REAL NOT NULL,
                point_count INTEGER NOT NULL,
                bbox TEXT NOT NULL,
                vertices BLOB NOT NULL,
                PRIMARY KEY (account, key)
            )
            """.trimIndent(),
        )
        db.execSQL(
            """
            CREATE TABLE $CHECKED_TABLE (
                account TEXT PRIMARY KEY,
                checked_at_ms INTEGER NOT NULL,
                refused INTEGER NOT NULL
            )
            """.trimIndent(),
        )
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit

    companion object {
        const val TABLE = "waiting"
        const val CHECKED_TABLE = "waiting_checked"
    }
}

/**
 * What Health Connect had waiting when Load last read it (`apps/android/docs/SPEC.md`
 * FR-3.2), kept on the phone so the Sync tab can list it again — after the app is closed too —
 * without reading Health Connect, which only happens when the user asks. Per account, like the
 * recordings ([Session.email]).
 *
 * Each session is kept as the Sync tab draws it: its times, type, distance and point count, its
 * bounding box, and the simplified line with each vertex's speed and time ([Vertices]) — not its
 * route. A row restored from here has no [Candidate.origin]; when it's sent, [SyncRunner] reads
 * its route from Health Connect again ([SyncCandidates.reread]), since a route goes to the server
 * as read, never as drawn. Recordings aren't kept here: they're on the phone already
 * (`recording/db/RecordedActivityStore`).
 */
class WaitingStore(context: Context) {

    private val helper = WaitingDbHelper(context)

    private fun account() = Session.email

    /** The last read: its sessions, newest first, when it was, and how many routes Health
     *  Connect wouldn't hand over then. */
    data class Saved(val sessions: List<Candidate>, val checkedAt: Instant?, val refused: Int)

    suspend fun read(): Saved = withContext(Dispatchers.IO) {
        val db = helper.readableDatabase
        val sessions = db.query(
            WaitingDbHelper.TABLE, null, "account = ?", arrayOf(account()), null, null, "started_at_ms DESC",
        ).use { c ->
            buildList {
                while (c.moveToNext()) {
                    val vertices = Vertices.decode(c.getBlob(c.getColumnIndexOrThrow("vertices")))
                    add(
                        Candidate(
                            key = c.getString(c.getColumnIndexOrThrow("key")),
                            source = HoldMyTrackApi.SOURCE_HEALTH_CONNECT,
                            externalId = c.getString(c.getColumnIndexOrThrow("external_id")),
                            startedAt = Instant.ofEpochMilli(c.getLong(c.getColumnIndexOrThrow("started_at_ms"))),
                            endedAt = Instant.ofEpochMilli(c.getLong(c.getColumnIndexOrThrow("ended_at_ms"))),
                            activityType = c.getString(c.getColumnIndexOrThrow("activity_type")),
                            distanceM = c.getDouble(c.getColumnIndexOrThrow("distance_m")),
                            name = "",
                            line = vertices.map { doubleArrayOf(it.lon, it.lat) },
                            metrics = vertices,
                            bbox = Vertices.decodeBbox(c.getString(c.getColumnIndexOrThrow("bbox"))),
                            pointCount = c.getInt(c.getColumnIndexOrThrow("point_count")),
                            origin = null,
                        ),
                    )
                }
            }
        }
        val (checkedAt, refused) = db.query(
            WaitingDbHelper.CHECKED_TABLE, null, "account = ?", arrayOf(account()), null, null, null,
        ).use { c ->
            if (c.moveToFirst()) {
                Instant.ofEpochMilli(c.getLong(c.getColumnIndexOrThrow("checked_at_ms"))) to c.getInt(c.getColumnIndexOrThrow("refused"))
            } else {
                null to 0
            }
        }
        Saved(sessions, checkedAt, refused)
    }

    /** A whole read: [sessions] in place of what was kept, all at once. */
    suspend fun replace(sessions: List<Candidate>, checkedAt: Instant, refused: Int) = withContext(Dispatchers.IO) {
        val db = helper.writableDatabase
        db.beginTransaction()
        try {
            db.delete(WaitingDbHelper.TABLE, "account = ?", arrayOf(account()))
            for (session in sessions) insert(db, session)
            db.insertWithOnConflict(
                WaitingDbHelper.CHECKED_TABLE,
                null,
                ContentValues().apply {
                    put("account", account())
                    put("checked_at_ms", checkedAt.toEpochMilli())
                    put("refused", refused)
                },
                SQLiteDatabase.CONFLICT_REPLACE,
            )
            db.setTransactionSuccessful()
        } finally {
            db.endTransaction()
        }
    }

    /** Back on the list: a session the server took and then failed to add. */
    suspend fun put(sessions: List<Candidate>) = withContext(Dispatchers.IO) {
        val db = helper.writableDatabase
        for (session in sessions) insert(db, session)
    }

    /** Off the list: sent, and the account has it now. */
    suspend fun remove(keys: Collection<String>) = withContext(Dispatchers.IO) {
        val db = helper.writableDatabase
        for (key in keys) db.delete(WaitingDbHelper.TABLE, "account = ? AND key = ?", arrayOf(account(), key))
    }

    /** Everything kept for the signed-in account: Health Connect switched off, or the account
     *  deleted. */
    suspend fun clear() = withContext(Dispatchers.IO) {
        val db = helper.writableDatabase
        db.delete(WaitingDbHelper.TABLE, "account = ?", arrayOf(account()))
        db.delete(WaitingDbHelper.CHECKED_TABLE, "account = ?", arrayOf(account()))
        Unit
    }

    private fun insert(db: SQLiteDatabase, session: Candidate) {
        db.insertWithOnConflict(
            WaitingDbHelper.TABLE,
            null,
            ContentValues().apply {
                put("account", account())
                put("key", session.key)
                put("external_id", session.externalId)
                put("started_at_ms", session.startedAt.toEpochMilli())
                put("ended_at_ms", session.endedAt.toEpochMilli())
                put("activity_type", session.activityType)
                put("distance_m", session.distanceM)
                put("point_count", session.pointCount)
                put("bbox", Vertices.encodeBbox(session.bbox))
                put("vertices", Vertices.encode(session.metrics))
            },
            SQLiteDatabase.CONFLICT_REPLACE,
        )
    }

    /**
     * A session's drawn line as bytes: each vertex its longitude, latitude and speed as doubles
     * and its time as a long, 32 bytes a vertex. [Candidate.line] is the same vertices without
     * speed and time ([SyncCandidates.build]), so it's rebuilt from these rather than kept twice.
     */
    object Vertices {
        private const val BYTES = 32

        fun encode(points: List<TrackMetricPoint>): ByteArray {
            val buffer = ByteBuffer.allocate(points.size * BYTES)
            for (p in points) buffer.putDouble(p.lon).putDouble(p.lat).putDouble(p.speedMps).putLong(p.timeS)
            return buffer.array()
        }

        fun decode(bytes: ByteArray): List<TrackMetricPoint> {
            val buffer = ByteBuffer.wrap(bytes)
            return List(bytes.size / BYTES) {
                TrackMetricPoint(lon = buffer.double, lat = buffer.double, speedMps = buffer.double, timeS = buffer.long)
            }
        }

        fun encodeBbox(bbox: List<Double>): String = bbox.joinToString(",")

        fun decodeBbox(text: String): List<Double> = if (text.isEmpty()) emptyList() else text.split(',').map { it.toDouble() }
    }
}