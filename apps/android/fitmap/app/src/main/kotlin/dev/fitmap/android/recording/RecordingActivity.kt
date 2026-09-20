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
import android.widget.AutoCompleteTextView
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.lifecycle.lifecycleScope
import dev.fitmap.android.R
import dev.fitmap.android.recording.db.RecordedActivityRecord
import dev.fitmap.android.recording.db.RecordedActivityStore
import dev.fitmap.android.recording.db.SyncStatus
import java.util.Locale
import java.util.UUID
import kotlinx.coroutines.launch

/**
 * A plain start/pause/stop GPS recording for a casual, watch-free activity — `docs/VISION.md`
 * §1.1/§4.1's deliberately narrow scope: GPS only, no sensor data, no training metrics. The
 * actual recording lives in `RecordingService`, which outlives this screen going away (a
 * phone call, the user checking another app); this Activity is a view onto it plus the one
 * step the service can't do itself — saving the finished recording locally.
 *
 * **Two modes, one layout and one class**, per the deferred-sync design: a plain launch
 * (no extra) is a fresh recording; a launch carrying [EXTRA_RECORDING_ID] is
 * `RecordedActivitiesActivity`'s Edit button, reopening this same screen against an
 * already-stopped, locally-saved row instead of starting a new one. The Record/Pause/Stop row
 * and the Save button never show at once, since the two modes never overlap.
 *
 * **Stop no longer submits anything.** `docs/adr/0007-in-app-gps-recording-submits-directly.md`
 * still holds — a finished recording still goes straight to `POST /v1/sync/activities` with
 * no Health Connect round-trip — but *when* moved: Stop now only saves the recording locally
 * (`RecordedActivityStore`, status [SyncStatus.NOT_SYNCED]); actually submitting it is
 * `RecordedActivitiesActivity`'s sync checkbox plus the Sync screen's existing "Sync Now".
 */
class RecordingActivity : AppCompatActivity() {

    private lateinit var store: RecordedActivityStore

    private lateinit var permissionNotice: TextView
    private lateinit var grantPermission: Button
    private lateinit var nameField: EditText
    private lateinit var typeField: AutoCompleteTextView
    private lateinit var descriptionField: EditText
    private lateinit var timeValue: TextView
    private lateinit var distanceValue: TextView
    private lateinit var altitudeValue: TextView
    private lateinit var speedValue: TextView
    private lateinit var status: TextView
    private lateinit var recordButton: Button
    private lateinit var pauseResumeButton: Button
    private lateinit var stopButton: Button
    private lateinit var saveButton: Button

    /** Null for a fresh recording; the row being edited otherwise. */
    private val editingId: String? get() = intent.getStringExtra(EXTRA_RECORDING_ID)
    private val editMode: Boolean get() = editingId != null

    // --- Record-mode-only state -------------------------------------------------------

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
        store = RecordedActivityStore(this)

        permissionNotice = findViewById(R.id.recording_permission_notice)
        grantPermission = findViewById(R.id.recording_grant_permission)
        nameField = findViewById(R.id.recording_name)
        typeField = findViewById(R.id.recording_type)
        descriptionField = findViewById(R.id.recording_description)
        timeValue = findViewById(R.id.recording_stat_time_value)
        distanceValue = findViewById(R.id.recording_stat_distance_value)
        altitudeValue = findViewById(R.id.recording_stat_altitude_value)
        speedValue = findViewById(R.id.recording_stat_speed_value)
        status = findViewById(R.id.recording_status)
        recordButton = findViewById(R.id.recording_record)
        pauseResumeButton = findViewById(R.id.recording_pause_resume)
        stopButton = findViewById(R.id.recording_stop)
        saveButton = findViewById(R.id.recording_save)

        typeField.setAdapter(ArrayAdapter(this, android.R.layout.simple_dropdown_item_1line, RecordingTypes.PRESETS))
        typeField.threshold = 0
        typeField.setOnClickListener { typeField.showDropDown() }

        if (editMode) setUpEditMode() else setUpRecordMode()
    }

    // --- Edit mode ----------------------------------------------------------------------

    private fun setUpEditMode() {
        recordButton.visibility = View.GONE
        pauseResumeButton.visibility = View.GONE
        stopButton.visibility = View.GONE
        saveButton.visibility = View.VISIBLE
        saveButton.setOnClickListener { onSaveEdit() }

        lifecycleScope.launch {
            val record = store.get(editingId!!) ?: run { finish(); return@launch }
            nameField.setText(record.name)
            typeField.setText(record.activityType, false)
            descriptionField.setText(record.description)
            renderStats(RecordingStats(
                elapsedMs = record.durationSeconds * 1000,
                distanceM = record.distanceMeters,
                altitudeM = null,
                speedMps = 0.0,
            ))
            if (!record.editable) {
                nameField.isEnabled = false
                typeField.isEnabled = false
                descriptionField.isEnabled = false
                saveButton.visibility = View.GONE
                status.setText(R.string.recording_locked_synced)
                status.visibility = View.VISIBLE
            }
        }
    }

    private fun onSaveEdit() {
        val id = editingId ?: return
        lifecycleScope.launch {
            val record = store.get(id) ?: return@launch
            store.update(
                record.copy(
                    name = nameField.text.toString().trim(),
                    activityType = typeField.text.toString().trim().ifEmpty { RecordingTypes.DEFAULT },
                    description = descriptionField.text.toString().trim(),
                ),
            )
            finish()
        }
    }

    // --- Record mode --------------------------------------------------------------------

    private fun setUpRecordMode() {
        typeField.setText(RecordingTypes.DEFAULT, false)

        grantPermission.setOnClickListener { requestPermissions() }
        recordButton.setOnClickListener { onRecord() }
        pauseResumeButton.setOnClickListener { onPauseResume() }
        stopButton.setOnClickListener { onStopClicked() }
    }

    override fun onStart() {
        super.onStart()
        if (editMode) return
        bindService(Intent(this, RecordingService::class.java), connection, BIND_AUTO_CREATE)
        renderPermissionState()
    }

    override fun onStop() {
        if (!editMode) {
            tickHandler.removeCallbacks(ticker)
            service?.onUpdate = null
            unbindService(connection)
        }
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

    private fun onStopClicked() {
        val bound = service ?: return
        val points = bound.stop()
        val stats = bound.currentStats()
        renderState(RecordingState.STOPPED)

        if (points.size < 2) {
            status.setText(R.string.recording_not_enough_points)
            status.visibility = View.VISIBLE
            return
        }
        val id = externalId ?: return
        lifecycleScope.launch {
            store.insert(
                RecordedActivityRecord(
                    id = id,
                    name = nameField.text.toString().trim(),
                    description = descriptionField.text.toString().trim(),
                    activityType = typeField.text.toString().trim().ifEmpty { RecordingTypes.DEFAULT },
                    startedAtMs = points.first().time.toEpochMilli(),
                    distanceMeters = stats.distanceM,
                    durationSeconds = stats.elapsedMs / 1000,
                    points = points,
                    syncStatus = SyncStatus.NOT_SYNCED,
                    createdAtMs = System.currentTimeMillis(),
                ),
            )
            Toast.makeText(this@RecordingActivity, R.string.recording_saved_locally, Toast.LENGTH_SHORT).show()
            finish()
        }
    }

    private fun renderState(state: RecordingState) {
        val fieldsEditable = state == RecordingState.IDLE
        nameField.isEnabled = fieldsEditable
        typeField.isEnabled = fieldsEditable
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
        altitudeValue.text = stats.altitudeM?.let { String.format(Locale.US, "%.0f m", it) }
            ?: getString(R.string.recording_stat_placeholder)
        speedValue.text = String.format(Locale.US, "%.1f km/h", stats.speedMps * 3.6)
    }

    companion object {
        const val EXTRA_RECORDING_ID = "recording_id"
    }
}
