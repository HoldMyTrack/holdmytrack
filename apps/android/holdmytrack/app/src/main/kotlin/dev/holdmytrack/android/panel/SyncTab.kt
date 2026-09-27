package dev.holdmytrack.android.panel

import android.os.Handler
import android.os.Looper
import android.text.format.DateFormat
import android.view.LayoutInflater
import android.view.View
import android.widget.LinearLayout
import android.widget.TextView
import com.google.android.material.button.MaterialButton
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.SyncHistory
import dev.holdmytrack.android.net.SyncHistoryEntry
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * The Activities panel's Sync tab — the history half of the web's `apps/web/src/ui/SyncTab.tsx`
 * (`apps/android/docs/SPEC.md` FR-4.1): every ingest job the account has produced, from any
 * path, as `GET /v1/uploads` lists them — a Health Connect session, a GPS recording and a file
 * uploaded on the web are the same kind of job, so they share one history. Five a page with a
 * pager, re-read every 1.5 seconds while anything is still processing (whichever tab is
 * showing, since the tab's badge counts those), and a finished row that became an activity has
 * View on map.
 *
 * The web tab's upload drop zone isn't here: this app sends activities through Sync Source.
 */
class SyncTab(
    private val root: View,
    /** How many jobs are still processing — the tab's badge. */
    private val onProcessing: (Int) -> Unit,
    /** A row's View on map: the activity it became, and when it started. */
    private val onViewOnMap: (activityId: String, startedAt: String) -> Unit,
) {
    private val context = root.context
    private val res = context.resources

    private val summary: TextView
    private val error: TextView = root.findViewById(R.id.sync_error)
    private val empty: View = root.findViewById(R.id.sync_empty)
    private val rows: LinearLayout = root.findViewById(R.id.sync_rows)
    private val pager: View = root.findViewById(R.id.sync_pager)
    private val range: TextView = root.findViewById(R.id.sync_range)
    private val previous: MaterialButton = root.findViewById(R.id.sync_previous)
    private val next: MaterialButton = root.findViewById(R.id.sync_next)

    private val main = Handler(Looper.getMainLooper())

    /** The page in view, the web's `useUploadHistory` offset. */
    private var offset = 0

    /** Bumped by every read, so a page answering after a newer one was asked for is dropped. */
    private var generation = 0

    private var started = false

    private val poll = Runnable { load() }

    init {
        val head: View = root.findViewById(R.id.sync_head)
        head.findViewById<TextView>(R.id.list_title).setText(R.string.status_history)
        summary = head.findViewById(R.id.list_summary)
        summary.setText(R.string.status_loading)
        previous.setOnClickListener { showPage(maxOf(0, offset - HISTORY_PAGE)) }
        next.setOnClickListener { showPage(offset + HISTORY_PAGE) }
    }

    /** Reads the page in view, and keeps it current while anything is processing. */
    fun start() {
        started = true
        load()
    }

    fun stop() {
        started = false
        generation += 1
        main.removeCallbacks(poll)
    }

    /** Something may have been imported since — a sync, a recording — so read it again. */
    fun refresh() {
        if (started) load()
    }

    private fun showPage(next: Int) {
        offset = next
        load()
    }

    private fun load() {
        main.removeCallbacks(poll)
        val gen = ++generation
        HoldMyTrackApi.syncHistory(HISTORY_PAGE, offset) { result ->
            if (gen != generation) return@syncHistory
            result.onSuccess { render(it) }.onFailure {
                error.text = res.getString(R.string.status_failed, it.message.orEmpty())
                error.visibility = View.VISIBLE
            }
        }
    }

    private fun render(history: SyncHistory) {
        error.visibility = View.GONE
        // "12 activities · 2 in progress" — the web's list summary.
        val total = history.total.toInt()
        val count = res.getQuantityString(R.plurals.status_count, total, total)
        summary.text = if (history.processing > 0) {
            count + " · " + res.getString(R.string.status_in_progress, history.processing.toInt())
        } else {
            count
        }
        empty.visibility = if (history.entries.isEmpty()) View.VISIBLE else View.GONE
        rows.visibility = if (history.entries.isEmpty()) View.GONE else View.VISIBLE
        rows.removeAllViews()
        history.entries.forEach(::addRow)
        renderPager(history)
        onProcessing(history.processing.toInt())

        // Only while something is in flight: a settled history makes no further requests.
        if (started && history.processing > 0) main.postDelayed(poll, POLL_INTERVAL_MS)
    }

    /** The web's `.sync-tab__pager`: only once the history runs past one page. */
    private fun renderPager(history: SyncHistory) {
        if (history.total <= history.limit) {
            pager.visibility = View.GONE
            return
        }
        val end = minOf(history.offset.toLong() + history.limit, history.total)
        pager.visibility = View.VISIBLE
        range.text = res.getString(R.string.status_range, history.offset + 1, end.toInt(), history.total.toInt())
        previous.isEnabled = history.offset > 0
        next.isEnabled = end < history.total
    }

    /**
     * One row, as the web's SyncTab draws it: a file's own name, or for anything synced the
     * source it came from — a synced row's `filename` is a raw external id, never meant to be
     * read. At the other end the status, then for a finished row "9 Sep · 34.7 km" and View on
     * map.
     */
    private fun addRow(entry: SyncHistoryEntry) {
        val title = when (entry.source) {
            "upload", "takeout" -> entry.filename
            else -> sourceTitle(entry.source)
        }
        val view = LayoutInflater.from(context).inflate(R.layout.item_sync_history_row, rows, false)
        view.findViewById<TextView>(R.id.row_name).text = title
        val status = view.findViewById<TextView>(R.id.row_status)
        val detail = view.findViewById<TextView>(R.id.row_detail)
        when (entry.status) {
            "done" -> {
                status.setText(R.string.status_row_ready)
                status.setTextColor(context.getColor(R.color.hmt_success))
                meta(entry)?.let {
                    detail.text = it
                    detail.visibility = View.VISIBLE
                }
            }
            "failed" -> {
                status.text = if (entry.error.isBlank()) {
                    res.getString(R.string.status_row_failed)
                } else {
                    res.getString(R.string.status_row_failed_with, entry.error)
                }
                status.setTextColor(context.getColor(R.color.hmt_danger))
            }
            else -> status.setText(R.string.status_row_processing)
        }
        val activityId = entry.activityId
        val startedAt = entry.startedAt
        if (entry.status == "done" && activityId != null && startedAt != null) {
            view.findViewById<TextView>(R.id.row_action).apply {
                visibility = View.VISIBLE
                setOnClickListener { onViewOnMap(activityId, startedAt) }
            }
        }
        rows.addView(view)
    }

    /** "9 Sep · 34.7 km", the web's `.sync-tab__row-meta` — only when both halves are known. */
    private fun meta(entry: SyncHistoryEntry): String? {
        val startedAt = entry.startedAt ?: return null
        val meters = entry.distanceMeters ?: return null
        val date = runCatching { shortDate().format(Instant.parse(startedAt)) }.getOrDefault(startedAt)
        return date + " · " + PanelFormat.distance(res, meters)
    }

    /** A row's title for anything that isn't a file — the web's `formatSourceLabel`. */
    private fun sourceTitle(source: String): String = when (source) {
        "healthconnect" -> res.getString(R.string.source_health_connect)
        "healthkit" -> res.getString(R.string.source_health_kit)
        "upload" -> res.getString(R.string.source_title_upload)
        "takeout" -> res.getString(R.string.source_title_takeout)
        "recorded" -> res.getString(R.string.source_title_recorded)
        else -> source
    }

    /** "Sep 9" / "9 сент." — the web's `formatShortDate`, in the app's language. Built per
     *  call, since the per-app language can change while the app runs. */
    private fun shortDate(): DateTimeFormatter {
        val locale = res.configuration.locales[0] ?: Locale.getDefault()
        return DateTimeFormatter.ofPattern(DateFormat.getBestDateTimePattern(locale, "dMMM"), locale)
            .withZone(ZoneId.systemDefault())
    }

    private companion object {
        /** The web's `useUploadHistory` page size and poll interval. */
        const val HISTORY_PAGE = 5
        const val POLL_INTERVAL_MS = 1_500L
    }
}
