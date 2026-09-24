package dev.holdmytrack.android

import android.os.Bundle
import android.os.Handler
import android.os.Looper
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
    private lateinit var rows: LinearLayout
    private lateinit var duplicatesHeading: TextView
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
        summary = findViewById(R.id.status_summary)
        rows = findViewById(R.id.status_rows)
        duplicatesHeading = findViewById(R.id.status_duplicates_heading)
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
                summary.text = getString(R.string.status_failed, it.message.orEmpty())
            }
        }
        HoldMyTrackApi.duplicates { result ->
            result.onSuccess { renderDuplicates(it) }.onFailure { duplicatesHeading.visibility = View.GONE }
        }
    }

    private fun render(history: SyncHistory) {
        summary.text = when {
            history.processing > 0 ->
                getString(R.string.status_summary_processing, history.processing, history.total)
            history.total == 0L -> getString(R.string.status_summary_empty)
            else -> getString(R.string.status_summary_settled, history.total)
        }

        rows.removeAllViews()
        for (entry in history.entries) {
            rows.addView(row(describe(entry), entry.status == "failed"))
        }

        // Only while something is in flight, and never twice over: every load can schedule at
        // most the one pending callback.
        main.removeCallbacks(poll)
        polling = history.processing > 0
        if (polling) main.postDelayed(poll, POLL_INTERVAL_MS)
    }

    /**
     * One history row as a line of text. The label is the job's own `source_detail`, which for
     * a synced activity is the Health Connect record id — no use to anyone on its own, so a
     * finished job is described by what it actually became (when it happened, how far) and the
     * id is left out of it.
     */
    private fun describe(entry: SyncHistoryEntry): String {
        val when_ = entry.startedAt?.let { format(it) } ?: format(entry.submittedAt)
        return when (entry.status) {
            "done" -> {
                val distance = entry.distanceMeters
                if (distance != null) {
                    getString(R.string.status_row_done, when_, distance / 1000.0)
                } else {
                    getString(R.string.status_row_done_no_distance, when_)
                }
            }
            "failed" -> getString(
                R.string.status_row_failed,
                when_,
                entry.error.ifBlank { getString(R.string.status_no_reason) },
            )
            else -> getString(R.string.status_row_processing, when_)
        }
    }

    private fun renderDuplicates(duplicates: List<Duplicate>) {
        duplicatesHeading.visibility = if (duplicates.isEmpty()) View.GONE else View.VISIBLE
        duplicateRows.removeAllViews()
        for (duplicate in duplicates) {
            duplicateRows.addView(
                row(
                    getString(
                        R.string.status_duplicate_row,
                        format(duplicate.startedAt),
                        duplicate.activityType,
                        sourceName(duplicate.source),
                        sourceName(duplicate.supersededBySource),
                    ),
                    false,
                ),
            )
        }
    }

    private fun row(text: String, failed: Boolean): TextView =
        TextView(this).apply {
            this.text = text
            textSize = 13f
            setPadding(0, 8, 0, 8)
            if (failed) setTextColor(FAILED_COLOR)
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
        const val FAILED_COLOR = 0xFFEF5350.toInt()
        val DATE_FORMAT: DateTimeFormatter =
            DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM).withZone(ZoneId.systemDefault())
    }
}
