package dev.holdmytrack.android.recording

import android.Manifest
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.content.pm.PackageManager
import android.content.res.ColorStateList
import android.os.IBinder
import android.os.SystemClock
import android.view.HapticFeedbackConstants
import android.view.View
import android.widget.Chronometer
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AlertDialog
import androidx.core.content.ContextCompat
import androidx.fragment.app.Fragment
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.progressindicator.LinearProgressIndicator
import dev.holdmytrack.android.R
import dev.holdmytrack.android.recording.db.LiveRecordingJournal
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.location.modes.CameraMode
import org.maplibre.android.maps.MapLibreMap

/**
 * GPS recording on the map (`apps/android/docs/SPEC.md` FR-5.1): the record button in
 * `MainActivity`'s bottom bar, the status pill, the binding to `RecordingService`, the Stop and
 * leftover prompts, and the camera following the latest fix. The button taps to start, pause and
 * resume; a two-second hold stops ([RecordTapRules]). The service does the recording itself.
 *
 * Create it as a field of [fragment], so its permission request is registered before the fragment starts.
 */
class RecordingController(
    private val fragment: Fragment,
    /** The map's history layers and chrome follow the recording: [active] and its [points] so far. */
    private val onChanged: (active: Boolean, points: List<RecordedPoint>) -> Unit,
    /** Whether the live track and the camera can be drawn yet: the style has loaded. */
    private val styleLoaded: () -> Boolean,
    private val map: () -> MapLibreMap?,
    /** A start from another tab goes to the map, where the recording is drawn. */
    private val showMap: () -> Unit,
) {
    private val context: Context get() = fragment.requireContext()

    private lateinit var button: RecordButton
    private lateinit var status: View
    private lateinit var statusDot: View
    private lateinit var statusTime: Chronometer
    private lateinit var statusDetail: TextView
    private lateinit var statusHold: LinearProgressIndicator

    /** Bound from [start] to [stop]; null between the two, or before the bind answers. Null reads as idle. */
    private var recorder: RecordingService? = null
    private var bound = false

    /** Whether a hold on the button is counting down, which the pill shows in place of its detail line. */
    private var holdingToStop = false

    /** Whether the camera has flown in to this recording's first fix; later fixes only pan, so the
     *  user's chosen zoom sticks. */
    private var following = false

    /** The prompt for a recording a process death left behind, while it's up: every bind asks
     *  again, and returning to the map mustn't stack a second one. */
    private var leftoverDialog: AlertDialog? = null

    /** The notification's Stop confirmation, while it's up; and whether one is owed to a Stop that
     *  arrived before the bind answered. */
    private var stopDialog: AlertDialog? = null
    private var pendingStopConfirm = false

    private val connection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName, binder: IBinder) {
            val service = (binder as RecordingService.LocalBinder).service
            recorder = service
            service.onChange = ::render
            render()
            if (pendingStopConfirm) {
                pendingStopConfirm = false
                confirmStop()
            }
            if (service.state == RecordingState.IDLE) service.findLeftover(::offerLeftover)
        }

        override fun onServiceDisconnected(name: ComponentName) {
            recorder = null
            render()
        }
    }

    /** Asked for on the button's first tap, when the reason is obvious. Notifications are asked
     *  too (without them there's no Pause or Stop in the shade), but only location is required. */
    private val permissionLauncher = fragment.registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        if (granted(Manifest.permission.ACCESS_FINE_LOCATION)) {
            startRecording()
        } else {
            Toast.makeText(context, R.string.recording_needs_location, Toast.LENGTH_LONG).show()
        }
    }

    val state: RecordingState get() = recorder?.state ?: RecordingState.IDLE

    val isRecording: Boolean get() = state != RecordingState.IDLE

    /** Binds the views: the button is the host's, in the middle of its bottom bar, since the
     *  recording is reachable from every tab; the pill is the map's. */
    fun bind(root: View) {
        button = fragment.requireActivity().findViewById(R.id.record_button)
        button.setOnClickListener { onTap() }
        button.onHoldComplete = ::stopRecording
        button.onHoldProgress = ::renderHoldProgress
        status = root.findViewById(R.id.recording_status)
        root.findViewById<View>(R.id.recording_status_stop).setOnClickListener { confirmStop() }
        statusDot = root.findViewById(R.id.recording_status_dot)
        statusTime = root.findViewById(R.id.recording_status_time)
        statusDetail = root.findViewById(R.id.recording_status_detail)
        statusHold = root.findViewById(R.id.recording_status_hold)
        // The notification's own format rather than the Chronometer's, so the two agree.
        statusTime.setOnChronometerTickListener {
            it.text = RecordingFormat.duration(SystemClock.elapsedRealtime() - it.base)
        }
    }

    /** From the fragment's `onStart`: watch the recording. */
    fun start() {
        bound = context.bindService(Intent(context, RecordingService::class.java), connection, Context.BIND_AUTO_CREATE)
    }

    /** From the fragment's `onStop`. */
    fun stop() {
        if (!bound) return
        recorder?.onChange = null
        context.unbindService(connection)
        bound = false
        recorder = null
    }

    /** From the fragment's `onDestroyView`. */
    fun dismissDialogs() {
        leftoverDialog?.dismiss()
        stopDialog?.dismiss()
    }

    /** The notification's Stop comes through the app: the Save screen after a stop needs a
     *  foreground window, and the stop is confirmed first ([confirmStop]). */
    fun handleIntent(intent: Intent) {
        val fromHistory = intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0
        if (RecordTapRules.stopIntentApplies(intent.action == RecordingService.ACTION_STOP, fromHistory)) confirmStop()
    }

    /** Brings the button, the pill, the map and the camera in line with the recording: on bind, on
     *  every change and fix, and once the style has loaded. */
    fun render() {
        val state = state
        val active = state != RecordingState.IDLE
        val (icon, background, description) = when (state) {
            RecordingState.IDLE -> Triple(R.drawable.ic_record_start, R.drawable.bg_record_button, R.string.record_button_start)
            RecordingState.RECORDING -> Triple(R.drawable.ic_pause, R.drawable.bg_record_button_recording, R.string.record_button_pause)
            RecordingState.PAUSED -> Triple(R.drawable.ic_play, R.drawable.bg_record_button_paused, R.string.record_button_resume)
        }
        button.setImageResource(icon)
        // Pause and play are tinted for the button's surface; the idle red dot keeps its colour.
        button.imageTintList = when (state) {
            RecordingState.IDLE -> null
            RecordingState.RECORDING -> ColorStateList.valueOf(context.getColor(R.color.hmt_record))
            RecordingState.PAUSED -> ColorStateList.valueOf(context.getColor(R.color.hmt_record_paused))
        }
        button.setBackgroundResource(background)
        button.contentDescription = context.getString(description)
        button.holdEnabled = active
        button.pulsing = state == RecordingState.RECORDING
        sizeButton(active)
        if (!active) stopDialog?.dismiss()
        val points = if (active) recorder?.points().orEmpty() else emptyList()
        renderStatus(state, hasFix = points.isNotEmpty())
        onChanged(active, points)

        if (!styleLoaded()) return
        if (!active) {
            following = false
            return
        }
        val instance = map() ?: return
        val last = points.lastOrNull() ?: return
        val target = LatLng(last.lat, last.lon)
        if (!following) {
            following = true
            // The recording's follow owns the camera now, not Find my location's.
            if (instance.locationComponent.isLocationComponentActivated) {
                instance.locationComponent.cameraMode = CameraMode.NONE
            }
            val zoom = maxOf(instance.cameraPosition.zoom, RECORDING_ZOOM)
            instance.animateCamera(CameraUpdateFactory.newLatLngZoom(target, zoom), FOLLOW_DURATION_MS)
        } else {
            instance.easeCamera(CameraUpdateFactory.newLatLng(target))
        }
    }

    private fun granted(permission: String) =
        ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED

    /** Pausing says how to stop, since a tap is the obvious guess at "stop". Every tap has a
     *  haptic, so one that registered is never in doubt. */
    private fun onTap() {
        if (!isRecording) showMap()
        val missing = listOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.POST_NOTIFICATIONS).filterNot(::granted)
        when (val tap = RecordTapRules.tap(state, missing)) {
            RecordTap.Pause -> {
                button.performHapticFeedback(HapticFeedbackConstants.TOGGLE_OFF)
                Toast.makeText(context, R.string.record_hold_to_stop, Toast.LENGTH_SHORT).show()
                context.startService(RecordingService.intent(context, RecordingService.ACTION_TOGGLE))
            }
            RecordTap.Resume -> {
                button.performHapticFeedback(HapticFeedbackConstants.TOGGLE_ON)
                context.startService(RecordingService.intent(context, RecordingService.ACTION_TOGGLE))
            }
            RecordTap.Start -> startRecording()
            is RecordTap.AskPermissions -> permissionLauncher.launch(tap.missing.toTypedArray())
        }
    }

    /** A two-second hold stops, so a stray touch can pause but never end a recording.
     *  `RecordingService` then opens the Save screen. */
    private fun stopRecording() {
        context.startService(RecordingService.intent(context, RecordingService.ACTION_STOP))
    }

    /** A confirm haptic and a pop of the button, ahead of what [render] shows once the service reports in. */
    private fun startRecording() {
        button.performHapticFeedback(HapticFeedbackConstants.CONFIRM)
        button.animate().cancel()
        button.animate().scaleX(POP_SCALE).scaleY(POP_SCALE).setDuration(POP_HALF_MS).withEndAction {
            button.animate().scaleX(1f).scaleY(1f).setDuration(POP_HALF_MS)
        }
        ContextCompat.startForegroundService(context, RecordingService.intent(context, RecordingService.ACTION_START))
    }

    /** The notification's Stop asks first: a tap in the shade is easy to make by mistake, where the
     *  map's hold ([RecordButton]) is its own confirmation. Before the bind answers there are no
     *  stats to show, so the connection asks instead. */
    private fun confirmStop() {
        val service = recorder
        if (service == null) {
            pendingStopConfirm = true
            return
        }
        if (service.state == RecordingState.IDLE || stopDialog?.isShowing == true || fragment.requireActivity().isFinishing) return
        val stats = service.currentStats()
        stopDialog = MaterialAlertDialogBuilder(context, R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(R.string.recording_stop_confirm_title)
            .setMessage(
                context.getString(
                    R.string.recording_stop_confirm_message,
                    RecordingFormat.duration(stats.elapsedMs),
                    RecordingFormat.distance(context.resources, stats.distanceM),
                ),
            )
            .setPositiveButton(R.string.recording_stop) { _, _ -> stopRecording() }
            .setNegativeButton(R.string.recording_keep, null)
            .setOnDismissListener { stopDialog = null }
            .show()
    }

    /** A recording the process died during (`RecordingService.findLeftover`). Not cancellable: it
     *  would come back on the next bind. Resume returns paused; Save goes to the Save screen;
     *  Discard confirms first, since the journal is the only copy. */
    private fun offerLeftover(leftover: LiveRecordingJournal.Leftover) {
        if (leftoverDialog?.isShowing == true || fragment.requireActivity().isFinishing) return
        leftoverDialog = MaterialAlertDialogBuilder(context)
            .setTitle(R.string.recording_leftover_title)
            .setMessage(
                context.getString(
                    R.string.recording_leftover_message,
                    RecordingFormat.duration(leftover.movingMs),
                    RecordingFormat.distance(context.resources, leftover.distanceM),
                ),
            )
            .setCancelable(false)
            .setPositiveButton(R.string.recording_leftover_resume) { _, _ ->
                if (recorder?.resumeLeftover(leftover) != true) {
                    Toast.makeText(context, R.string.recording_needs_location, Toast.LENGTH_LONG).show()
                }
            }
            .setNeutralButton(R.string.recording_leftover_save) { _, _ -> recorder?.saveLeftover(leftover) }
            .setNegativeButton(R.string.recording_discard) { _, _ -> confirmDiscardLeftover(leftover) }
            .show()
    }

    private fun confirmDiscardLeftover(leftover: LiveRecordingJournal.Leftover) {
        leftoverDialog = MaterialAlertDialogBuilder(context, R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(R.string.recording_discard_confirm_title)
            .setMessage(R.string.recording_discard_confirm_message)
            .setCancelable(false)
            .setPositiveButton(R.string.recording_discard) { _, _ -> recorder?.discardLeftover(leftover) }
            .setNegativeButton(android.R.string.cancel) { _, _ ->
                // Still showing during its own button's callback; cleared so the guard lets it back.
                leftoverDialog = null
                offerLeftover(leftover)
            }
            .show()
    }

    /** 56dp idle, 60dp while recording: a bigger target for the hold that stops it. */
    private fun sizeButton(active: Boolean) {
        val density = context.resources.displayMetrics.density
        val size = ((if (active) BUTTON_ACTIVE_DP else BUTTON_IDLE_DP) * density).toInt()
        val params = button.layoutParams
        if (params.width == size) return
        params.width = size
        params.height = size
        button.layoutParams = params
    }

    /** The status pill while recording: a dot (amber when paused), the moving time, and the
     *  distance or "Waiting for GPS…". A hold on the button takes the detail line ([renderHoldProgress]). */
    private fun renderStatus(state: RecordingState, hasFix: Boolean) {
        val service = recorder
        if (state == RecordingState.IDLE || service == null) {
            statusTime.stop()
            status.visibility = View.GONE
            return
        }
        status.visibility = View.VISIBLE
        val recording = state == RecordingState.RECORDING
        val stats = service.currentStats()
        statusTime.base = SystemClock.elapsedRealtime() - stats.elapsedMs
        if (recording) {
            statusTime.start()
        } else {
            // Trap: setBase redraws in the Chronometer's own format, so the text is set after it.
            statusTime.stop()
            statusTime.text = RecordingFormat.duration(stats.elapsedMs)
        }
        statusDot.backgroundTintList = ColorStateList.valueOf(context.getColor(if (recording) R.color.hmt_record else R.color.hmt_record_paused))
        if (holdingToStop) return
        val distance = RecordingFormat.distance(context.resources, stats.distanceM)
        statusDetail.text = when {
            !recording -> context.getString(R.string.recording_status_paused, distance)
            !hasFix -> context.getString(R.string.recording_status_waiting)
            else -> distance
        }
    }

    /** The hold's countdown in the pill too, where the finger on the button can't hide it. */
    private fun renderHoldProgress(progress: Float) {
        val holding = progress > 0f
        statusHold.progress = (progress * statusHold.max).toInt()
        if (holding == holdingToStop) return
        holdingToStop = holding
        statusHold.visibility = if (holding) View.VISIBLE else View.GONE
        if (holding) {
            statusDetail.setText(R.string.recording_status_holding)
        } else {
            renderStatus(state, hasFix = recorder?.points()?.isNotEmpty() == true)
        }
    }

    companion object {
        private const val BUTTON_IDLE_DP = 56f
        private const val BUTTON_ACTIVE_DP = 60f
        private const val POP_SCALE = 1.15f
        private const val POP_HALF_MS = 100L

        /** The zoom on a recording's first fix: street level, so the line visibly grows. */
        private const val RECORDING_ZOOM = 16.0
        private const val FOLLOW_DURATION_MS = 900
    }
}
