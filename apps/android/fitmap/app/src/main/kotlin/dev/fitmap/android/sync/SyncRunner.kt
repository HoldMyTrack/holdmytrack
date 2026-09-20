package dev.fitmap.android.sync

import android.util.Log
import androidx.health.connect.client.HealthConnectClient
import androidx.health.connect.client.records.ExerciseRoute
import androidx.health.connect.client.records.ExerciseRouteResult
import androidx.health.connect.client.records.ExerciseSessionRecord
import androidx.health.connect.client.request.ReadRecordsRequest
import androidx.health.connect.client.time.TimeRangeFilter
import dev.fitmap.android.health.ExerciseTypes
import dev.fitmap.android.net.FitMapApi
import java.io.IOException
import java.time.Instant
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject

/** One activity the server rejected, or that could never be accepted, with the reason why. */
data class Rejection(val startedAt: Instant, val activityType: String, val reason: String)

/** What a finished (or curtailed) run did. */
data class SyncReport(
    val scanned: Int,
    val synced: Int,
    val alreadyPresent: Int,
    val skippedNoRoute: Int,
    val rejected: List<Rejection>,
    /** Null when the run reached the end of the store; otherwise why it stopped short. */
    val stoppedBecause: String?,
)

/** Live counts while a run is in progress, for the screen to show. */
data class SyncProgress(val scanned: Int, val synced: Int)

/**
 * One foreground sync run: read exercise sessions and their routes out of Health Connect,
 * send them to `POST /v1/sync/activities`, and move the watermark only over records that are
 * genuinely finished with.
 *
 * **Foreground-only is a platform constraint, not a design preference** (`docs/IMPLEMENTATION.md`
 * §4.0). Routes written by other apps read back as `ConsentRequired` in the background even
 * with "Always allow" granted — Phase 1 measured the same 46 sessions returning 23 routes in
 * the foreground and none in the background. Nothing here schedules itself: the caller runs it
 * from a foreground screen and cancels it when that screen stops, and because the watermark
 * only moves on confirmed records, a run cut off mid-way simply resumes next time.
 *
 * **Every record gets one of two verdicts, and the watermark hangs on the difference:**
 *  - *Terminal* — the server took it, already had it, permanently refused it, or it has no
 *    route and never will. The watermark may pass it.
 *  - *Blocking* — a route exists but could not be read (`ConsentRequired`), or the request
 *    failed. The watermark stops, and the run stops with it, because everything after this
 *    point would otherwise be read and confirmed while this one is quietly left behind — the
 *    exact "complete in every respect except the map" failure §4.0 warns about.
 *
 * **A session with no route is terminal, not a failure.** Half the sessions Phase 1 measured
 * had none, and the reason is ordinary: a gym session, a swim or a rowing machine has no
 * trajectory by its nature. FitMap's scope is outdoor GPS tracking (`docs/VISION.md` §1.1), so
 * these are skipped by design rather than retried forever or represented as gaps.
 */
class SyncRunner(
    private val client: HealthConnectClient,
    private val cursor: SyncCursor,
) {

    /** Records read since the watermark last moved, in order. Cleared each time it moves. */
    private val segment = mutableListOf<ExerciseSessionRecord>()

    /** Activities prepared but not yet sent. */
    private val batch = mutableListOf<JSONObject>()
    private val batchRecords = mutableListOf<ExerciseSessionRecord>()
    private var batchPoints = 0

    private var scanned = 0
    private var synced = 0
    private var alreadyPresent = 0
    private var skippedNoRoute = 0
    private val rejected = mutableListOf<Rejection>()

    suspend fun run(onProgress: (SyncProgress) -> Unit): SyncReport {
        val startFrom = cursor.at
        val alreadyHandled = cursor.handledAtCursor
        var pageToken: String? = null
        var stoppedBecause: String? = null

        paging@ while (true) {
            val page = client.readRecords(
                ReadRecordsRequest(
                    ExerciseSessionRecord::class,
                    // `after` is inclusive, so the record the watermark stands on comes back
                    // every run; SyncCursor.handledAtCursor is what filters it out again
                    // without also losing a session that shares its start instant.
                    timeRangeFilter = TimeRangeFilter.after(startFrom ?: Instant.EPOCH),
                    ascendingOrder = true,
                    pageSize = PAGE_SIZE,
                    pageToken = pageToken,
                ),
            )

            for (record in page.records) {
                if (record.startTime == startFrom && record.metadata.id in alreadyHandled) continue
                scanned++
                onProgress(SyncProgress(scanned, synced))

                when (val result = record.exerciseRouteResult) {
                    is ExerciseRouteResult.Data -> take(record, result.exerciseRoute)
                    is ExerciseRouteResult.NoData -> {
                        skippedNoRoute++
                        segment += record
                    }
                    else -> {
                        // ConsentRequired. Blocking, always: the route is there and we were
                        // refused it, so passing over this record would lose it for good.
                        stoppedBecause = CONSENT_REQUIRED
                        break@paging
                    }
                }

                if (batch.size >= MAX_BATCH_ACTIVITIES || batchPoints >= MAX_BATCH_POINTS) {
                    stoppedBecause = flush()
                    if (stoppedBecause != null) break@paging
                }
            }

            pageToken = page.pageToken ?: break
        }

        // Whatever is buffered still has to be sent and confirmed before the watermark can
        // cover it — including when the loop above stopped at a blocking record, since
        // everything before that record is still legitimately finished with.
        val flushFailure = flush()
        return SyncReport(
            scanned = scanned,
            synced = synced,
            alreadyPresent = alreadyPresent,
            skippedNoRoute = skippedNoRoute,
            rejected = rejected.toList(),
            stoppedBecause = stoppedBecause ?: flushFailure,
        )
    }

    /** Prepares one session with geometry, or records why it can never be accepted. */
    private suspend fun take(record: ExerciseSessionRecord, route: ExerciseRoute) {
        val points = route.route
        val activityType = ExerciseTypes.name(record.exerciseType)
        val refusal = when {
            points.size < MIN_POINTS -> "the route has fewer than two points"
            points.size > MAX_POINTS_PER_ACTIVITY ->
                "the route has ${points.size} points, more than one sync request accepts"
            else -> null
        }
        if (refusal != null) {
            // Terminal, like a server rejection: nothing about this record will change, so the
            // watermark must pass it rather than stall on it forever.
            rejected += Rejection(record.startTime, activityType, refusal)
            segment += record
            return
        }
        batch += prepare(record, activityType, points)
        batchRecords += record
        batchPoints += points.size
        segment += record
    }

    /**
     * Sends the buffered batch and moves the watermark over everything it covers.
     *
     * Returns null when the run may continue, or a reason to stop. A failed request stops the
     * run *without* moving the watermark: nothing in that batch was decided, and guessing
     * either way is how records get skipped.
     */
    private suspend fun flush(): String? {
        if (batch.isEmpty()) {
            advanceOverSegment()
            return null
        }
        val results = try {
            FitMapApi.syncActivities(batch, FitMapApi.SOURCE_HEALTH_CONNECT)
        } catch (e: IOException) {
            Log.w(TAG, "sync batch failed", e)
            batch.clear()
            batchRecords.clear()
            batchPoints = 0
            segment.clear()
            return e.message ?: "the sync request failed"
        }

        val byId = results.associateBy { it.externalId }
        for (record in batchRecords) {
            when (val result = byId[record.metadata.id]?.status) {
                "enqueued" -> synced++
                "already_processed" -> alreadyPresent++
                else -> rejected += Rejection(
                    record.startTime,
                    ExerciseTypes.name(record.exerciseType),
                    byId[record.metadata.id]?.error?.ifBlank { null }
                        ?: "the server did not report on this activity (status ${result ?: "missing"})",
                )
            }
        }

        batch.clear()
        batchRecords.clear()
        batchPoints = 0
        advanceOverSegment()
        return null
    }

    /**
     * Moves the watermark to the last record of the current segment. Safe by construction:
     * the run stops at the first blocking record, so a segment only ever contains records that
     * are finished with.
     */
    private fun advanceOverSegment() {
        val last = segment.lastOrNull() ?: return
        val idsAtInstant = segment.filter { it.startTime == last.startTime }
            .map { it.metadata.id }
            .toSet()
        cursor.advanceTo(last.startTime, idsAtInstant)
        segment.clear()
    }

    /**
     * The wire shape `POST /v1/sync/activities` accepts (`docs/IMPLEMENTATION.md` §4.0.3).
     *
     * `external_id` is Health Connect's own record id, not a hash of the payload — the endpoint
     * keys idempotency on `(user_id, source, external_id)` precisely so a re-sent record is
     * recognised as the same activity rather than minted as a new one, which is what makes an
     * interrupted run safe to simply repeat.
     *
     * Elevation is sent when the location carries it and omitted otherwise, rather than
     * defaulted to zero — an unknown altitude is not sea level. Heart rate is never sent: it
     * lives in a separate record type behind a separate permission, and FitMap declares only
     * what it uses.
     *
     * Built off the main thread: a long ride is tens of thousands of points, and serialising
     * that is real work.
     */
    private suspend fun prepare(
        record: ExerciseSessionRecord,
        activityType: String,
        points: List<ExerciseRoute.Location>,
    ): JSONObject = withContext(Dispatchers.Default) {
        val array = JSONArray()
        for (point in points) {
            val json = JSONObject()
                .put("lat", point.latitude)
                .put("lon", point.longitude)
                .put("time", point.time.toString())
            point.altitude?.let { json.put("elevation_m", it.inMeters) }
            array.put(json)
        }
        JSONObject()
            .put("external_id", record.metadata.id)
            .put("activity_type", activityType)
            .put("points", array)
    }

    private companion object {
        const val TAG = "FitMapSync"

        /** How many sessions one Health Connect read asks for. */
        const val PAGE_SIZE = 50

        /** The endpoint accepts 100 per request; a smaller batch keeps one request modest on
         *  mobile data, and makes the watermark move more often on a long first backfill. */
        const val MAX_BATCH_ACTIVITIES = 25
        const val MAX_BATCH_POINTS = 20_000

        /** The server's own floor and ceiling, checked here so a record that could never be
         *  accepted is reported as such instead of being sent to be refused. */
        const val MIN_POINTS = 2
        const val MAX_POINTS_PER_ACTIVITY = 50_000

        const val CONSENT_REQUIRED =
            "Health Connect would not hand over a route. Keep FitMap on screen while syncing, " +
                "check that exercise routes are still allowed, then sync again — nothing after " +
                "that activity has been synced, so none of it is lost."
    }
}
