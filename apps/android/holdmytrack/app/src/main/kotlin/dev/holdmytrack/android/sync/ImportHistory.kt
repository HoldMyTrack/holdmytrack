package dev.holdmytrack.android.sync

import android.os.Handler
import android.os.Looper
import android.text.format.DateFormat
import android.view.LayoutInflater
import android.view.View
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.SyncHistory
import dev.holdmytrack.android.net.SyncHistoryEntry
import dev.holdmytrack.android.panel.PanelFormat
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * The sync history — the web's `/sync` page (`apps/android/docs/SPEC.md` FR-4): every ingest job
 * the account has produced, from any path, as `GET /v1/uploads` lists them — a Health Connect
 * session, a GPS recording and a file uploaded on the web are the same kind of job, so they
 * share one history. Each row says how it ended — Ready, Duplicate (its activity set aside for a
 * fuller copy from elsewhere), Failed — or that it's still processing, and when it finished.
 * Re-read every 1.5 seconds while anything is still processing (the web shows those in its
 * Upload menu; this app has no other place for them), and a Ready row has View on map.
 *
 * Two sizes over the same `include_sync_history`: the Sync tab's [preview], the latest
 * [PREVIEW_ROWS] rows with See all ([onSeeAll]), and `SyncHistoryActivity`'s whole of it, twenty
 * a page with the web's pager.
 */
class ImportHistory(
    private val root: View,
    private val preview: Boolean,
    /** A row's View on map: the activity it became, and when it started. */
    private val onViewOnMap: (activityId: String, startedAt: String) -> Unit,
    /** The preview's See all. */
    private val onSeeAll: () -> Unit = {},
) {
    private val context = root.context
    private val res = context.resources

    private val summary: TextView = root.findViewById(R.id.sync_summary)
    private val seeAll: View = root.findViewById(R.id.sync_see_all)
    private val error: TextView = root.findViewById(R.id.sync_error)
    private val empty: View = root.findViewById(R.id.sync_empty)
    private val rows: LinearLayout = root.findViewById(R.id.sync_rows)
    private val pager: View = root.findViewById(R.id.sync_pager)
    private val range: TextView = root.findViewById(R.id.sync_range)
    private val newer: Button = root.findViewById(R.id.sync_newer)
    private val older: Button = root.findViewById(R.id.sync_older)

    private val main = Handler(Looper.getMainLooper())

    /** The page in view. */
    private var offset = 0

    /** Bumped by every read, so a page answering after a newer one was asked for is dropped. */
    private var generation = 0

    private var started = false

    private val poll = Runnable { load() }

    private val pageSize = if (preview) PREVIEW_ROWS else HISTORY_PAGE

    init {
        summary.setText(R.string.status_loading)
        seeAll.setOnClickListener { onSeeAll() }
        newer.setOnClickListener { showPage(maxOf(0, offset - pageSize)) }
        older.setOnClickListener { showPage(offset + pageSize) }
    }

    /** Reads the page in view, and keeps it current while anything is processing. Also how a
     *  finished sync is picked up. */
    fun start() {
        started = true
        load()
    }

    fun stop() {
        started = false
        generation += 1
        main.removeCallbacks(poll)
    }

    private fun showPage(next: Int) {
        offset = next
        load()
    }

    private fun load() {
        main.removeCallbacks(poll)
        val gen = ++generation
        HoldMyTrackApi.syncHistory(pageSize, offset) { result ->
            if (gen != generation) return@syncHistory
            result.onSuccess { render(it) }.onFailure {
                error.text = res.getString(R.string.status_failed, it.message.orEmpty())
                error.visibility = View.VISIBLE
            }
        }
    }

    private fun render(history: SyncHistory) {
        error.visibility = View.GONE
        // "12 activities · 2 in progress".
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
        seeAll.visibility = if (preview && history.total > 0) View.VISIBLE else View.GONE
        if (!preview) renderPager(history)

        // Only while something is in flight: a settled history makes no further requests.
        if (started && history.processing > 0) main.postDelayed(poll, POLL_INTERVAL_MS)
    }

    /** The web's `.sync__pager`: only once the history runs past one page, both buttons in
     *  their places, "1–20 of 57" or "21 of 21" between. */
    private fun renderPager(history: SyncHistory) {
        if (history.total <= history.limit) {
            pager.visibility = View.GONE
            return
        }
        val end = minOf(history.offset.toLong() + history.limit, history.total).toInt()
        pager.visibility = View.VISIBLE
        range.text = if (end == history.offset + 1) {
            res.getString(R.string.status_range_one, end, history.total.toInt())
        } else {
            res.getString(R.string.status_range, history.offset + 1, end, history.total.toInt())
        }
        newer.isEnabled = history.offset > 0
        older.isEnabled = end < history.total
    }

    /**
     * One row, as the web's `/sync` draws it: a file's own name, or for anything synced the
     * source it came from — a synced row's `filename` is a raw external id, never meant to be
     * read — with "9 Sep · 34.7 km" under it once finished, and for a duplicate which copy was
     * kept. At the other end the status, when it finished, and a Ready row's View on map.
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
        val duplicate = entry.status == "done" && entry.keptSource != null
        when {
            duplicate -> {
                status.setText(R.string.status_row_duplicate)
                status.setTextColor(context.getColor(R.color.hmt_accent_strong))
                val kept = res.getString(R.string.status_row_kept, sourceName(entry.keptSource.orEmpty()))
                showDetail(detail, meta(entry)?.let { "$it · $kept" } ?: kept)
            }
            entry.status == "done" -> {
                status.setText(R.string.status_row_ready)
                status.setTextColor(context.getColor(R.color.hmt_success))
                meta(entry)?.let { showDetail(detail, it) }
            }
            entry.status == "failed" -> {
                status.text = if (entry.error.isBlank()) {
                    res.getString(R.string.status_row_failed)
                } else {
                    res.getString(R.string.status_row_failed_with, entry.error)
                }
                status.setTextColor(context.getColor(R.color.hmt_danger))
            }
            else -> status.setText(R.string.status_row_processing)
        }
        finishedLabel(entry.finishedAt)?.let { finished ->
            view.findViewById<TextView>(R.id.row_when).apply {
                text = finished
                visibility = View.VISIBLE
            }
        }
        val activityId = entry.activityId
        val startedAt = entry.startedAt
        // A duplicate's activity is on no map, so it has nowhere to go.
        if (entry.status == "done" && !duplicate && activityId != null && startedAt != null) {
            view.findViewById<TextView>(R.id.row_action).apply {
                visibility = View.VISIBLE
                setOnClickListener { onViewOnMap(activityId, startedAt) }
            }
        }
        rows.addView(view)
    }

    private fun showDetail(detail: TextView, text: String) {
        detail.text = text
        detail.visibility = View.VISIBLE
    }

    /** "9 Sep · 34.7 km" — only when both halves are known. */
    private fun meta(entry: SyncHistoryEntry): String? {
        val startedAt = entry.startedAt ?: return null
        val meters = entry.distanceMeters ?: return null
        val date = runCatching { shortDate().format(Instant.parse(startedAt)) }.getOrDefault(startedAt)
        return date + " · " + PanelFormat.distance(res, meters)
    }

    /** When the import finished, "Oct 6, 14:31" in the phone's zone and clock, or null while
     *  it's still processing. */
    private fun finishedLabel(finishedAt: String?): String? {
        val at = finishedAt?.let { runCatching { Instant.parse(it) }.getOrNull() } ?: return null
        val locale = res.configuration.locales[0] ?: Locale.getDefault()
        val skeleton = if (DateFormat.is24HourFormat(context)) "MMMdHm" else "MMMdhm"
        return DateTimeFormatter.ofPattern(DateFormat.getBestDateTimePattern(locale, skeleton), locale)
            .withZone(ZoneId.systemDefault())
            .format(at)
    }

    /** A row's title for anything that isn't a file — the web's `formatSourceLabel`. */
    private fun sourceTitle(source: String): String = when (source) {
        "healthconnect" -> res.getString(R.string.source_health_connect)
        "healthkit" -> res.getString(R.string.source_health_kit)
        "upload" -> res.getString(R.string.source_title_upload)
        "takeout" -> res.getString(R.string.source_title_takeout)
        "recorded" -> res.getString(R.string.source_title_recorded)
        "timeline" -> res.getString(R.string.source_title_timeline)
        else -> source
    }

    /** The `source` values, said in a sentence (the web's `formatIngestSource`). */
    private fun sourceName(source: String): String = when (source) {
        "healthconnect" -> res.getString(R.string.source_health_connect)
        "healthkit" -> res.getString(R.string.source_health_kit)
        "upload" -> res.getString(R.string.source_upload)
        "takeout" -> res.getString(R.string.source_takeout)
        "recorded" -> res.getString(R.string.source_recorded)
        "timeline" -> res.getString(R.string.source_timeline)
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
        /** The web's `/sync` page size. */
        const val HISTORY_PAGE = 20

        /** The Sync tab's rows: enough to see the latest run landed. */
        const val PREVIEW_ROWS = 3
        const val POLL_INTERVAL_MS = 1_500L
    }
}
