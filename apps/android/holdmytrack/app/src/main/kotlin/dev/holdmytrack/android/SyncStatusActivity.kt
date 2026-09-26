package dev.holdmytrack.android

import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.LayoutInflater
import android.view.View
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import dev.holdmytrack.android.net.Duplicate
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.SyncHistory
import dev.holdmytrack.android.net.SyncHistoryEntry
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/**
 * Sync history, pending work, and why an activity is not on the map.
 *
 * It reads `GET /v1/uploads`, which lists **ingest jobs**, not syncs — a Health Connect session
 * and an uploaded file are the same kind of job, so they share one history rather than the app
 * inventing a separate notion of "a sync" that the server has no record of.
 *
 * The duplicates section is the other half of the same question. Once a second ingest path
 * exists, an activity can be missing for two quite different reasons — it failed, or it was
 * already here from somewhere else — and the second is not a failure at all
 * (`docs/IMPLEMENTATION.md` §4.6). Showing only errors would leave a user hunting for a fault
 * that isn't there.
 */
class SyncStatusActivity : AppCompatActivity() {

    private lateinit var summary: TextView
    private lateinit var error: TextView
    private lateinit var empty: View
    private lateinit var rows: LinearLayout
    private lateinit var duplicatesHead: View
    private lateinit var duplicateRows: LinearLayout

    private val main = Handler(Looper.getMainLooper())
    private var polling = false

    /** Re-reads while anything is still processing, and only then — a job that has finished
     *  is not going to change again, so a screen of settled rows costs no requests at all. */
    private val poll = object : Runnable {
        override fun run() {
            load()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_sync_status)
        // The two list heads are the same include, so each one's title and summary are looked
        // up inside it rather than by a screen-wide id.
        val head: View = findViewById(R.id.status_head)
        head.findViewById<TextView>(R.id.list_title).setText(R.string.status_history)
        summary = head.findViewById(R.id.list_summary)
        // Until the first page answers, so a slow network reads as loading rather than empty.
        summary.setText(R.string.status_loading)
        error = findViewById(R.id.status_error)
        empty = findViewById(R.id.status_empty)
        rows = findViewById(R.id.status_rows)
        duplicatesHead = findViewById(R.id.status_duplicates_head)
        duplicatesHead.findViewById<TextView>(R.id.list_title).setText(R.string.status_duplicates_heading)
        duplicateRows = findViewById(R.id.status_duplicates)
    }

    override fun onResume() {
        super.onResume()
        load()
    }

    override fun onStop() {
        main.removeCallbacks(poll)
        polling = false
        super.onStop()
    }

    private fun load() {
        HoldMyTrackApi.syncHistory(HISTORY_PAGE) { result ->
            result.onSuccess { render(it) }.onFailure {
                error.text = getString(R.string.status_failed, it.message.orEmpty())
                error.visibility = View.VISIBLE
            }
        }
        HoldMyTrackApi.duplicates { result ->
            result.onSuccess { renderDuplicates(it) }.onFailure { renderDuplicates(emptyList()) }
        }
    }

    private fun render(history: SyncHistory) {
        error.visibility = View.GONE
        summary.text = when {
            history.processing > 0 ->
                getString(R.string.status_summary_processing, history.processing, history.total)
            history.total == 0L -> ""
            else -> getString(R.string.status_summary_settled, history.total)
        }
        empty.visibility = if (history.total == 0L) View.VISIBLE else View.GONE

        rows.removeAllViews()
        for (entry in history.entries) {
            addHistoryRow(entry)
        }

        // Only while something is in flight, and never twice over: every load can schedule at
        // most the one pending callback.
        main.removeCallbacks(poll)
        polling = history.processing > 0
        if (polling) main.postDelayed(poll, POLL_INTERVAL_MS)
    }

    /**
     * One history row: when, on the left, and what came of it on the right. The label is the
     * job's own `source_detail`, which for a synced activity is the Health Connect record id —
     * no use to anyone on its own, so a row is named by when the activity happened (or, until
     * it has been processed, when it was sent) and the id is left out of it.
     */
    private fun addHistoryRow(entry: SyncHistoryEntry) {
        val name = entry.startedAt?.let { format(it) } ?: format(entry.submittedAt)
        val failed = entry.status == "failed"
        val status = when (entry.status) {
            "done" -> entry.distanceMeters?.let { getString(R.string.status_row_distance, it / 1000.0) }
                ?: getString(R.string.status_row_no_distance)
            "failed" -> entry.error.ifBlank { getString(R.string.status_no_reason) }
            else -> getString(R.string.status_row_processing)
        }
        rows.addView(row(rows, name, status, detail = null, failed = failed))
    }

    private fun renderDuplicates(duplicates: List<Duplicate>) {
        val visibility = if (duplicates.isEmpty()) View.GONE else View.VISIBLE
        duplicatesHead.visibility = visibility
        duplicateRows.visibility = visibility
        duplicateRows.removeAllViews()
        for (duplicate in duplicates) {
            duplicateRows.addView(
                row(
                    duplicateRows,
                    name = format(duplicate.startedAt),
                    status = getString(R.string.status_duplicate_what, duplicate.activityType, sourceName(duplicate.source)),
                    detail = getString(R.string.status_duplicate_why, sourceName(duplicate.supersededBySource)),
                    failed = false,
                ),
            )
        }
    }

    /** A row from `item_sync_history_row`: the web's `.sync-tab__row`, with a failure's reason
     *  in the danger color, as `.sync-tab__row-status--error` sets it. */
    private fun row(parent: LinearLayout, name: String, status: String, detail: String?, failed: Boolean): View =
        LayoutInflater.from(this).inflate(R.layout.item_sync_history_row, parent, false).apply {
            findViewById<TextView>(R.id.row_name).text = name
            findViewById<TextView>(R.id.row_status).apply {
                text = status
                if (failed) setTextColor(getColor(R.color.hmt_danger))
            }
            findViewById<TextView>(R.id.row_detail).apply {
                text = detail
                visibility = if (detail == null) View.GONE else View.VISIBLE
            }
        }

    /** The `source` values §3.3 defines, said the way a person would say them. */
    private fun sourceName(source: String): String = when (source) {
        "healthconnect" -> getString(R.string.source_health_connect)
        "healthkit" -> getString(R.string.source_health_kit)
        "upload" -> getString(R.string.source_upload)
        "takeout" -> getString(R.string.source_takeout)
        "recorded" -> getString(R.string.source_recorded)
        else -> source
    }

    private fun format(isoInstant: String): String =
        runCatching { DATE_FORMAT.format(Instant.parse(isoInstant)) }.getOrDefault(isoInstant)

    private companion object {
        const val HISTORY_PAGE = 25
        const val POLL_INTERVAL_MS = 2_000L
        val DATE_FORMAT: DateTimeFormatter =
            DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT).withZone(ZoneId.systemDefault())
    }
}
