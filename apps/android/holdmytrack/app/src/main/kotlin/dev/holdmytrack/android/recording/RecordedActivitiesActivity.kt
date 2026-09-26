package dev.holdmytrack.android.recording

import android.content.Intent
import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.google.android.material.checkbox.MaterialCheckBox
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.textfield.MaterialAutoCompleteTextView
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.db.RecordedActivityRecord
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import dev.holdmytrack.android.recording.db.SyncStatus
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.launch

/**
 * Every GPS recording on this device that hasn't synced yet, filterable by sync status and
 * activity type — the review-and-queue step: Stop (`RecordingService`) only saves locally, so
 * this is where a recording actually gets marked to go out (the per-row checkbox) and where
 * Edit (`RecordingActivity`) is reached (`docs/SPEC.md` FR-3.8 / `apps/android/docs/ROADMAP.md`
 * Phase 7). A row leaves this list once it syncs — it's deleted from the device, and lives on
 * the server from then on.
 *
 * Designed after the web's Activities panel: two drop-down filters, then rows from
 * `item_recorded_activity` between hairlines (`.activities-panel__row`).
 */
class RecordedActivitiesActivity : AppCompatActivity() {

    private lateinit var store: RecordedActivityStore
    private lateinit var statusFilter: MaterialAutoCompleteTextView
    private lateinit var typeFilter: MaterialAutoCompleteTextView
    private lateinit var rowsContainer: LinearLayout
    private lateinit var emptyView: TextView
    private lateinit var demoNotice: TextView

    private var all: List<RecordedActivityRecord> = emptyList()

    /** Index-aligned with `statusFilter`'s items — null means "All". */
    private val statusValues = listOf(null, SyncStatus.NOT_SYNCED, SyncStatus.QUEUED)
    private var statusIndex = 0

    /** Index-aligned with `typeFilter`'s items; rebuilt whenever the underlying activity
     *  types on file change, so null (index 0, "All") is the only value guaranteed to
     *  survive a reload. */
    private var typeValues: List<String?> = listOf(null)
    private var typeIndex = 0

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_recorded_activities)
        store = RecordedActivityStore(this)

        statusFilter = findViewById(R.id.recorded_filter_status)
        typeFilter = findViewById(R.id.recorded_filter_type)
        rowsContainer = findViewById(R.id.recorded_rows)
        emptyView = findViewById(R.id.recorded_empty)
        demoNotice = findViewById(R.id.recorded_demo_notice)

        val statusLabels = listOf(
            getString(R.string.recorded_filter_all),
            getString(R.string.recorded_status_not_synced),
            getString(R.string.recorded_status_queued),
        )
        statusFilter.setSimpleItems(statusLabels.toTypedArray())
        statusFilter.setText(statusLabels[statusIndex], false)
        statusFilter.setOnItemClickListener { _, _, position, _ ->
            statusIndex = position
            render()
        }
        typeFilter.setOnItemClickListener { _, _, position, _ ->
            typeIndex = position
            render()
        }
    }

    /** Re-read on every resume, not just on create — returning from Edit (or from a Sync that
     *  just removed rows) is a resume, and there is no other signal that the underlying
     *  table changed. */
    override fun onResume() {
        super.onResume()
        // Re-checked every resume, not cached: returning from Profile after signing in or out
        // is a resume, and demo status can only really change that way.
        demoNotice.visibility = if (Session.isDemo) View.VISIBLE else View.GONE
        lifecycleScope.launch {
            all = store.all()
            rebuildTypeFilterOptions()
            render()
        }
    }

    private fun rebuildTypeFilterOptions() {
        val previousValue = typeValues.getOrNull(typeIndex)
        val distinctTypes = all.map { it.activityType }.distinct().sorted()
        typeValues = listOf(null) + distinctTypes
        val labels = listOf(getString(R.string.recorded_filter_all)) + distinctTypes
        typeIndex = typeValues.indexOf(previousValue).coerceAtLeast(0)
        typeFilter.setSimpleItems(labels.toTypedArray())
        typeFilter.setText(labels[typeIndex], false)
    }

    private fun render() {
        val statusValue = statusValues.getOrNull(statusIndex)
        val typeValue = typeValues.getOrNull(typeIndex)
        val filtered = all.filter { record ->
            (statusValue == null || record.syncStatus == statusValue) &&
                (typeValue == null || record.activityType == typeValue)
        }

        rowsContainer.removeAllViews()
        emptyView.visibility = if (filtered.isEmpty()) View.VISIBLE else View.GONE
        for (record in filtered) rowsContainer.addView(buildRow(record))
    }

    private fun buildRow(record: RecordedActivityRecord): View {
        val row = LayoutInflater.from(this).inflate(R.layout.item_recorded_activity, rowsContainer, false)

        row.findViewById<MaterialCheckBox>(R.id.row_queued).apply {
            isChecked = record.syncStatus != SyncStatus.NOT_SYNCED
            // A demo account can record and manage rows locally — only sync itself is a
            // mutation the server rejects (requireNotDemo, services/server/internal/
            // httpapi/auth.go) — so queuing is blocked here too, matching the Sync
            // screen's own demo gate: no point letting a demo account queue something
            // "Sync Now" can never actually take.
            isEnabled = !Session.isDemo
            setOnCheckedChangeListener { _, checked ->
                val newStatus = if (checked) SyncStatus.QUEUED else SyncStatus.NOT_SYNCED
                lifecycleScope.launch {
                    store.setSyncStatus(record.id, newStatus)
                    all = all.map { if (it.id == record.id) it.copy(syncStatus = newStatus) else it }
                    render()
                }
            }
        }

        row.findViewById<TrackSilhouetteView>(R.id.row_preview).setPoints(record.points)
        row.findViewById<TextView>(R.id.row_title).text = record.name.ifBlank { formatDate(record.startedAtMs) }
        row.findViewById<TextView>(R.id.row_meta).text = getString(
            R.string.recorded_row_subtitle,
            record.activityType,
            record.distanceMeters / 1000.0,
            statusLabel(record.syncStatus),
        )

        row.findViewById<View>(R.id.row_edit).setOnClickListener {
            startActivity(
                Intent(this, RecordingActivity::class.java)
                    .putExtra(RecordingActivity.EXTRA_RECORDING_ID, record.id),
            )
        }
        // Every row here is unsynced, so Delete discards the only copy — the confirmation
        // dialog below says so.
        row.findViewById<View>(R.id.row_delete).setOnClickListener { confirmDelete(record) }

        return row
    }

    private fun confirmDelete(record: RecordedActivityRecord) {
        MaterialAlertDialogBuilder(this)
            .setTitle(R.string.recorded_delete_confirm_title)
            .setMessage(R.string.recorded_delete_confirm_message)
            .setPositiveButton(R.string.recording_delete) { _, _ ->
                lifecycleScope.launch {
                    store.delete(record.id)
                    all = all.filterNot { it.id == record.id }
                    rebuildTypeFilterOptions()
                    render()
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun statusLabel(status: String) = when (status) {
        SyncStatus.QUEUED -> getString(R.string.recorded_status_queued)
        else -> getString(R.string.recorded_status_not_synced)
    }

    private fun formatDate(epochMs: Long): String =
        DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT)
            .withZone(ZoneId.systemDefault())
            .format(Instant.ofEpochMilli(epochMs))
}
