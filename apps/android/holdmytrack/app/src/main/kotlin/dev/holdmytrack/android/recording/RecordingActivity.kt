package dev.holdmytrack.android.recording

import android.net.Uri
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import dev.holdmytrack.android.recording.db.SyncStatus
import dev.holdmytrack.android.recording.db.toGpx
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Edit for one saved GPS recording — `RecordedActivitiesActivity`'s Edit button, launched with
 * [EXTRA_RECORDING_ID]. Recording itself has no screen: it's the map's record button and the
 * notification (`MainActivity`, `RecordingService`), which ask nothing, so this is where a
 * recording's name, type and description get set, and where its GPX can be downloaded.
 *
 * Every row here is unsynced — a synced recording is deleted from the device
 * (`SyncActivity.flushRecordedQueue`) — so there's no read-only state to render.
 */
class RecordingActivity : AppCompatActivity() {

    private lateinit var store: RecordedActivityStore

    private lateinit var nameField: EditText
    private lateinit var typeField: TextView
    private lateinit var descriptionField: EditText

    /** The raw `activity_type` the Type field currently holds — [typeField] only shows its
     *  formatted label. */
    private var selectedType = RecordingTypes.DEFAULT

    /** The account's server-side type counts for the Type picker, read once per screen;
     *  empty until that answers, or if it can't (offline), which still leaves a usable list. */
    private var serverTypeCounts: Map<String, Int> = emptyMap()

    private val editingId: String get() = intent.getStringExtra(EXTRA_RECORDING_ID)!!

    /** The system's own "save as" screen, so the user picks where the GPX lands (Downloads, a
     *  cloud drive) and the app needs no storage permission. */
    private val downloadLauncher = registerForActivityResult(
        ActivityResultContracts.CreateDocument(GPX_MIME_TYPE),
    ) { uri -> uri?.let(::writeGpx) }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_recording)
        store = RecordedActivityStore(this)

        nameField = findViewById(R.id.recording_name)
        typeField = findViewById(R.id.recording_type)
        descriptionField = findViewById(R.id.recording_description)
        val timeValue: TextView = findViewById(R.id.recording_stat_time_value)
        val distanceValue: TextView = findViewById(R.id.recording_stat_distance_value)

        typeField.setOnClickListener { openTypePicker() }
        findViewById<Button>(R.id.recording_save).setOnClickListener { onSave() }
        findViewById<Button>(R.id.recording_download).setOnClickListener { onDownload() }
        HoldMyTrackApi.activityTypeCounts { result -> result.onSuccess { serverTypeCounts = it } }

        lifecycleScope.launch {
            val record = store.get(editingId) ?: run { finish(); return@launch }
            nameField.setText(record.name)
            setType(record.activityType)
            descriptionField.setText(record.description)
            timeValue.text = RecordingFormat.duration(record.durationSeconds * 1000)
            distanceValue.text = RecordingFormat.distance(record.distanceMeters)
        }
    }

    private fun setType(type: String) {
        selectedType = type
        typeField.text = RecordingTypes.format(type).ifEmpty { type }
    }

    private fun openTypePicker() {
        lifecycleScope.launch {
            val known = RecordingTypes.known(serverTypeCounts, store.all())
            ActivityTypePicker.show(this@RecordingActivity, selectedType, known, ::setType)
        }
    }

    private fun onSave() {
        lifecycleScope.launch {
            val record = store.get(editingId) ?: return@launch
            val newType = selectedType
            // A queued row's type can be cleared back to "unknown" here (Edit stays open until
            // sync) — RecordedActivitiesActivity's checkbox gate only blocks queuing a row
            // that's *already* unknown, so without this, clearing the type on an
            // already-queued row would reopen the exact dedup gap that gate exists to close.
            // Demoting back to not-synced forces the same re-check on the next visit.
            val newStatus = if (newType == RecordingTypes.DEFAULT && record.syncStatus == SyncStatus.QUEUED) {
                SyncStatus.NOT_SYNCED
            } else {
                record.syncStatus
            }
            store.update(
                record.copy(
                    name = nameField.text.toString().trim(),
                    activityType = newType,
                    description = descriptionField.text.toString().trim(),
                    syncStatus = newStatus,
                ),
            )
            RecordingTypes.rememberLastUsed(this@RecordingActivity, newType)
            finish()
        }
    }

    private fun onDownload() {
        lifecycleScope.launch {
            val record = store.get(editingId) ?: return@launch
            val base = record.name.ifBlank { "holdmytrack-${record.id.take(8)}" }
            downloadLauncher.launch("${base.replace(UNSAFE_FILENAME, "_")}.gpx")
        }
    }

    private fun writeGpx(uri: Uri) {
        lifecycleScope.launch {
            val record = store.get(editingId) ?: return@launch
            val written = withContext(Dispatchers.IO) {
                runCatching {
                    contentResolver.openOutputStream(uri)?.use { it.write(record.toGpx().toByteArray()) } != null
                }.getOrDefault(false)
            }
            val message = if (written) R.string.recording_downloaded else R.string.recording_download_failed
            Toast.makeText(this@RecordingActivity, message, Toast.LENGTH_SHORT).show()
        }
    }

    companion object {
        const val EXTRA_RECORDING_ID = "recording_id"
        private const val GPX_MIME_TYPE = "application/gpx+xml"
        private val UNSAFE_FILENAME = Regex("[^A-Za-z0-9._ -]")
    }
}
