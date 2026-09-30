package dev.holdmytrack.android.recording

import android.content.Intent
import android.view.LayoutInflater
import android.view.View
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import dev.holdmytrack.android.R
import dev.holdmytrack.android.panel.PanelFormat
import dev.holdmytrack.android.recording.db.RecordedActivityRecord
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.launch

/**
 * The Sync screen's Recorded activities list: every GPS recording on this device that hasn't
 * synced yet, one `item_recorded_activity` row each, between hairlines (after the web's
 * `.activities-panel__row`). Stop (`RecordingService`) only saves locally, so this is what the
 * next "Sync now" sends, and where Edit (`RecordingActivity`) is reached. A row leaves the list
 * once it syncs — it's deleted from the device, and lives on the server from then on.
 */
class RecordedActivityRows(
    private val activity: AppCompatActivity,
    private val container: LinearLayout,
    private val emptyView: TextView,
    /** Called after anything that changes what "Sync now" would send — a delete, a reload. */
    private val onChanged: () -> Unit,
) {

    private val store = RecordedActivityStore(activity)
    private var all: List<RecordedActivityRecord> = emptyList()

    val count: Int
        get() = all.size

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

        row.findViewById<TrackSilhouetteView>(R.id.row_preview).setPoints(record.points)
        val title = record.name.ifBlank { formatDate(record.startedAtMs) }
        row.findViewById<TextView>(R.id.row_title).text = title
        // Named per row: TalkBack reads each control on its own, and "Delete" alone doesn't
        // say which recording goes.
        row.findViewById<View>(R.id.row_edit).contentDescription = activity.getString(R.string.recorded_row_edit_named, title)
        row.findViewById<View>(R.id.row_delete).contentDescription = activity.getString(R.string.recorded_row_delete_named, title)
        // The Activities panel's row line: "[date · ] distance · duration · type", the date only
        // when a name took the title.
        val res = activity.resources
        row.findViewById<TextView>(R.id.row_meta).text = listOfNotNull(
            formatDate(record.startedAtMs).takeIf { record.name.isNotBlank() },
            PanelFormat.distance(res, record.distanceMeters),
            PanelFormat.duration(res, record.durationSeconds),
            RecordingTypes.format(res, record.activityType),
        ).joinToString(" · ")

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
