package dev.fitmap.android.recording

import android.Manifest
import android.content.ComponentName
import android.content.Intent
import android.content.ServiceConnection
import android.content.pm.PackageManager
import android.os.Bundle
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.view.View
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.EditText
import android.widget.Spinner
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.lifecycle.lifecycleScope
import dev.fitmap.android.R
import dev.fitmap.android.net.FitMapApi
import java.util.Locale
import java.util.UUID
import kotlinx.coroutines.launch
import org.json.JSONArray
import org.json.JSONObject

/**
 * A plain start/pause/stop GPS recording for a casual, watch-free activity — `docs/VISION.md`
 * §1.1/§4.1's deliberately narrow scope: GPS only, no sensor data, no training metrics. The
 * actual recording lives in `RecordingService`, which outlives this screen going away (a
 * phone call, the user checking another app); this Activity is a view onto it plus the one
 * step the service can't do itself — building and submitting the finished wire payload
 * (`docs/adr/0007-in-app-gps-recording-submits-directly.md`).
 */
class RecordingActivity : AppCompatActivity() {

    private lateinit var permissionNotice: TextView
    private lateinit var grantPermission: Button
    private lateinit var nameField: EditText
    private lateinit var typeSpinner: Spinner
    private lateinit var descriptionField: EditText
    private lateinit var timeValue: TextView
    private lateinit var distanceValue: TextView
    private lateinit var altitudeValue: TextView
    private lateinit var speedValue: TextView
    private lateinit var status: TextView
    private lateinit var recordButton: Button
    private lateinit var pauseResumeButton: Button
    private lateinit var stopButton: Button

    private var service: RecordingService? = null
    private var externalId: String? = null

    /** GPS fixes only arrive every few seconds (`RecordingService.MIN_UPDATE_MS`); a live
     *  elapsed-time readout needs its own tick independent of that, or the clock would visibly
     *  stall between fixes instead of counting up smoothly. Distance/altitude/speed still only
     *  change on a real fix — this ticker only redraws the time each second. */
    private val tickHandler = Handler(Looper.getMainLooper())
    private val ticker = object : Runnable {
        override fun run() {
            service?.let { renderStats(it.currentStats()) }
            tickHandler.postDelayed(this, 1000)
        }
    }

    /** Captured from `RecordingService.stop()` so a failed submit can be retried without
     *  re-recording — the points already exist, only the network call failed. Non-null only
     *  between a stop and a confirmed submit. */
    private var pendingPoints: List<RecordedPoint>? = null

    private val permissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions(),
    ) { renderPermissionState() }

    private val connection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName, binder: IBinder) {
            val bound = (binder as RecordingService.LocalBinder).service
            service = bound
            bound.onUpdate = ::renderStats
            renderState(bound.state)
            renderStats(bound.currentStats())
        }

        override fun onServiceDisconnected(name: ComponentName) {
            service = null
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_recording)

        permissionNotice = findViewById(R.id.recording_permission_notice)
        grantPermission = findViewById(R.id.recording_grant_permission)
        nameField = findViewById(R.id.recording_name)
        typeSpinner = findViewById(R.id.recording_type)
        descriptionField = findViewById(R.id.recording_description)
        timeValue = findViewById(R.id.recording_stat_time_value)
        distanceValue = findViewById(R.id.recording_stat_distance_value)
        altitudeValue = findViewById(R.id.recording_stat_altitude_value)
        speedValue = findViewById(R.id.recording_stat_speed_value)
        status = findViewById(R.id.recording_status)
        recordButton = findViewById(R.id.recording_record)
        pauseResumeButton = findViewById(R.id.recording_pause_resume)
        stopButton = findViewById(R.id.recording_stop)

        typeSpinner.adapter = ArrayAdapter(this, android.R.layout.simple_spinner_dropdown_item, RecordingTypes.LABELS)

        grantPermission.setOnClickListener { requestPermissions() }
        recordButton.setOnClickListener { onRecord() }
        pauseResumeButton.setOnClickListener { onPauseResume() }
        stopButton.setOnClickListener { onStopClicked() }
    }

    override fun onStart() {
        super.onStart()
        bindService(Intent(this, RecordingService::class.java), connection, BIND_AUTO_CREATE)
        renderPermissionState()
    }

    override fun onStop() {
        tickHandler.removeCallbacks(ticker)
        service?.onUpdate = null
        unbindService(connection)
        super.onStop()
    }

    private fun hasLocationPermission() =
        ContextCompat.checkSelfPermission(this, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED

    private fun hasNotificationPermission() =
        ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

    private fun requestPermissions() {
        permissionLauncher.launch(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.POST_NOTIFICATIONS))
    }

    /** A foreground service with a location type throws at `startForeground` without
     *  `ACCESS_FINE_LOCATION` — checked before Record is ever reachable, not caught after. The
     *  notification permission is a courtesy, not a hard requirement (the service still runs
     *  without it, just invisibly), so only location gates the Record button itself. */
    private fun renderPermissionState() {
        val hasLocation = hasLocationPermission()
        permissionNotice.visibility = if (hasLocation) View.GONE else View.VISIBLE
        grantPermission.visibility = if (hasLocation) View.GONE else View.VISIBLE
        recordButton.isEnabled = hasLocation && service?.state == RecordingState.IDLE
        if (!hasNotificationPermission() && hasLocation) {
            // Not blocking — requested once alongside location rather than as a second dialog
            // the user has to clear later; the service runs without it, just invisibly.
            requestPermissions()
        }
    }

    private fun onRecord() {
        val bound = service ?: return
        if (!hasLocationPermission()) return
        externalId = UUID.randomUUID().toString()
        pendingPoints = null
        stopButton.setText(R.string.recording_stop)
        status.visibility = View.GONE
        ContextCompat.startForegroundService(this, Intent(this, RecordingService::class.java))
        bound.start()
        renderState(RecordingState.RECORDING)
    }

    private fun onPauseResume() {
        val bound = service ?: return
        when (bound.state) {
            RecordingState.RECORDING -> bound.pause()
            RecordingState.PAUSED -> bound.resume()
            else -> return
        }
        renderState(bound.state)
    }

    /** Handles both "Stop" (the recording is still active) and, once `stopButton` has been
     *  relabeled after a failed submit, "Retry" (the recording already stopped and its points
     *  are sitting in [pendingPoints]) — the same button, because at that point there is
     *  nothing left to record, only a submission to retry. */
    private fun onStopClicked() {
        val alreadyStopped = pendingPoints != null
        val points = if (alreadyStopped) pendingPoints!! else (service?.stop() ?: return)
        if (!alreadyStopped) renderState(RecordingState.STOPPED)
        submit(points)
    }

    private fun renderState(state: RecordingState) {
        val fieldsEditable = state == RecordingState.IDLE
        nameField.isEnabled = fieldsEditable
        typeSpinner.isEnabled = fieldsEditable
        descriptionField.isEnabled = fieldsEditable

        recordButton.visibility = if (state == RecordingState.IDLE) View.VISIBLE else View.GONE
        recordButton.isEnabled = hasLocationPermission() && state == RecordingState.IDLE
        val active = state == RecordingState.RECORDING || state == RecordingState.PAUSED
        pauseResumeButton.visibility = if (active) View.VISIBLE else View.GONE
        stopButton.visibility = if (active) View.VISIBLE else View.GONE
        pauseResumeButton.setText(if (state == RecordingState.PAUSED) R.string.recording_resume else R.string.recording_pause)

        tickHandler.removeCallbacks(ticker)
        if (state == RecordingState.RECORDING) tickHandler.post(ticker)
    }

    private fun renderStats(stats: RecordingStats) {
        val totalSeconds = stats.elapsedMs / 1000
        timeValue.text = String.format(
            Locale.US, "%d:%02d:%02d", totalSeconds / 3600, (totalSeconds % 3600) / 60, totalSeconds % 60,
        )
        distanceValue.text = String.format(Locale.US, "%.2f km", stats.distanceM / 1000.0)
        altitudeValue.text = stats.altitudeM?.let { String.format(Locale.US, "%.0f m", it) } ?: getString(R.string.recording_stat_placeholder)
        speedValue.text = String.format(Locale.US, "%.1f km/h", stats.speedMps * 3.6)
    }

    private fun submit(points: List<RecordedPoint>) {
        if (points.size < 2) {
            pendingPoints = null
            status.setText(R.string.recording_not_enough_points)
            status.visibility = View.VISIBLE
            return
        }
        val id = externalId ?: return
        pendingPoints = points
        status.setText(R.string.recording_submitting)
        status.visibility = View.VISIBLE
        stopButton.isEnabled = false

        val pointsArray = JSONArray()
        for (point in points) {
            val json = JSONObject()
                .put("lat", point.lat)
                .put("lon", point.lon)
                .put("time", point.time.toString())
            point.elevationM?.let { json.put("elevation_m", it) }
            pointsArray.put(json)
        }
        val activity = JSONObject()
            .put("external_id", id)
            .put("activity_type", RecordingTypes.wireValue(typeSpinner.selectedItemPosition))
            .put("name", nameField.text.toString().trim())
            .put("description", descriptionField.text.toString().trim())
            .put("points", pointsArray)

        lifecycleScope.launch {
            val result = runCatching { FitMapApi.syncActivities(listOf(activity), FitMapApi.SOURCE_RECORDED) }
            stopButton.isEnabled = true
            result.onSuccess {
                pendingPoints = null
                finish()
            }.onFailure { failure ->
                status.text = getString(R.string.recording_submit_failed, failure.message.orEmpty())
                // Stays visible (relabeled) so the already-recorded points can be resubmitted
                // without recording the activity a second time.
                stopButton.visibility = View.VISIBLE
                stopButton.setText(R.string.recording_retry)
            }
        }
    }
}
