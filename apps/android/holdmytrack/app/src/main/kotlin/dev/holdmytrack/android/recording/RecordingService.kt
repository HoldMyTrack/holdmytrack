package dev.holdmytrack.android.recording

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.graphics.drawable.Icon
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Binder
import android.os.IBinder
import android.os.SystemClock
import android.util.Log
import android.widget.Toast
import dev.holdmytrack.android.MainActivity
import androidx.core.content.ContextCompat
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.db.LiveRecordingJournal
import dev.holdmytrack.android.recording.db.RecordedActivityRecord
import java.time.Instant
import java.util.UUID
import java.util.concurrent.Executors
import kotlinx.coroutines.CoroutineExceptionHandler
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.asCoroutineDispatcher
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

enum class RecordingState { IDLE, RECORDING, PAUSED }

/** One GPS fix kept for the wire payload — see `RecordedActivityRecord.toSyncJson`. */
data class RecordedPoint(val lat: Double, val lon: Double, val elevationM: Float?, val time: Instant)

/** What the notification redraws on every fix — recomputed, not accumulated field by field,
 *  so a dropped update never leaves the display inconsistent with itself. */
data class RecordingStats(
    val elapsedMs: Long,
    val distanceM: Double,
    val altitudeM: Double?,
    val speedMps: Double,
)

/**
 * A foreground, GPS-only recording — a declared foreground service rather than plain
 * background location (`apps/android/docs/IMPLEMENTATION.md` §7): an ordinary background process is
 * free for the OS to kill, and continuous GPS is a battery cost a user needs visible feedback
 * about, so this runs as a declared foreground service with a persistent notification for as
 * long as a recording is in progress, tied to start/pause/resume/stop rather than to any
 * Activity's own lifecycle — the whole point is that it outlives the screen turning off.
 *
 * **Driven by intents, not by a bound caller.** The map's record button (`MainActivity`) and
 * the notification's own Pause/Resume and Stop actions both send [ACTION_START] /
 * [ACTION_TOGGLE] / [ACTION_STOP], so a recording can be controlled from the notification
 * shade with no HoldMyTrack screen open at all. Binding is only how `MainActivity` watches it
 * ([onChange], [points]) to draw the live track.
 *
 * **Stop saves here, not in a screen**, for the same reason: Stop can come from the
 * notification. A recording shorter than [MIN_DURATION_MS] of moving time (pauses excluded),
 * or with fewer than two fixes, is dropped rather than saved. Otherwise it's inserted into
 * `recorded_activities` with no name or description and the
 * account's last-used type ([RecordingTypes.lastUsed]) — nothing is asked while recording —
 * and then `RecordingActivity`'s Save screen opens on the new row for name, type and
 * description. The row is already stored by then, so leaving that screen with Back keeps it
 * as saved here; Save screen's Discard is the only way it goes. The notification's Stop is
 * routed through `MainActivity` rather than straight here, so an app window is in the
 * foreground for that screen to open over (a service can't start an activity from the
 * background), and so it can be confirmed there first — one tap in the shade is easy to make
 * by mistake.
 *
 * **Foreground-only in the Path 2 sense doesn't apply here.** `docs/adr/
 * 0007-in-app-gps-recording-submits-directly.md` is explicit: Phase 3's foreground-only sync
 * exists because a route written by another app can't be trusted to a background read
 * (`ConsentRequired`) — there is no other app's data being asked for here, HoldMyTrack is recording
 * its own. This is a foreground *service*, not a foreground-only *permission* restriction.
 *
 * **Every fix is also written to disk as it arrives** ([LiveRecordingJournal]), so a process
 * death mid-recording — which takes the in-memory list and the notification with it — doesn't
 * take the recording. The next time the map binds here and finds no recording running but a
 * journal left over ([findLeftover]), it offers to resume ([resumeLeftover], which comes back
 * paused, so the time the process was dead counts as neither moving time nor distance), save
 * ([saveLeftover]) or discard ([discardLeftover]) it. Nothing restarts on its own: the service
 * stays not sticky, since a restart from the background gets no while-in-use location access
 * and the recording would carry on invisibly anyway. Points are appended only while
 * [RecordingState.RECORDING], never while paused, so a paused stretch doesn't inflate distance
 * or speed with GPS drift from a stationary phone.
 *
 * **Pause is user-initiated only** — no auto-pause. `docs/VISION.md` §1.1 explicitly disclaims
 * that as fitness-tracker sophistication this feature doesn't attempt.
 */
class RecordingService : Service() {

    inner class LocalBinder : Binder() {
        val service: RecordingService get() = this@RecordingService
    }

    private val binder = LocalBinder()

    var state: RecordingState = RecordingState.IDLE
        private set

    /** Set by whoever is bound and wants live redraws (state changes and new fixes); cleared
     *  on unbind. Nothing here fans out to more than one listener — only `MainActivity` binds. */
    var onChange: (() -> Unit)? = null

    /** The journal's key for the recording in progress, and the `external_id` it syncs under
     *  (`RecordedActivityRecord`) — minted at Start, so the journal and the saved row share it. */
    private var recordingId = ""

    /** Fixed at Start: the recording belongs to whoever was signed in when it began, even if
     *  it ends up saved from a leftover while someone else is. */
    private var account = ""

    private val recorded = mutableListOf<RecordedPoint>()
    private var lastFix: Location? = null
    private var distanceM = 0.0

    /** Elapsed time is measured off `elapsedRealtime()`, not wall-clock time — immune to the
     *  user's clock being adjusted (timezone travel, NTP correction) mid-recording. */
    private var recordingStartedAtRealtime = 0L
    private var accumulatedBeforePauseMs = 0L

    private var locationManager: LocationManager? = null

    private val locationListener = LocationListener { location -> onLocation(location) }

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onUnbind(intent: Intent?): Boolean {
        onChange = null
        return false
    }

    /** Not sticky — see the class doc: a process death is recovered from the journal, at the
     *  user's say, not by the system restarting this in the background. */
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START -> if (state == RecordingState.IDLE) start() else startForeground()
            ACTION_TOGGLE -> toggle()
            ACTION_STOP -> stop()
        }
        if (state == RecordingState.IDLE) stopSelf(startId)
        return START_NOT_STICKY
    }

    /** A copy, not the live list — it keeps growing on the main thread while the caller
     *  draws it. */
    fun points(): List<RecordedPoint> = recorded.toList()

    /** `MainActivity` has already confirmed `ACCESS_FINE_LOCATION` before sending
     *  [ACTION_START] — a foreground service with a location type throws at `startForeground`
     *  without it. Re-checked here only so a stray start can't crash the process. */
    private fun start() {
        if (!hasLocationPermission()) return
        recordingId = UUID.randomUUID().toString()
        account = Session.email
        recorded.clear()
        lastFix = null
        distanceM = 0.0
        accumulatedBeforePauseMs = 0L
        recordingStartedAtRealtime = SystemClock.elapsedRealtime()
        state = RecordingState.RECORDING

        startForeground()
        listenForFixes()
        publish()
        val id = recordingId
        val owner = account
        val context = applicationContext
        journalWrite { journal ->
            // Normally answered before this, from the map's prompt — but a leftover the prompt
            // never got to (another account's, or a start tapped before the prompt came up)
            // is saved as it stands rather than overwritten: the journal holds one recording.
            journal.leftover()?.let { journal.finish(it.id, recordOf(context, it.id, it.account, it.movingMs, it.distanceM, it.points), it.account) }
            journal.begin(id, owner)
        }
    }

    private fun hasLocationPermission() =
        checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED

    private fun listenForFixes() {
        val manager = getSystemService(LOCATION_SERVICE) as LocationManager
        locationManager = manager
        @Suppress("MissingPermission") // every caller checks hasLocationPermission first
        manager.requestLocationUpdates(LocationManager.GPS_PROVIDER, MIN_UPDATE_MS, MIN_UPDATE_M, locationListener)
    }

    /**
     * Answers [onFound] (on the main thread) with a recording a process death left in the
     * journal, when there is one for the signed-in account and nothing is recording now —
     * another account's waits for that account, since its Save screen would find nothing
     * under this one. Queued behind every pending journal write, so a Stop that has only just
     * happened is never mistaken for a leftover.
     */
    fun findLeftover(onFound: (LiveRecordingJournal.Leftover) -> Unit) {
        journalWrite { journal ->
            val leftover = journal.leftover() ?: return@journalWrite
            withContext(Dispatchers.Main) {
                if (state == RecordingState.IDLE && leftover.account == Session.email) onFound(leftover)
            }
        }
    }

    /** Picks [leftover] up where it was, paused: the gap is a pause, not distance or moving
     *  time. False, and nothing changes, when location access has been taken away since. */
    fun resumeLeftover(leftover: LiveRecordingJournal.Leftover): Boolean {
        if (state != RecordingState.IDLE || !hasLocationPermission()) return false
        recordingId = leftover.id
        account = leftover.account
        recorded.clear()
        recorded += leftover.points
        lastFix = null
        distanceM = leftover.distanceM
        accumulatedBeforePauseMs = leftover.movingMs
        state = RecordingState.PAUSED
        // Started, not just bound, so it outlives the map the way a fresh recording does;
        // ACTION_START on a service that isn't idle only goes foreground.
        ContextCompat.startForegroundService(this, intent(this, ACTION_START))
        listenForFixes()
        publish()
        return true
    }

    /** Saves [leftover] as Stop would have, Save screen included. */
    fun saveLeftover(leftover: LiveRecordingJournal.Leftover) {
        val context = applicationContext
        val record = recordOf(context, leftover.id, leftover.account, leftover.movingMs, leftover.distanceM, leftover.points)
        finish(context, leftover.id, record, leftover.account)
    }

    fun discardLeftover(leftover: LiveRecordingJournal.Leftover) {
        journalWrite { it.finish(leftover.id, null, leftover.account) }
    }

    private fun toggle() {
        when (state) {
            RecordingState.RECORDING -> {
                state = RecordingState.PAUSED
                accumulatedBeforePauseMs += SystemClock.elapsedRealtime() - recordingStartedAtRealtime
                val id = recordingId
                val movingMs = accumulatedBeforePauseMs
                val distance = distanceM
                journalWrite { it.updateTotals(id, movingMs, distance) }
            }
            RecordingState.PAUSED -> {
                state = RecordingState.RECORDING
                recordingStartedAtRealtime = SystemClock.elapsedRealtime()
                // The next fix after a pause is a fresh baseline, not a jump from wherever the
                // phone drifted to while stationary — counting that gap as distance would
                // inflate the track.
                lastFix = null
            }
            RecordingState.IDLE -> return
        }
        publish()
    }

    private fun stop() {
        if (state == RecordingState.IDLE) return
        val stats = currentStats()
        val points = recorded.toList()
        state = RecordingState.IDLE
        locationManager?.removeUpdates(locationListener)
        locationManager = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        recorded.clear()
        publish()

        val context = applicationContext
        finish(context, recordingId, recordOf(context, recordingId, account, stats.elapsedMs, stats.distanceM, points), account)
    }

    /** Moves recording [id] out of the journal — into `recorded_activities` as [record], then
     *  the Save screen on it, or, with no [record] (too short), nowhere. */
    private fun finish(context: Context, id: String, record: RecordedActivityRecord?, owner: String) {
        if (record == null) {
            Toast.makeText(context, R.string.recording_too_short, Toast.LENGTH_SHORT).show()
        }
        journalWrite { journal ->
            journal.finish(id, record, owner)
            if (record != null) {
                withContext(Dispatchers.Main) {
                    context.startActivity(RecordingActivity.saveIntent(context, record.id).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                }
            }
        }
    }

    /** Journal writes go through one thread, in order, on an application-lifetime scope rather
     *  than this service's own: the service is free to be destroyed the moment it stops being
     *  foreground, and a Stop's save must still finish. */
    private fun journalWrite(block: suspend (LiveRecordingJournal) -> Unit) {
        val journal = journal(applicationContext)
        journalScope.launch { block(journal) }
    }

    fun currentStats(): RecordingStats {
        val elapsed = when (state) {
            RecordingState.RECORDING -> accumulatedBeforePauseMs + (SystemClock.elapsedRealtime() - recordingStartedAtRealtime)
            else -> accumulatedBeforePauseMs
        }
        val fix = lastFix
        return RecordingStats(
            elapsedMs = elapsed,
            distanceM = distanceM,
            altitudeM = fix?.takeIf { it.hasAltitude() }?.altitude,
            speedMps = fix?.takeIf { it.hasSpeed() }?.speed?.toDouble() ?: 0.0,
        )
    }

    private fun onLocation(location: Location) {
        if (state != RecordingState.RECORDING) return
        lastFix?.let { distanceM += it.distanceTo(location) }
        lastFix = location
        val point = RecordedPoint(
            lat = location.latitude,
            lon = location.longitude,
            elevationM = if (location.hasAltitude()) location.altitude.toFloat() else null,
            time = Instant.ofEpochMilli(location.time),
        )
        recorded += point
        val id = recordingId
        val seq = recorded.size - 1
        val movingMs = currentStats().elapsedMs
        val distance = distanceM
        journalWrite { it.append(id, seq, point, movingMs, distance) }
        publish()
    }

    private fun publish() {
        if (state != RecordingState.IDLE) {
            getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, buildNotification())
        }
        onChange?.invoke()
    }

    private fun startForeground() {
        startForeground(NOTIFICATION_ID, buildNotification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
    }

    /**
     * Title says whether it's recording at all ("am I recording?" is the question this
     * notification exists to answer at a glance); the text carries the same stats the old
     * recording screen showed. While recording, elapsed time is the system's own chronometer
     * rather than text — it ticks every second without this service having to re-post the
     * notification every second. Paused, a chronometer would keep counting, so the frozen
     * time moves into the title instead.
     */
    private fun buildNotification(): Notification {
        val manager = getSystemService(NotificationManager::class.java)
        // Default importance, silenced — not IMPORTANCE_LOW: a low-importance notification is
        // filed under the shade's collapsed "Silent" section, which hides Pause and Stop behind
        // an extra tap (found on an emulator). A channel's importance can't be raised once
        // created, hence the new id and the removal of the old one.
        manager.deleteNotificationChannel(LEGACY_CHANNEL_ID)
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_ID, getString(R.string.recording_notification_channel), NotificationManager.IMPORTANCE_DEFAULT).apply {
                setSound(null, null)
                enableVibration(false)
            },
        )
        val stats = currentStats()
        val recording = state == RecordingState.RECORDING
        val details = listOfNotNull(
            RecordingFormat.distance(resources, stats.distanceM),
            RecordingFormat.speed(resources, stats.speedMps),
            stats.altitudeM?.let { RecordingFormat.altitude(resources, it) },
        ).joinToString(" · ")

        val builder = Notification.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_circle_dot)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setCategory(Notification.CATEGORY_STATUS)
            .setContentText(details)
            .setContentIntent(
                PendingIntent.getActivity(
                    this, 0,
                    Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP),
                    PendingIntent.FLAG_IMMUTABLE,
                ),
            )
            .addAction(
                action(
                    if (recording) R.drawable.ic_pause else R.drawable.ic_play,
                    if (recording) R.string.recording_pause else R.string.recording_resume,
                    ACTION_TOGGLE,
                ),
            )
            .addAction(
                Notification.Action.Builder(
                    Icon.createWithResource(this, R.drawable.ic_square),
                    getString(R.string.recording_stop),
                    PendingIntent.getActivity(
                        this, ACTION_STOP.hashCode(),
                        Intent(this, MainActivity::class.java)
                            .setAction(ACTION_STOP)
                            .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP),
                        PendingIntent.FLAG_IMMUTABLE,
                    ),
                ).build(),
            )
        if (recording) {
            builder.setContentTitle(getString(R.string.recording_notification_title))
                .setShowWhen(true)
                .setUsesChronometer(true)
                .setWhen(System.currentTimeMillis() - stats.elapsedMs)
        } else {
            builder.setContentTitle(
                getString(R.string.recording_notification_title_paused, RecordingFormat.duration(stats.elapsedMs)),
            ).setShowWhen(false)
        }
        return builder.build()
    }

    private fun action(icon: Int, title: Int, action: String): Notification.Action =
        Notification.Action.Builder(
            Icon.createWithResource(this, icon),
            getString(title),
            PendingIntent.getService(this, action.hashCode(), intent(this, action), PendingIntent.FLAG_IMMUTABLE),
        ).build()

    companion object {
        const val ACTION_START = "dev.holdmytrack.android.recording.START"
        const val ACTION_TOGGLE = "dev.holdmytrack.android.recording.TOGGLE"
        const val ACTION_STOP = "dev.holdmytrack.android.recording.STOP"

        /** A recording shorter than this — moving time, pauses excluded — is dropped on Stop
         *  rather than saved: an accidental tap, not an activity anyone meant to keep. */
        const val MIN_DURATION_MS = 60_000L

        fun intent(context: Context, action: String): Intent =
            Intent(context, RecordingService::class.java).setAction(action)

        private const val CHANNEL_ID = "recording_status"
        private const val LEGACY_CHANNEL_ID = "recording"
        private const val NOTIFICATION_ID = 1

        /** A GPS-only recording of a walk/hike/ride doesn't need a fix every second — this
         *  keeps the point count, and the battery cost, sane over a multi-hour recording. */
        private const val MIN_UPDATE_MS = 3_000L
        private const val MIN_UPDATE_M = 5f

        private const val TAG = "RecordingService"

        /** A failed write is logged, not thrown: a full disk shouldn't take a recording down
         *  with it, and a Stop whose save fails leaves the journal behind to offer next time. */
        private val journalScope = CoroutineScope(
            SupervisorJob() +
                Executors.newSingleThreadExecutor().asCoroutineDispatcher() +
                CoroutineExceptionHandler { _, error -> Log.e(TAG, "journal write failed", error) },
        )

        private var journalInstance: LiveRecordingJournal? = null

        private fun journal(context: Context): LiveRecordingJournal =
            journalInstance ?: LiveRecordingJournal(context).also { journalInstance = it }

        /** The row Stop saves — blank name and description, the account's last-used type
         *  ([RecordingTypes.lastUsed]) — or null for one too short to keep. */
        private fun recordOf(
            context: Context,
            id: String,
            account: String,
            movingMs: Long,
            distanceM: Double,
            points: List<RecordedPoint>,
        ): RecordedActivityRecord? {
            if (movingMs < MIN_DURATION_MS || points.size < 2) return null
            return RecordedActivityRecord(
                id = id,
                name = "",
                description = "",
                activityType = RecordingTypes.lastUsed(context, account),
                startedAtMs = points.first().time.toEpochMilli(),
                distanceMeters = distanceM,
                durationSeconds = movingMs / 1000,
                points = points,
                createdAtMs = System.currentTimeMillis(),
            )
        }
    }
}
