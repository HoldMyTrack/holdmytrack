package dev.holdmytrack.android

import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.format.DateFormat
import android.view.LayoutInflater
import android.view.View
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import com.google.android.material.button.MaterialButton
import dev.holdmytrack.android.net.Duplicate
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.SyncHistory
import dev.holdmytrack.android.net.SyncHistoryEntry
import dev.holdmytrack.android.recording.RecordingFormat
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/**
 * Sync history, pending work, and why an activity is not on the map.
 *
 * It reads `GET /v1/uploads`, which lists **ingest jobs**, not syncs — a Health Connect session
 * and an uploaded file are the same kind of job, so they share one history rather than the app
 * inventing a separate notion of "a sync" that the server has no record of.
 *
 * The list is the web's Sync tab history (`apps/web/src/ui/SyncTab.tsx`), row for row: the same
 * page size, the same titles, statuses and "date · distance" line, View on map, and the same
 * pager — one account's history should read the same on either client.
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
    private lateinit var pager: View
    private lateinit var range: TextView
    private lateinit var previous: MaterialButton
    private lateinit var next: MaterialButton
    private lateinit var duplicatesHead: View
    private lateinit var duplicateRows: LinearLayout

    private val main = Handler(Looper.getMainLooper())
    private var polling = false

    /** The page in view, as the web's `useUploadHistory` offset — kept across a rotation. */
    private var offset = 0

    /** Bumped by every history read, so a page that answers after a newer one was asked for
     *  is dropped rather than drawn over it. */
    private var generation = 0

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
        pager = findViewById(R.id.status_pager)
        range = findViewById(R.id.status_range)
        previous = findViewById(R.id.status_previous)
        next = findViewById(R.id.status_next)
        previous.setOnClickListener { showPage(maxOf(0, offset - HISTORY_PAGE)) }
        next.setOnClickListener { showPage(offset + HISTORY_PAGE) }
        offset = savedInstanceState?.getInt(STATE_OFFSET) ?: 0
        duplicatesHead = findViewById(R.id.status_duplicates_head)
        duplicatesHead.findViewById<TextView>(R.id.list_title).setText(R.string.status_duplicates_heading)
        duplicateRows = findViewById(R.id.status_duplicates)
    }

    override fun onResume() {
        super.onResume()
        load()
        HoldMyTrackApi.duplicates { result ->
            result.onSuccess { renderDuplicates(it) }.onFailure { renderDuplicates(emptyList()) }
        }
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.putInt(STATE_OFFSET, offset)
    }

    override fun onStop() {
        main.removeCallbacks(poll)
        polling = false
        super.onStop()
    }

    private fun showPage(next: Int) {
        offset = next
        main.removeCallbacks(poll)
        load()
    }

    private fun load() {
        val gen = ++generation
        HoldMyTrackApi.syncHistory(HISTORY_PAGE, offset) { result ->
            if (gen != generation) return@syncHistory
            result.onSuccess { render(it) }.onFailure {
                error.text = getString(R.string.status_failed, it.message.orEmpty())
                error.visibility = View.VISIBLE
            }
        }
    }

    private fun render(history: SyncHistory) {
        error.visibility = View.GONE
        // "12 activities · 2 in progress" — the web's list summary.
        val total = history.total.toInt()
        val count = resources.getQuantityString(R.plurals.status_count, total, total)
        summary.text = if (history.processing > 0) {
            count + " · " + getString(R.string.status_in_progress, history.processing.toInt())
        } else {
            count
        }
        empty.visibility = if (history.entries.isEmpty()) View.VISIBLE else View.GONE

        rows.removeAllViews()
        for (entry in history.entries) {
            addHistoryRow(entry)
        }
        renderPager(history)

        // Only while something is in flight, and never twice over: every load can schedule at
        // most the one pending callback.
        main.removeCallbacks(poll)
        polling = history.processing > 0
        if (polling) main.postDelayed(poll, POLL_INTERVAL_MS)
    }

    /** The web's `.sync-tab__pager`: shown only once the history runs past one page. */
    private fun renderPager(history: SyncHistory) {
        if (history.total <= history.limit) {
            pager.visibility = View.GONE
            return
        }
        val end = minOf(history.offset.toLong() + history.limit, history.total)
        pager.visibility = View.VISIBLE
        range.text = getString(R.string.status_range, history.offset + 1, end.toInt(), history.total.toInt())
        previous.isEnabled = history.offset > 0
        next.isEnabled = end < history.total
    }

    /**
     * One history row, as the web's SyncTab draws it: a file's own name, or for anything synced
     * the source it came from — a synced row's `filename` is a raw external id (a Health Connect
     * record id), never meant to be read. On the right the status, then for a finished row
     * "9 Sep · 34.7 km" and View on map.
     */
    private fun addHistoryRow(entry: SyncHistoryEntry) {
        val title = when (entry.source) {
            "upload", "takeout" -> entry.filename
            else -> sourceTitle(entry.source)
        }
        val view = when (entry.status) {
            "done" -> row(rows, title, getString(R.string.status_row_ready), detail = meta(entry), failed = false).apply {
                findViewById<TextView>(R.id.row_status).setTextColor(getColor(R.color.hmt_success))
            }
            "failed" -> {
                val status = if (entry.error.isBlank()) {
                    getString(R.string.status_row_failed)
                } else {
                    getString(R.string.status_row_failed_with, entry.error)
                }
                row(rows, title, status, detail = null, failed = true)
            }
            else -> row(rows, title, getString(R.string.status_row_processing), detail = null, failed = false)
        }
        val startedAt = entry.startedAt
        if (entry.status == "done" && entry.activityId != null && startedAt != null) {
            view.findViewById<TextView>(R.id.row_action).apply {
                visibility = View.VISIBLE
                setOnClickListener { viewOnMap(startedAt) }
            }
        }
        rows.addView(view)
    }

    /** "9 Sep · 34.7 km", the web's `.sync-tab__row-meta` — only when both halves are known. */
    private fun meta(entry: SyncHistoryEntry): String? {
        val startedAt = entry.startedAt ?: return null
        val meters = entry.distanceMeters ?: return null
        val date = runCatching { shortDate().format(Instant.parse(startedAt)) }.getOrDefault(startedAt)
        return date + " · " + RecordingFormat.distance(resources, meters)
    }

    /** Back to the map on the activity's day, flown to its tracks — the web's View on map. The
     *  day is the phone's, as the map's own date range is. */
    private fun viewOnMap(startedAt: String) {
        val day = runCatching { Instant.parse(startedAt).atZone(ZoneId.systemDefault()).toLocalDate().toString() }
            .getOrNull() ?: return
        startActivity(MainActivity.showDay(this, day))
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

    /** A history row's title for anything that isn't a file — the web's `formatSourceLabel`. */
    private fun sourceTitle(source: String): String = when (source) {
        "healthconnect" -> getString(R.string.source_health_connect)
        "healthkit" -> getString(R.string.source_health_kit)
        "upload" -> getString(R.string.source_title_upload)
        "takeout" -> getString(R.string.source_title_takeout)
        "recorded" -> getString(R.string.source_title_recorded)
        else -> source
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

    /** "Sep 9" / "9 сент." — the web's `formatShortDate`, in the app's language. Built per
     *  call, since the per-app language can change while the app runs. */
    private fun shortDate(): DateTimeFormatter {
        val locale = resources.configuration.locales[0] ?: Locale.getDefault()
        return DateTimeFormatter.ofPattern(DateFormat.getBestDateTimePattern(locale, "dMMM"), locale)
            .withZone(ZoneId.systemDefault())
    }

    private companion object {
        /** The web's `useUploadHistory` page size and poll interval. */
        const val HISTORY_PAGE = 5
        const val POLL_INTERVAL_MS = 1_500L
        const val STATE_OFFSET = "offset"
        val DATE_FORMAT: DateTimeFormatter =
            DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT).withZone(ZoneId.systemDefault())
    }
}
