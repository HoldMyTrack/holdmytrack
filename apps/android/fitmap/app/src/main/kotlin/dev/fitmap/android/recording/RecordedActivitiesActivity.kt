package dev.fitmap.android.recording

import android.app.AlertDialog
import android.content.Intent
import android.os.Bundle
import android.view.Gravity
import android.view.View
import android.widget.AdapterView
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.CheckBox
import android.widget.LinearLayout
import android.widget.Spinner
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import dev.fitmap.android.R
import dev.fitmap.android.net.Session
import dev.fitmap.android.recording.db.RecordedActivityRecord
import dev.fitmap.android.recording.db.RecordedActivityStore
import dev.fitmap.android.recording.db.SyncStatus
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.launch

/**
 * Every GPS Logger recording on this device, filterable by sync status and activity type —
 * the review-and-queue step the deferred-sync design added: `RecordingActivity`'s Stop no
 * longer submits anything, so this is where a recording actually gets marked to go out (the
 * per-row checkbox) and where `RecordingActivity`'s Edit button leads back to, reused as-is
 * (`docs/SPEC.md` FR-3.8 / `apps/android/docs/ROADMAP.md` Phase 7).
 *
 * Rows are built in code rather than from a row layout, matching `SyncStatusActivity`'s own
 * convention for the same reason it states there: no component vocabulary exists yet to build
 * one from (Phase 5 is that pass).
 */
class RecordedActivitiesActivity : AppCompatActivity() {

    private lateinit var store: RecordedActivityStore
    private lateinit var statusFilter: Spinner
    private lateinit var typeFilter: Spinner
    private lateinit var rowsContainer: LinearLayout
    private lateinit var emptyView: TextView
    private lateinit var demoNotice: TextView

    private var all: List<RecordedActivityRecord> = emptyList()

    /** Index-aligned with `statusFilter`'s adapter — null means "All". */
    private val statusValues = listOf(null, SyncStatus.NOT_SYNCED, SyncStatus.QUEUED, SyncStatus.SYNCED)

    /** Index-aligned with `typeFilter`'s adapter; rebuilt whenever the underlying activity
     *  types on file change, so null (index 0, "All types") is the only value guaranteed to
     *  survive a reload. */
    private var typeValues: List<String?> = listOf(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_recorded_activities)
        store = RecordedActivityStore(this)

        statusFilter = findViewById(R.id.recorded_filter_status)
        typeFilter = findViewById(R.id.recorded_filter_type)
        rowsContainer = findViewById(R.id.recorded_rows)
        emptyView = findViewById(R.id.recorded_empty)
        demoNotice = findViewById(R.id.recorded_demo_notice)

        statusFilter.adapter = dropdownAdapter(
            listOf(
                getString(R.string.recorded_filter_all),
                getString(R.string.recorded_status_not_synced),
                getString(R.string.recorded_status_queued),
                getString(R.string.recorded_status_synced),
            ),
        )
        val filterListener = object : AdapterView.OnItemSelectedListener {
            override fun onItemSelected(parent: AdapterView<*>?, view: View?, position: Int, id: Long) = render()
            override fun onNothingSelected(parent: AdapterView<*>?) {}
        }
        statusFilter.onItemSelectedListener = filterListener
        typeFilter.onItemSelectedListener = filterListener
    }

    /** Re-read on every resume, not just on create — returning from Edit (or from a Stop that
     *  just added a new row) is a resume, and there is no other signal that the underlying
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
        val previousValue = typeValues.getOrNull(typeFilter.selectedItemPosition)
        val distinctTypes = all.map { it.activityType }.distinct().sorted()
        typeValues = listOf(null) + distinctTypes
        typeFilter.adapter = dropdownAdapter(listOf(getString(R.string.recorded_filter_all)) + distinctTypes)
        typeFilter.setSelection(typeValues.indexOf(previousValue).coerceAtLeast(0), false)
    }

    private fun dropdownAdapter(labels: List<String>) =
        ArrayAdapter(this, android.R.layout.simple_spinner_item, labels).also {
            it.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item)
        }

    private fun render() {
        val statusValue = statusValues.getOrNull(statusFilter.selectedItemPosition)
        val typeValue = typeValues.getOrNull(typeFilter.selectedItemPosition)
        val filtered = all.filter { record ->
            (statusValue == null || record.syncStatus == statusValue) &&
                (typeValue == null || record.activityType == typeValue)
        }

        rowsContainer.removeAllViews()
        emptyView.visibility = if (filtered.isEmpty()) View.VISIBLE else View.GONE
        for (record in filtered) rowsContainer.addView(buildRow(record))
    }

    private fun buildRow(record: RecordedActivityRecord): View {
        val row = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(0, 20, 0, 20)
        }

        // A recording still on RecordingTypes.DEFAULT ("unknown") is blocked from queuing —
        // cross-source deduplication (docs/IMPLEMENTATION.md §4.6) requires an exact
        // activity-type match, and "unknown" never equals whatever a same-walk Health Connect
        // sync reports (typically a real type like "walking"), so a recording left untyped
        // silently defeats dedup rather than failing loudly. Gating it here, at the one place a
        // row can be queued, is cheaper than loosening the server's matcher and keeps "same
        // type" an exact, reliable signal for every source.
        val needsType = record.activityType == RecordingTypes.DEFAULT
        row.addView(
            CheckBox(this).apply {
                isChecked = record.syncStatus != SyncStatus.NOT_SYNCED
                // A demo account can record and manage rows locally — only sync itself is a
                // mutation the server rejects (requireNotDemo, services/server/internal/
                // httpapi/auth.go) — so queuing is blocked here too, matching the Sync
                // screen's own demo gate: no point letting a demo account queue something
                // "Sync Now" can never actually take.
                isEnabled = record.syncStatus != SyncStatus.SYNCED && !Session.isDemo && !needsType
                setOnCheckedChangeListener { _, checked ->
                    val newStatus = if (checked) SyncStatus.QUEUED else SyncStatus.NOT_SYNCED
                    lifecycleScope.launch {
                        store.setSyncStatus(record.id, newStatus)
                        all = all.map { if (it.id == record.id) it.copy(syncStatus = newStatus) else it }
                        render()
                    }
                }
            },
        )

        val previewSize = (48 * resources.displayMetrics.density).toInt()
        row.addView(
            TrackSilhouetteView(this).apply {
                setPoints(record.points)
                contentDescription = getString(R.string.recorded_row_preview)
            },
            LinearLayout.LayoutParams(previewSize, previewSize).apply { marginEnd = previewSize / 4 },
        )

        val textColumn = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
        }
        textColumn.addView(
            TextView(this).apply {
                text = record.name.ifBlank { formatDate(record.startedAtMs) }
                textSize = 15f
            },
        )
        textColumn.addView(
            TextView(this).apply {
                val subtitle = getString(
                    R.string.recorded_row_subtitle,
                    record.activityType,
                    record.distanceMeters / 1000.0,
                    statusLabel(record.syncStatus),
                )
                text = if (needsType && record.syncStatus != SyncStatus.SYNCED) {
                    "$subtitle · ${getString(R.string.recorded_row_needs_type)}"
                } else {
                    subtitle
                }
                textSize = 12f
            },
        )
        row.addView(textColumn)

        row.addView(
            Button(this).apply {
                text = getString(R.string.recording_edit)
                setOnClickListener {
                    startActivity(
                        Intent(this@RecordedActivitiesActivity, RecordingActivity::class.java)
                            .putExtra(RecordingActivity.EXTRA_RECORDING_ID, record.id),
                    )
                }
            },
        )

        // Always enabled, regardless of sync status — unlike Edit, which locks once synced,
        // there's nothing left for a synced row's own fields to protect against, and removing
        // a row from this device's list is a decision the user can always make. Local-only:
        // see RecordedActivityStore.delete's own doc comment for why a synced row's real
        // server-side activity isn't touched, and the confirmation dialog below says so too.
        row.addView(
            Button(this).apply {
                text = getString(R.string.recording_delete)
                setOnClickListener { confirmDelete(record) }
            },
        )

        return row
    }

    private fun confirmDelete(record: RecordedActivityRecord) {
        val message = if (record.syncStatus == SyncStatus.SYNCED) {
            R.string.recorded_delete_confirm_message_synced
        } else {
            R.string.recorded_delete_confirm_message_unsynced
        }
        AlertDialog.Builder(this)
            .setTitle(R.string.recorded_delete_confirm_title)
            .setMessage(message)
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
        SyncStatus.SYNCED -> getString(R.string.recorded_status_synced)
        else -> getString(R.string.recorded_status_not_synced)
    }

    private fun formatDate(epochMs: Long): String =
        DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM)
            .withZone(ZoneId.systemDefault())
            .format(Instant.ofEpochMilli(epochMs))
}
