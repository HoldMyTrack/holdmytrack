package dev.holdmytrack.android.sync

import android.content.res.Resources
import android.util.Log
import androidx.health.connect.client.HealthConnectClient
import androidx.health.connect.client.records.ExerciseRoute
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.SyncActivityJson
import dev.holdmytrack.android.net.SyncPointJson
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import dev.holdmytrack.android.recording.db.toSyncActivity
import java.io.IOException
import java.time.Instant
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** One ticked activity the server refused, or that it could never accept, with the reason why. */
data class Rejection(val key: String, val startedAt: Instant, val activityType: String, val reason: String)

/** What a finished (or curtailed) run did. */
data class SyncReport(
    /** The keys the account has now — sent, or already there: off the list. */
    val landed: Set<String>,
    val synced: Int,
    val alreadyPresent: Int,
    val rejected: List<Rejection>,
    /** Null when everything ticked was sent; otherwise why the run stopped short. */
    val stoppedBecause: String?,
)

/**
 * Sends what the user ticked on the Sync tab (`docs/SPEC.md` FR-3.6, FR-3.8) to
 * `POST /v1/sync/activities`, and nothing else: an unticked candidate never reaches the server.
 * Health Connect sessions go in batches under `healthconnect`; each recording in a request of its
 * own under `recorded`, and is deleted from the phone once the server has it — it lives in the
 * account from then on.
 *
 * Interrupting a run is safe: the server is idempotent on `(user_id, source, external_id)`, so
 * whatever was sent before the cut is simply known next time the list is read (`POST
 * /v1/sync/known`), and sending one again answers `already_processed`.
 *
 * **Foreground only** (`docs/IMPLEMENTATION.md` §4.0): routes written by other apps read back as
 * `ConsentRequired` in the background, so the routes sent here are the ones the Sync tab read
 * while it was on screen — or, for a row kept from an earlier Load ([WaitingStore]),
 * read again here, by its id, as the run reaches it ([SyncCandidates.reread]) — and the tab
 * cancels a run when it leaves.
 */
class SyncRunner(
    private val store: RecordedActivityStore,
    /** For the reasons a run reports — the refusals and why it stopped — in the app's language. */
    private val res: Resources,
    /** Reads a kept row's route again; null when Health Connect can't be read. */
    private val healthConnect: HealthConnectClient?,
) {
    private val landed = mutableSetOf<String>()
    private var synced = 0
    private var alreadyPresent = 0
    private val rejected = mutableListOf<Rejection>()

    suspend fun send(ticked: List<Candidate>, onProgress: (sent: Int) -> Unit): SyncReport {
        val stopped = sendSessions(ticked.filter { it.source == HoldMyTrackApi.SOURCE_HEALTH_CONNECT }, onProgress)
            ?: sendRecordings(ticked.filter { it.origin is Recording }, onProgress)
        return SyncReport(landed.toSet(), synced, alreadyPresent, rejected.toList(), stopped)
    }

    /** Health Connect's ticked sessions, a batch at a time. Returns why it stopped, if it did. */
    private suspend fun sendSessions(sessions: List<Candidate>, onProgress: (Int) -> Unit): String? {
        val batch = mutableListOf<Candidate>()
        var points = 0
        for (ticked in sessions) {
            val candidate = withRoute(ticked) ?: continue
            if (candidate.pointCount > MAX_POINTS_PER_ACTIVITY) {
                val refusal = res.getString(R.string.sync_refusal_too_many_points, candidate.pointCount)
                rejected += Rejection(candidate.key, candidate.startedAt, candidate.activityType, refusal)
                continue
            }
            batch += candidate
            points += candidate.pointCount
            if (batch.size >= MAX_BATCH_ACTIVITIES || points >= MAX_BATCH_POINTS) {
                flush(batch, HoldMyTrackApi.SOURCE_HEALTH_CONNECT)?.let { return it }
                onProgress(landed.size)
                batch.clear()
                points = 0
            }
        }
        val stopped = flush(batch, HoldMyTrackApi.SOURCE_HEALTH_CONNECT)
        onProgress(landed.size)
        return stopped
    }

    /** [candidate] with its route: as read by this Load, or read again now for a row
     *  kept from an earlier one. Null, with the reason among the refusals, when it can't be. */
    private suspend fun withRoute(candidate: Candidate): Candidate? {
        if (candidate.origin != null) return candidate
        val reason = when (val again = healthConnect?.let { SyncCandidates.reread(it, candidate.externalId) }) {
            is SyncCandidates.Reread.Found -> return candidate.copy(origin = again.origin, pointCount = again.origin.route.size)
            SyncCandidates.Reread.Gone -> R.string.sync_refusal_gone
            SyncCandidates.Reread.Refused, null -> R.string.sync_refusal_route
        }
        rejected += Rejection(candidate.key, candidate.startedAt, candidate.activityType, res.getString(reason))
        return null
    }

    /** Each ticked recording on its own; one that lands is deleted from the phone. */
    private suspend fun sendRecordings(recordings: List<Candidate>, onProgress: (Int) -> Unit): String? {
        for (candidate in recordings) {
            flush(listOf(candidate), HoldMyTrackApi.SOURCE_RECORDED)?.let { return it }
            if (candidate.key in landed) store.delete(candidate.externalId)
            onProgress(landed.size)
        }
        return null
    }

    /**
     * Sends [batch] and sorts the server's verdicts. Returns null when the run may go on, or the
     * reason to stop: a failed request decided nothing, so the batch stays ticked on the list.
     */
    private suspend fun flush(batch: List<Candidate>, source: String): String? {
        if (batch.isEmpty()) return null
        val results = try {
            HoldMyTrackApi.syncActivities(batch.map { prepare(it) }, source)
        } catch (e: IOException) {
            Log.w(TAG, "sync batch failed", e)
            return e.message ?: res.getString(R.string.sync_request_failed)
        }
        val byId = results.associateBy { it.externalId }
        for (candidate in batch) {
            val result = byId[candidate.externalId]
            when (result?.status) {
                "enqueued" -> {
                    synced++
                    landed += candidate.key
                }
                "already_processed" -> {
                    alreadyPresent++
                    landed += candidate.key
                }
                else -> rejected += Rejection(
                    candidate.key,
                    candidate.startedAt,
                    candidate.activityType,
                    result?.error?.ifBlank { null } ?: res.getString(R.string.sync_no_report, result?.status ?: "missing"),
                )
            }
        }
        return null
    }

    /**
     * The wire shape `POST /v1/sync/activities` accepts (`docs/IMPLEMENTATION.md` §4.0.3), built
     * from the route as read — never the simplified line the map draws.
     *
     * `external_id` is Health Connect's own record id, or the recording's UUID: the endpoint keys
     * idempotency on `(user_id, source, external_id)`, which is what makes a re-sent activity the
     * same activity rather than a new one.
     *
     * Elevation is sent when the location carries it and omitted otherwise, rather than defaulted
     * to zero — an unknown altitude is not sea level. Heart rate is never read or sent: HoldMyTrack
     * keeps no health data, only the geography (`docs/VISION.md` §1.1).
     *
     * Built off the main thread: a long ride is tens of thousands of points.
     */
    private suspend fun prepare(candidate: Candidate): SyncActivityJson = withContext(Dispatchers.Default) {
        when (val origin = candidate.origin) {
            is Recording -> origin.record.toSyncActivity()
            is HealthConnectSession -> SyncActivityJson(
                externalId = candidate.externalId,
                activityType = candidate.activityType,
                points = origin.route.map {
                    SyncPointJson(lat = it.latitude, lon = it.longitude, elevationM = it.altitude?.inMeters, time = it.time.toString())
                },
            )
            null -> error("candidate ${candidate.key} has nothing to send")
        }
    }

    private companion object {
        const val TAG = "HoldMyTrackSync"

        /** The endpoint accepts 100 per request; a smaller batch keeps one request modest on
         *  mobile data. */
        const val MAX_BATCH_ACTIVITIES = 25
        const val MAX_BATCH_POINTS = 20_000

        /** The server's ceiling, checked here so a route it could never accept is reported as
         *  such instead of being sent to be refused. */
        const val MAX_POINTS_PER_ACTIVITY = 50_000
    }
}
