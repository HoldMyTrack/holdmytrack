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
import android.widget.Toast
import dev.holdmytrack.android.MainActivity
import dev.holdmytrack.android.R
import dev.holdmytrack.android.recording.db.RecordedActivityRecord
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import dev.holdmytrack.android.recording.db.SyncStatus
import java.time.Instant
import java.util.UUID
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

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
 * `RecordedActivityStore` as [SyncStatus.NOT_SYNCED] with no name or description and the
 * account's last-used type ([RecordingTypes.lastUsed]) — nothing is asked while recording —
 * and then `RecordingActivity`'s Save screen opens on the new row for name, type and
 * description. The row is already stored by then, so leaving that screen with Back keeps it
 * as saved here; Save screen's Discard is the only way it goes. The notification's Stop is
 * routed through `MainActivity` rather than straight here, so an app window is in the
 * foreground for that screen to open over (a service can't start an activity from the
 * background).
 *
 * **Foreground-only in the Path 2 sense doesn't apply here.** `docs/adr/
 * 0007-in-app-gps-recording-submits-directly.md` is explicit: Phase 3's foreground-only sync
 * exists because a route written by another app can't be trusted to a background read
 * (`ConsentRequired`) — there is no other app's data being asked for here, HoldMyTrack is recording
 * its own. This is a foreground *service*, not a foreground-only *permission* restriction.
 *
 * **Buffering is an in-memory list, deliberately not solving crash recovery.** A low-memory
 * kill mid-recording loses whatever hasn't been saved yet — `apps/android/docs/ROADMAP.md`'s
 * own "crash/kill recovery" item names this as a real, still-open gap, not an oversight here.
 * What this class does guarantee: points are appended only while [RecordingState.RECORDING],
 * never while paused, so a paused stretch doesn't inflate distance or speed with GPS drift
 * from a stationary phone.
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

    /** Not sticky: a restarted service would come back with none of the points it had, so
     *  there is nothing worth resurrecting. */
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
        if (checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) != PackageManager.PERMISSION_GRANTED) return
        recorded.clear()
        lastFix = null
        distanceM = 0.0
        accumulatedBeforePauseMs = 0L
        recordingStartedAtRealtime = SystemClock.elapsedRealtime()
        state = RecordingState.RECORDING

        startForeground()
        val manager = getSystemService(LOCATION_SERVICE) as LocationManager
        locationManager = manager
        @Suppress("MissingPermission") // checked above
        manager.requestLocationUpdates(LocationManager.GPS_PROVIDER, MIN_UPDATE_MS, MIN_UPDATE_M, locationListener)
        publish()
    }

    private fun toggle() {
        when (state) {
            RecordingState.RECORDING -> {
                state = RecordingState.PAUSED
                accumulatedBeforePauseMs += SystemClock.elapsedRealtime() - recordingStartedAtRealtime
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

        if (stats.elapsedMs < MIN_DURATION_MS || points.size < 2) {
            Toast.makeText(applicationContext, R.string.recording_too_short, Toast.LENGTH_SHORT).show()
            return
        }
        val context = applicationContext
        val record = RecordedActivityRecord(
            id = UUID.randomUUID().toString(),
            name = "",
            description = "",
            activityType = RecordingTypes.lastUsed(context),
            startedAtMs = points.first().time.toEpochMilli(),
            distanceMeters = stats.distanceM,
            durationSeconds = stats.elapsedMs / 1000,
            points = points,
            syncStatus = SyncStatus.NOT_SYNCED,
            createdAtMs = System.currentTimeMillis(),
        )
        // Not this service's own scope: the service is free to be destroyed the moment it
        // stops being foreground, and the insert must still finish.
        saveScope.launch {
            RecordedActivityStore(context).insert(record)
            context.startActivity(RecordingActivity.saveIntent(context, record.id).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }
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
        recorded += RecordedPoint(
            lat = location.latitude,
            lon = location.longitude,
            elevationM = if (location.hasAltitude()) location.altitude.toFloat() else null,
            time = Instant.ofEpochMilli(location.time),
        )
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

        private val saveScope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    }
}
