package dev.holdmytrack.android.sync

import androidx.health.connect.client.HealthConnectClient
import androidx.health.connect.client.records.ExerciseRoute
import androidx.health.connect.client.records.ExerciseRouteResult
import androidx.health.connect.client.records.ExerciseSessionRecord
import androidx.health.connect.client.request.ReadRecordsRequest
import androidx.health.connect.client.time.TimeRangeFilter
import dev.holdmytrack.android.health.ExerciseTypes
import dev.holdmytrack.android.map.Geo
import dev.holdmytrack.android.map.Simplify
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.recording.db.RecordedActivityRecord
import java.time.Duration
import java.time.Instant
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** What a [Candidate] is on the phone: a Health Connect session, or a recording of this app's. */
sealed interface CandidateOrigin

/** A Health Connect session with its route, as read. */
class HealthConnectSession(val record: ExerciseSessionRecord, val route: List<ExerciseRoute.Location>) : CandidateOrigin

/** A GPS recording waiting on this phone — its only copy. */
class Recording(val record: RecordedActivityRecord) : CandidateOrigin

/**
 * One thing the Sync tab offers (`docs/SPEC.md` FR-3.6): an activity on this phone that isn't in
 * the account yet. [key] is unique across both origins and is what the hidden set stores;
 * [line] is the route simplified for drawing ([Simplify]), as `[lon, lat]` pairs, and [bbox]
 * its `[west, south, east, north]`.
 */
data class Candidate(
    val key: String,
    val source: String,
    val externalId: String,
    val startedAt: Instant,
    /** When it ended: what the overlap hint (`docs/SPEC.md` FR-3.7) compares, with [startedAt]. */
    val endedAt: Instant,
    val activityType: String,
    val distanceM: Double,
    /** A recording's name, when it was given one; Health Connect's titles aren't read. */
    val name: String,
    val line: List<DoubleArray>,
    val bbox: List<Double>,
    val pointCount: Int,
    val origin: CandidateOrigin?,
) {
    val isRecording: Boolean get() = source == HoldMyTrackApi.SOURCE_RECORDED
}

/** What reading Health Connect found: the sessions with a route, and how many routes it was
 *  refused (`ConsentRequired` — a read away from the foreground, or a permission taken away). */
data class HealthConnectRead(val sessions: List<Candidate>, val refused: Int)

/**
 * The Sync tab's list (`docs/SPEC.md` FR-3.6): Health Connect sessions with a route from the
 * last [WINDOW], minus what the account already has (`POST /v1/sync/known`), plus every recording
 * waiting on the phone, which never ages out — the phone holds its only copy. There is no
 * cursor: the list is worked out afresh each time from the phone and the server, so a reinstall
 * or a second phone agrees, and an activity deleted on the web is offered again.
 */
object SyncCandidates {

    /** How far back Health Connect is listed; older sessions stay in Health Connect. */
    val WINDOW: Duration = Duration.ofDays(90)

    private const val PAGE_SIZE = 100

    /** The server's floor: a route of fewer points is never listed. */
    private const val MIN_POINTS = 2

    fun key(source: String, externalId: String) = "$source:$externalId"

    /**
     * Every session in Health Connect since [now] less [WINDOW] that has a readable route. A
     * session without one — a gym session, a swim — has no place on a map and isn't listed.
     */
    suspend fun readHealthConnect(client: HealthConnectClient, now: Instant = Instant.now()): HealthConnectRead {
        val sessions = mutableListOf<Candidate>()
        var refused = 0
        var pageToken: String? = null
        do {
            val page = client.readRecords(
                ReadRecordsRequest(
                    ExerciseSessionRecord::class,
                    timeRangeFilter = TimeRangeFilter.after(now.minus(WINDOW)),
                    ascendingOrder = false,
                    pageSize = PAGE_SIZE,
                    pageToken = pageToken,
                ),
            )
            for (record in page.records) {
                when (val result = record.exerciseRouteResult) {
                    is ExerciseRouteResult.Data -> {
                        val route = result.exerciseRoute.route
                        if (route.size >= MIN_POINTS) sessions += fromHealthConnect(record, route)
                    }
                    is ExerciseRouteResult.NoData -> Unit
                    else -> refused++
                }
            }
            pageToken = page.pageToken
        } while (pageToken != null)
        return HealthConnectRead(sessions, refused)
    }

    private suspend fun fromHealthConnect(record: ExerciseSessionRecord, route: List<ExerciseRoute.Location>): Candidate =
        withContext(Dispatchers.Default) {
            val lats = DoubleArray(route.size) { route[it].latitude }
            val lons = DoubleArray(route.size) { route[it].longitude }
            build(
                source = HoldMyTrackApi.SOURCE_HEALTH_CONNECT,
                externalId = record.metadata.id,
                startedAt = record.startTime,
                endedAt = record.endTime,
                activityType = ExerciseTypes.name(record.exerciseType),
                name = "",
                lats = lats,
                lons = lons,
                distanceM = Geo.lengthM(lats, lons),
                origin = HealthConnectSession(record, route),
            )
        }

    suspend fun fromRecording(record: RecordedActivityRecord): Candidate = withContext(Dispatchers.Default) {
        build(
            source = HoldMyTrackApi.SOURCE_RECORDED,
            externalId = record.id,
            startedAt = Instant.ofEpochMilli(record.startedAtMs),
            endedAt = record.points.lastOrNull()?.time ?: Instant.ofEpochMilli(record.startedAtMs).plusSeconds(record.durationSeconds),
            activityType = record.activityType,
            name = record.name,
            lats = DoubleArray(record.points.size) { record.points[it].lat },
            lons = DoubleArray(record.points.size) { record.points[it].lon },
            distanceM = record.distanceMeters,
            origin = Recording(record),
        )
    }

    /** A candidate from its route, with the line it's drawn as. */
    fun build(
        source: String,
        externalId: String,
        startedAt: Instant,
        endedAt: Instant,
        activityType: String,
        name: String,
        lats: DoubleArray,
        lons: DoubleArray,
        distanceM: Double,
        origin: CandidateOrigin?,
    ): Candidate {
        val line = Simplify.indexes(lats, lons).map { doubleArrayOf(lons[it], lats[it]) }
        val bbox = if (lats.isEmpty()) emptyList() else listOf(lons.min(), lats.min(), lons.max(), lats.max())
        return Candidate(
            key = key(source, externalId),
            source = source,
            externalId = externalId,
            startedAt = startedAt,
            endedAt = endedAt,
            activityType = activityType,
            distanceM = distanceM,
            name = name,
            line = line,
            bbox = bbox,
            pointCount = lats.size,
            origin = origin,
        )
    }

    /**
     * What the list shows: Health Connect's [sessions] the account doesn't have ([known] holds
     * their external ids) and every one of the [recordings], newest first — the hidden ones only
     * with [showHidden].
     */
    fun listed(
        sessions: List<Candidate>,
        known: Set<String>,
        recordings: List<Candidate>,
        hidden: Set<String>,
        showHidden: Boolean,
    ): List<Candidate> =
        (sessions.filter { it.externalId !in known } + recordings)
            .filter { showHidden || it.key !in hidden }
            .sortedByDescending { it.startedAt }
}
