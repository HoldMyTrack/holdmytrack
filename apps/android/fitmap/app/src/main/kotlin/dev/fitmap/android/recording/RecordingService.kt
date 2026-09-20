package dev.fitmap.android.recording

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Binder
import android.os.IBinder
import android.os.SystemClock
import dev.fitmap.android.R
import java.time.Instant

enum class RecordingState { IDLE, RECORDING, PAUSED, STOPPED }

/** One GPS fix kept for the wire payload — see `RecordingActivity.submit`. */
data class RecordedPoint(val lat: Double, val lon: Double, val elevationM: Float?, val time: Instant)

/** What the recording screen redraws on every fix — recomputed, not accumulated field by
 *  field, so a dropped update never leaves the display inconsistent with itself. */
data class RecordingStats(
    val elapsedMs: Long,
    val distanceM: Double,
    val altitudeM: Double?,
    val speedMps: Double,
)

/**
 * A foreground, GPS-only recording — Phase 7's own answer to "foreground service or plain
 * background location?" (`apps/android/docs/ROADMAP.md`): an ordinary background process is
 * free for the OS to kill, and continuous GPS is a battery cost a user needs visible feedback
 * about, so this runs as a declared foreground service with a persistent notification for as
 * long as a recording is in progress, tied to Record/Pause/Resume/Stop rather than to any
 * Activity's own lifecycle — the whole point is that it outlives the screen turning off.
 *
 * **Foreground-only in the Path 2 sense doesn't apply here.** `docs/adr/
 * 0007-in-app-gps-recording-submits-directly.md` is explicit: Phase 3's foreground-only sync
 * exists because a route written by another app can't be trusted to a background read
 * (`ConsentRequired`) — there is no other app's data being asked for here, FitMap is recording
 * its own. This is a foreground *service*, not a foreground-only *permission* restriction.
 *
 * **Buffering is an in-memory list, deliberately not solving crash recovery.** A phone call or
 * a low-memory kill mid-recording loses whatever hasn't been submitted yet — `apps/android/
 * docs/ROADMAP.md`'s own "crash/kill recovery" item names this as a real, still-open gap, not
 * an oversight here. What this class does guarantee: points are appended only while
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

    /** Set by whoever is bound and wants live redraws; cleared on unbind. Nothing here fans
     *  out to more than one listener — one Activity is ever bound at a time. */
    var onUpdate: ((RecordingStats) -> Unit)? = null

    private val points = mutableListOf<RecordedPoint>()
    private var lastFix: Location? = null
    private var distanceM = 0.0

    /** Elapsed time is measured off `elapsedRealtime()`, not wall-clock time — immune to the
     *  user's clock being adjusted (timezone travel, NTP correction) mid-recording. */
    private var recordingStartedAtRealtime = 0L
    private var accumulatedBeforePauseMs = 0L
    private var pausedAtRealtime = 0L

    private var locationManager: LocationManager? = null

    private val locationListener = LocationListener { location -> onLocation(location) }

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onUnbind(intent: Intent?): Boolean {
        onUpdate = null
        return false
    }

    /** Starts a fresh recording. The caller (`RecordingActivity`) has already confirmed
     *  `ACCESS_FINE_LOCATION` and `POST_NOTIFICATIONS` are granted — a foreground service with
     *  a location type throws at `startForeground` without the former, and the latter is what
     *  makes the persistent notification actually visible. */
    fun start() {
        if (state != RecordingState.IDLE) return
        points.clear()
        lastFix = null
        distanceM = 0.0
        accumulatedBeforePauseMs = 0L
        recordingStartedAtRealtime = SystemClock.elapsedRealtime()
        state = RecordingState.RECORDING

        startForeground(NOTIFICATION_ID, buildNotification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
        val manager = getSystemService(LOCATION_SERVICE) as LocationManager
        locationManager = manager
        @Suppress("MissingPermission") // caller-verified, see the doc comment above
        manager.requestLocationUpdates(LocationManager.GPS_PROVIDER, MIN_UPDATE_MS, MIN_UPDATE_M, locationListener)
    }

    fun pause() {
        if (state != RecordingState.RECORDING) return
        state = RecordingState.PAUSED
        pausedAtRealtime = SystemClock.elapsedRealtime()
        accumulatedBeforePauseMs += pausedAtRealtime - recordingStartedAtRealtime
        publish()
    }

    fun resume() {
        if (state != RecordingState.PAUSED) return
        state = RecordingState.RECORDING
        recordingStartedAtRealtime = SystemClock.elapsedRealtime()
        // The next fix after a pause is a fresh baseline, not a jump from wherever the phone
        // drifted to while stationary — counting that gap as distance would inflate the track.
        lastFix = null
    }

    /** Ends the recording and stops the foreground service; the caller reads [points] before
     *  or immediately after this to build the submission. */
    fun stop(): List<RecordedPoint> {
        if (state == RecordingState.IDLE) return points.toList()
        if (state == RecordingState.RECORDING) {
            accumulatedBeforePauseMs += SystemClock.elapsedRealtime() - recordingStartedAtRealtime
        }
        state = RecordingState.STOPPED
        locationManager?.removeUpdates(locationListener)
        locationManager = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
        return points.toList()
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
        points += RecordedPoint(
            lat = location.latitude,
            lon = location.longitude,
            elevationM = if (location.hasAltitude()) location.altitude.toFloat() else null,
            time = Instant.ofEpochMilli(location.time),
        )
        publish()
    }

    private fun publish() {
        onUpdate?.invoke(currentStats())
    }

    private fun buildNotification(): Notification {
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_ID, getString(R.string.recording_notification_channel), NotificationManager.IMPORTANCE_LOW),
        )
        return Notification.Builder(this, CHANNEL_ID)
            .setContentTitle(getString(R.string.recording_notification_title))
            .setSmallIcon(android.R.drawable.ic_menu_mylocation)
            .setOngoing(true)
            .build()
    }

    private companion object {
        const val CHANNEL_ID = "recording"
        const val NOTIFICATION_ID = 1

        /** A GPS-only recording of a walk/hike/ride doesn't need a fix every second — this
         *  keeps the point count, and the battery cost, sane over a multi-hour recording. */
        const val MIN_UPDATE_MS = 3_000L
        const val MIN_UPDATE_M = 5f
    }
}

