package dev.holdmytrack.android.recording

import android.content.Intent
import android.view.LayoutInflater
import android.view.View
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.google.android.material.checkbox.MaterialCheckBox
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import dev.holdmytrack.android.R
import dev.holdmytrack.android.recording.db.RecordedActivityRecord
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import dev.holdmytrack.android.recording.db.SyncStatus
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.launch

/**
 * The Sync Source screen's Recorded activities list: every GPS recording on this device that
 * hasn't synced yet, one `item_recorded_activity` row each, between hairlines (after the web's
 * `.activities-panel__row`). Stop (`RecordingService`) only saves locally, so this is where a
 * recording gets checked to go out with the next "Sync now" and where Edit
 * (`RecordingActivity`) is reached. A row leaves the list once it syncs — it's deleted from the
 * device, and lives on the server from then on.
 *
 * The checkbox *is* the row's `SyncStatus` (QUEUED / NOT_SYNCED), written straight through to
 * `RecordedActivityStore`, so what's checked survives leaving the screen.
 */
class RecordedActivityRows(
    private val activity: AppCompatActivity,
    private val container: LinearLayout,
    private val emptyView: TextView,
    /** Called after anything that changes what "Sync now" would send — a check, an uncheck, a
     *  delete, a reload. */
    private val onChanged: () -> Unit,
) {

    private val store = RecordedActivityStore(activity)
    private var all: List<RecordedActivityRecord> = emptyList()

    /** False for the demo account: it can record and manage rows locally, but the server
     *  rejects its sync (requireNotDemo, services/server/internal/httpapi/auth.go), so there is
     *  no point letting it check something "Sync now" can never take. */
    var checkable: Boolean = true
        set(value) {
            field = value
            render()
        }

    val checkedCount: Int
        get() = all.count { it.syncStatus == SyncStatus.QUEUED }

    /** Re-read from the store — on every resume (returning from Edit is one), and after a sync
     *  that deleted the rows it sent. */
    fun reload() {
        activity.lifecycleScope.launch {
            all = store.all()
            render()
            onChanged()
        }
    }

    private fun render() {
        container.removeAllViews()
        emptyView.visibility = if (all.isEmpty()) View.VISIBLE else View.GONE
        container.visibility = if (all.isEmpty()) View.GONE else View.VISIBLE
        for (record in all) container.addView(buildRow(record))
    }

    private fun buildRow(record: RecordedActivityRecord): View {
        val row = LayoutInflater.from(activity).inflate(R.layout.item_recorded_activity, container, false)

        row.findViewById<MaterialCheckBox>(R.id.row_queued).apply {
            isChecked = record.syncStatus == SyncStatus.QUEUED
            isEnabled = checkable
            setOnCheckedChangeListener { _, checked ->
                val newStatus = if (checked) SyncStatus.QUEUED else SyncStatus.NOT_SYNCED
                activity.lifecycleScope.launch {
                    store.setSyncStatus(record.id, newStatus)
                    all = all.map { if (it.id == record.id) it.copy(syncStatus = newStatus) else it }
                    onChanged()
                }
            }
        }

        row.findViewById<TrackSilhouetteView>(R.id.row_preview).setPoints(record.points)
        val title = record.name.ifBlank { formatDate(record.startedAtMs) }
        row.findViewById<TextView>(R.id.row_title).text = title
        // Named per row: TalkBack reads each control on its own, and "Delete" alone doesn't
        // say which recording goes.
        row.findViewById<View>(R.id.row_queued).contentDescription = activity.getString(R.string.recorded_row_queue_named, title)
        row.findViewById<View>(R.id.row_edit).contentDescription = activity.getString(R.string.recorded_row_edit_named, title)
        row.findViewById<View>(R.id.row_delete).contentDescription = activity.getString(R.string.recorded_row_delete_named, title)
        row.findViewById<TextView>(R.id.row_meta).text = activity.getString(
            R.string.recorded_row_subtitle,
            record.activityType,
            RecordingFormat.distance(activity.resources, record.distanceMeters),
        )

        row.findViewById<View>(R.id.row_edit).setOnClickListener {
            activity.startActivity(
                Intent(activity, RecordingActivity::class.java)
                    .putExtra(RecordingActivity.EXTRA_RECORDING_ID, record.id),
            )
        }
        // Every row here is unsynced, so Delete discards the only copy — the confirmation
        // dialog below says so.
        row.findViewById<View>(R.id.row_delete).setOnClickListener { confirmDelete(record) }

        return row
    }

    private fun confirmDelete(record: RecordedActivityRecord) {
        MaterialAlertDialogBuilder(activity, R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(R.string.recorded_delete_confirm_title)
            .setMessage(R.string.recorded_delete_confirm_message)
            .setPositiveButton(R.string.recording_delete) { _, _ ->
                activity.lifecycleScope.launch {
                    store.delete(record.id)
                    all = all.filterNot { it.id == record.id }
                    render()
                    onChanged()
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun formatDate(epochMs: Long): String =
        DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT)
            .withZone(ZoneId.systemDefault())
            .format(Instant.ofEpochMilli(epochMs))
}
