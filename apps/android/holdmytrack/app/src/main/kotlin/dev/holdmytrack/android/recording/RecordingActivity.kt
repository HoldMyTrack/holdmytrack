package dev.holdmytrack.android.recording

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.google.android.material.dialog.MaterialAlertDialogBuilder
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
 * Also the Save screen `RecordingService` opens the moment a recording stops ([saveIntent]):
 * the same form, titled "Save recording", with Discard in place of Download GPX. The row is
 * already stored with the account's last-used type by then, so Type arrives pre-filled and
 * Back keeps the recording as it is — only Discard removes it.
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

    private val isSaveScreen: Boolean get() = intent.getBooleanExtra(EXTRA_JUST_STOPPED, false)

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
        val download: Button = findViewById(R.id.recording_download)
        download.setOnClickListener { onDownload() }
        if (isSaveScreen) {
            setTitle(R.string.recording_save_title)
            download.visibility = View.GONE
            findViewById<Button>(R.id.recording_discard).apply {
                visibility = View.VISIBLE
                setOnClickListener { confirmDiscard() }
            }
        }
        HoldMyTrackApi.activityTypeCounts { result -> result.onSuccess { serverTypeCounts = it } }

        lifecycleScope.launch {
            val record = store.get(editingId) ?: run { finish(); return@launch }
            nameField.setText(record.name)
            setType(record.activityType)
            descriptionField.setText(record.description)
            timeValue.text = RecordingFormat.duration(record.durationSeconds * 1000)
            distanceValue.text = RecordingFormat.distance(resources, record.distanceMeters)
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
            store.update(
                record.copy(
                    name = nameField.text.toString().trim(),
                    activityType = newType,
                    description = descriptionField.text.toString().trim(),
                ),
            )
            RecordingTypes.rememberLastUsed(this@RecordingActivity, newType)
            finish()
        }
    }

    private fun confirmDiscard() {
        MaterialAlertDialogBuilder(this)
            .setTitle(R.string.recording_discard_confirm_title)
            .setMessage(R.string.recording_discard_confirm_message)
            .setPositiveButton(R.string.recording_discard) { _, _ ->
                lifecycleScope.launch {
                    store.delete(editingId)
                    finish()
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
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
        private const val EXTRA_JUST_STOPPED = "just_stopped"

        /** The Save screen for a recording that has just stopped — see the class doc. */
        fun saveIntent(context: Context, recordingId: String): Intent =
            Intent(context, RecordingActivity::class.java)
                .putExtra(EXTRA_RECORDING_ID, recordingId)
                .putExtra(EXTRA_JUST_STOPPED, true)
        private const val GPX_MIME_TYPE = "application/gpx+xml"
        private val UNSAFE_FILENAME = Regex("[^A-Za-z0-9._ -]")
    }
}
