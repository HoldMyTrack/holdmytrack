package dev.holdmytrack.android.panel

import android.content.Intent
import android.content.res.ColorStateList
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.ImageView
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.recyclerview.widget.ConcatAdapter
import androidx.recyclerview.widget.DiffUtil
import androidx.recyclerview.widget.ItemTouchHelper
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.ListAdapter
import androidx.recyclerview.widget.RecyclerView
import com.google.android.material.chip.Chip
import com.google.android.material.chip.ChipGroup
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.snackbar.Snackbar
import dev.holdmytrack.android.R
import dev.holdmytrack.android.SyncHistoryActivity
import dev.holdmytrack.android.UploadActivity
import dev.holdmytrack.android.health.HealthConnect
import dev.holdmytrack.android.net.ActivityOverlap
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.OverlapSpan
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.RecordingActivity
import dev.holdmytrack.android.recording.RecordingTypes
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import dev.holdmytrack.android.sync.Candidate
import dev.holdmytrack.android.sync.HealthConnectCard
import dev.holdmytrack.android.sync.HealthConnectCard.Companion.isReadable
import dev.holdmytrack.android.sync.HiddenCandidates
import dev.holdmytrack.android.sync.SyncCandidates
import dev.holdmytrack.android.sync.SyncReport
import dev.holdmytrack.android.sync.SyncRunner
import dev.holdmytrack.android.ui.LargeText
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * The Activities sheet's Sync tab — the bottom bar's Sync (`docs/SPEC.md` FR-3.6, FR-3.8): every
 * activity on this phone that isn't on the map yet, the user's to choose. Health Connect's
 * sessions with a route from the last three months, and every recording waiting on the phone,
 * newest first ([SyncCandidates]), each drawn dashed on the map while the tab shows
 * ([onCandidates]). **Nothing is ticked**: only what the user ticks is sent ([SyncRunner]), and
 * an unticked row never reaches the server. A row swiped aside is hidden ([HiddenCandidates]) —
 * Show hidden brings it back. Under the list, Health Connect's setup ([HealthConnectCard]), then
 * Upload files and Sync history, each a screen of its own.
 *
 * There's no cursor: each time the tab opens the list is worked out afresh from the phone and the
 * server (`POST /v1/sync/known`), so a reinstall or a second phone agrees, and an activity
 * deleted on the web is offered again.
 *
 * **Foreground only** (`docs/IMPLEMENTATION.md` §4.0): routes written by other apps read back as
 * `ConsentRequired` in the background, so a read or a run in progress is cancelled when the tab
 * leaves the screen ([stop], [pause]); a run cut off is safe, the server being idempotent.
 */
class SyncTab(
    private val content: View,
    private val activity: AppCompatActivity,
    /** The scope reads and runs go in — the map's view's, so they end with it. */
    private val scope: () -> CoroutineScope,
    /** Asks for Health Connect permissions — the map's registered launcher. */
    requestPermissions: (Set<String>) -> Unit,
    /** What the map draws: the listed candidates, [Candidate.key] `highlight` picked out. */
    private val onCandidates: (candidates: List<Candidate>, highlight: String?) -> Unit,
    /** Frame these on the map. */
    private val onFrame: (List<Candidate>) -> Unit,
    /** A run sent something: the map's activities, its days and its tiles are out of date. */
    private val onSynced: () -> Unit,
) {
    private val res = content.resources
    private val store = RecordedActivityStore(activity)

    // Its layout manager first: the head and foot are inflated against it.
    private val list: RecyclerView = content.findViewById<RecyclerView>(R.id.sync_list).apply {
        layoutManager = LinearLayoutManager(activity)
    }
    private val bar: View = content.findViewById(R.id.sync_bar)
    private val progress: View = content.findViewById(R.id.sync_progress)
    private val tickedLine: TextView = content.findViewById(R.id.sync_ticked)
    private val syncNow: Button = content.findViewById(R.id.sync_now)

    private val head: View = LayoutInflater.from(activity).inflate(R.layout.include_sync_head, list, false)
    private val accountNotice: TextView = head.findViewById(R.id.sync_account_notice)
    private val results: TextView = head.findViewById(R.id.sync_results)
    private val problems: TextView = head.findViewById(R.id.sync_problems)
    private val sourceNote: TextView = head.findViewById(R.id.sync_source_note)
    private val filters: View = head.findViewById(R.id.sync_filters)
    private val typeChips: ChipGroup = head.findViewById(R.id.sync_types)
    private val selectAll: Button = head.findViewById(R.id.sync_select_all)

    private val foot: View = LayoutInflater.from(activity).inflate(R.layout.include_sync_foot, list, false)
    private val listNote: TextView = foot.findViewById(R.id.sync_list_note)
    private val showHiddenButton: Button = foot.findViewById(R.id.sync_show_hidden)
    private val upload: View = foot.findViewById(R.id.sync_upload)
    private val card = HealthConnectCard(foot.findViewById(R.id.sync_health_connect_section), requestPermissions)

    private val rows = RowAdapter()

    private var showing = false
    private var loading = false
    private var readiness: HealthConnect.Readiness? = null

    /** Health Connect's sessions as last read, and which the account already has. */
    private var sessions: List<Candidate> = emptyList()
    private var known: Set<String> = emptySet()
    private var recordings: List<Candidate> = emptyList()
    private var hidden: Set<String> = emptySet()
    /** The account's activity each candidate overlaps, by key — the hint, nothing more. */
    private var overlaps: Map<String, ActivityOverlap> = emptyMap()
    private var showHidden = false

    /** What Health Connect couldn't give, said over the list. */
    private var sourceProblem: String? = null

    private val ticked = mutableSetOf<String>()

    /** The types shown; empty is every type. */
    private val types = mutableSetOf<String>()
    private var highlight: String? = null

    /** The camera framed this opening's list already. */
    private var framed = false

    private var loadJob: Job? = null
    private var runJob: Job? = null

    init {
        list.adapter = ConcatAdapter(ViewAdapter(head), rows, ViewAdapter(foot))
        ItemTouchHelper(SwipeToHide()).attachToRecyclerView(list)
        syncNow.setOnClickListener { startSync() }
        selectAll.setOnClickListener { toggleAll() }
        showHiddenButton.setOnClickListener {
            showHidden = !showHidden
            render()
        }
        upload.setOnClickListener { activity.startActivity(Intent(activity, UploadActivity::class.java)) }
        foot.findViewById<View>(R.id.sync_history).setOnClickListener {
            activity.startActivity(Intent(activity, SyncHistoryActivity::class.java))
        }
        LargeText.stack(foot.findViewById(R.id.sync_tiles))
    }

    /** The tab showed: everything read again, nothing ticked. */
    fun start() {
        showing = true
        framed = false
        ticked.clear()
        types.clear()
        highlight = null
        results.visibility = View.GONE
        problems.visibility = View.GONE
        refresh()
    }

    /** The tab went: the map's lines go, and so does anything still reading or sending. */
    fun stop() {
        showing = false
        pause()
        onCandidates(emptyList(), null)
    }

    /** Back on screen — from Health Connect's settings, a recording's Edit, or the app itself:
     *  read again, keeping what's ticked. */
    fun resume() {
        if (!showing) return
        refresh()
    }

    /** Off screen: Health Connect's routes can't be read from the background. */
    fun pause() {
        loadJob?.cancel()
        loadJob = null
        runJob?.cancel()
        runJob = null
        loading = false
    }

    /** Health Connect's permissions came back from their dialog. */
    fun permissionsAnswered() {
        if (showing) refresh()
    }

    /** A line tapped on the map: its row picked out and scrolled to. */
    fun focus(key: String) {
        highlight = key
        render()
        val index = rows.currentList.indexOfFirst { it.key == key }
        if (index >= 0) list.scrollToPosition(index + 1)
    }

    private fun refresh() {
        if (runJob != null) return
        loadJob?.cancel()
        loadJob = scope().launch {
            loading = true
            render()
            val now = HealthConnect.readiness(activity)
            readiness = now
            load(now)
            loading = false
            render()
            if (!framed) {
                val all = shown()
                if (all.isNotEmpty()) {
                    framed = true
                    onFrame(all)
                }
            }
        }
    }

    /** Reads the phone and asks the server what it has. A failure leaves that source out of the
     *  list and says why; the recordings are always listed. */
    private suspend fun load(readiness: HealthConnect.Readiness) {
        recordings = store.all().map { SyncCandidates.fromRecording(it) }
        sourceProblem = null
        var complete = false
        // The demo account can't sync, so Health Connect isn't read for it.
        if (!Session.isDemo && readiness.isReadable()) {
            val client = HealthConnect.clientOrNull(activity)
            if (client != null) {
                try {
                    val read = SyncCandidates.readHealthConnect(client)
                    val ids = read.sessions.map { it.externalId }
                    known = if (ids.isEmpty()) emptySet() else HoldMyTrackApi.syncKnown(HoldMyTrackApi.SOURCE_HEALTH_CONNECT, ids)
                    sessions = read.sessions
                    complete = read.refused == 0
                    if (read.refused > 0) sourceProblem = res.getQuantityString(R.plurals.sync_routes_refused, read.refused, read.refused)
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    sessions = emptyList()
                    sourceProblem = res.getString(R.string.sync_read_failed, e.message.orEmpty())
                }
            }
        } else {
            sessions = emptyList()
        }
        // A hidden row that's gone — a recording synced or deleted elsewhere, a session synced
        // from another phone or aged out of the window — needn't be remembered. A session is only
        // forgotten after a whole read: one missing from a partial read may still be there.
        val hiddenStore = hiddenStore()
        val recordedPrefix = HoldMyTrackApi.SOURCE_RECORDED + ":"
        val keptSessions = if (complete) {
            sessions.filter { it.externalId !in known }.map { it.key }
        } else {
            hiddenStore.all().filterNot { it.startsWith(recordedPrefix) }
        }
        hiddenStore.retainOnly(recordings.map { it.key }.toSet() + keptSessions)
        hidden = hiddenStore.all()
        ticked.retainAll(all().map { it.key }.toSet())
        // A hint only: when the server can't say, the rows go without it.
        overlaps = if (Session.isDemo) {
            emptyMap()
        } else {
            try {
                HoldMyTrackApi.activityOverlaps(all().map { OverlapSpan(it.key, it.startedAt, it.endedAt) })
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                emptyMap()
            }
        }
    }

    private fun hiddenStore() = HiddenCandidates(activity, Session.email)

    /** Everything waiting, hidden or not. */
    private fun all(): List<Candidate> = SyncCandidates.listed(sessions, known, recordings, hidden, showHidden = true)

    /** What the list shows: hidden rows only with Show hidden, and only the chosen types. */
    private fun shown(): List<Candidate> =
        SyncCandidates.listed(sessions, known, recordings, hidden, showHidden)
            .filter { types.isEmpty() || it.activityType in types }

    private fun sendable(): List<Candidate> = shown().filter { it.key in ticked && it.key !in hidden }

    private fun render() {
        if (!showing) return
        val demo = Session.isDemo
        accountNotice.text = if (demo) res.getString(R.string.sync_demo_read_only) else null
        accountNotice.visibility = if (demo) View.VISIBLE else View.GONE
        card.visible = !demo
        readiness?.let { if (!demo) card.render(it) }
        upload.visibility = if (demo) View.GONE else View.VISIBLE

        val everything = all()
        val shown = shown()
        renderTypes(everything)
        sourceNote.text = sourceProblem ?: res.getString(R.string.sync_window_note)
        sourceNote.setTextColor(activity.getColor(if (sourceProblem != null) R.color.hmt_danger else R.color.hmt_ink_meta))
        sourceNote.visibility = if (!demo && readiness?.isReadable() == true) View.VISIBLE else View.GONE

        val tickable = shown.filter { it.key !in hidden }
        filters.visibility = if (everything.isEmpty()) View.GONE else View.VISIBLE
        selectAll.visibility = if (tickable.isEmpty() || demo) View.GONE else View.VISIBLE
        selectAll.setText(if (tickable.isNotEmpty() && tickable.all { it.key in ticked }) R.string.sync_select_none else R.string.sync_select_all)

        rows.submitList(shown.map { row(it) })
        listNote.text = when {
            loading && everything.isEmpty() -> res.getString(R.string.sync_list_loading)
            everything.isEmpty() -> res.getString(R.string.sync_list_empty)
            else -> null
        }
        listNote.visibility = if (listNote.text.isNullOrEmpty()) View.GONE else View.VISIBLE
        val hiddenCount = everything.count { it.key in hidden }
        showHiddenButton.visibility = if (hiddenCount > 0 || showHidden) View.VISIBLE else View.GONE
        showHiddenButton.text = if (showHidden) res.getString(R.string.sync_hide_hidden) else res.getString(R.string.sync_show_hidden, hiddenCount)

        val sending = sendable()
        val running = runJob != null
        bar.visibility = if (demo || (everything.isEmpty() && !loading)) View.GONE else View.VISIBLE
        progress.visibility = if (running || loading) View.VISIBLE else View.GONE
        tickedLine.text = res.getString(
            R.string.sync_ticked,
            sending.size,
            tickable.size,
            PanelFormat.totalDistance(res, sending.sumOf { it.distanceM }),
        )
        syncNow.isEnabled = !demo && !running && sending.isNotEmpty()
        syncNow.text = if (sending.isEmpty()) res.getString(R.string.sync_now) else res.getString(R.string.sync_send, sending.size)

        onCandidates(shown, highlight)
    }

    /** One chip per type listed, when there's more than one to choose between. */
    private fun renderTypes(everything: List<Candidate>) {
        val listedTypes = everything.map { it.activityType }.distinct().sortedBy { RecordingTypes.format(res, it) }
        types.retainAll(listedTypes.toSet())
        typeChips.visibility = if (listedTypes.size > 1) View.VISIBLE else View.GONE
        val current = (0 until typeChips.childCount).map { typeChips.getChildAt(it).tag as String }
        if (current != listedTypes) {
            typeChips.removeAllViews()
            for (type in listedTypes) {
                typeChips.addView(Chip(activity).apply {
                    tag = type
                    text = RecordingTypes.format(res, type)
                    isCheckable = true
                    setOnCheckedChangeListener { _, checked ->
                        if (checked) types += type else types -= type
                        render()
                    }
                })
            }
        }
        for (i in 0 until typeChips.childCount) {
            val chip = typeChips.getChildAt(i) as Chip
            val checked = chip.tag in types
            if (chip.isChecked != checked) chip.isChecked = checked
        }
    }

    /** Ticks every row shown, or, when they all are, unticks them. */
    private fun toggleAll() {
        val tickable = shown().filter { it.key !in hidden }.map { it.key }
        if (tickable.all { it in ticked }) ticked -= tickable.toSet() else ticked += tickable
        render()
    }

    private fun toggle(key: String) {
        if (key in hidden) return
        if (!ticked.remove(key)) ticked += key
        render()
    }

    private fun pick(candidate: Candidate) {
        highlight = candidate.key
        render()
        onFrame(listOf(candidate))
    }

    /** Swiped aside: hidden, with Undo; a hidden one swiped is back on the list. */
    private fun swiped(key: String) {
        val store = hiddenStore()
        val wasHidden = key in hidden
        if (wasHidden) store.unhide(key) else store.hide(key)
        ticked -= key
        hidden = store.all()
        render()
        Snackbar.make(list, if (wasHidden) R.string.sync_unhidden else R.string.sync_hidden, Snackbar.LENGTH_LONG)
            .apply { if (bar.visibility == View.VISIBLE) anchorView = bar }
            .setAction(R.string.sync_undo) {
                val again = hiddenStore()
                if (wasHidden) again.hide(key) else again.unhide(key)
                hidden = again.all()
                render()
            }
            .show()
    }

    private fun confirmDelete(candidate: Candidate) {
        MaterialAlertDialogBuilder(activity, R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(R.string.recorded_delete_confirm_title)
            .setMessage(R.string.recorded_delete_confirm_message)
            .setPositiveButton(R.string.recording_delete) { _, _ ->
                scope().launch {
                    store.delete(candidate.externalId)
                    hiddenStore().unhide(candidate.key)
                    hidden = hiddenStore().all()
                    recordings = recordings.filterNot { it.key == candidate.key }
                    ticked -= candidate.key
                    render()
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    /** Sends what's ticked (and shown) — nothing else. What lands leaves the list. */
    private fun startSync() {
        val sending = sendable()
        if (sending.isEmpty() || runJob != null) return
        loadJob?.cancel()
        loadJob = null
        loading = false
        results.visibility = View.GONE
        problems.visibility = View.GONE
        runJob = scope().launch {
            try {
                val report = SyncRunner(store, res).send(sending) { sent ->
                    tickedLine.text = res.getString(R.string.sync_progress, sent, sending.size)
                }
                landed(report)
                show(report)
                if (report.landed.isNotEmpty()) onSynced()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                showProblems(listOf(res.getString(R.string.sync_failed, e.message.orEmpty())))
            } finally {
                runJob = null
                render()
            }
        }
        render()
    }

    /** What the server has now leaves the list: a session is known, a recording is gone. */
    private fun landed(report: SyncReport) {
        val landedSessions = sessions.filter { it.key in report.landed }.map { it.externalId }
        known = known + landedSessions
        recordings = recordings.filterNot { it.key in report.landed }
        ticked -= report.landed
        if (highlight in report.landed) highlight = null
    }

    /**
     * What the run did, and what it couldn't: the ones the server refused, each with the server's
     * own reason — they stay ticked on the list — and why it stopped early, if it did.
     */
    private fun show(report: SyncReport) {
        val sent = report.synced + report.alreadyPresent
        if (sent > 0) {
            results.text = if (report.alreadyPresent > 0) {
                res.getString(R.string.sync_summary_with_present, report.synced, report.alreadyPresent)
            } else {
                res.getQuantityString(R.plurals.sync_summary, report.synced, report.synced)
            }
            results.visibility = View.VISIBLE
        }
        val trouble = report.rejected.map {
            res.getString(R.string.sync_rejected_row, DATE_FORMAT.withZone(ZoneId.systemDefault()).format(it.startedAt), RecordingTypes.format(res, it.activityType), it.reason)
        }.toMutableList()
        report.stoppedBecause?.let { trouble += res.getString(R.string.sync_stopped, it) }
        showProblems(trouble)
    }

    private fun showProblems(lines: List<String>) {
        problems.text = lines.joinToString("\n")
        problems.visibility = if (lines.isEmpty()) View.GONE else View.VISIBLE
    }

    private fun row(candidate: Candidate): Row {
        val date = DATE_FORMAT.withZone(ZoneId.systemDefault()).format(candidate.startedAt)
        val named = candidate.name.isNotBlank()
        val meta = listOfNotNull(
            date.takeIf { named },
            PanelFormat.distance(res, candidate.distanceM),
            RecordingTypes.format(res, candidate.activityType),
            res.getString(if (candidate.isRecording) R.string.sync_origin_recorded else R.string.sync_origin_health_connect),
        ).joinToString(" · ")
        val overlap = overlaps[candidate.key]?.let { o ->
            res.getString(R.string.sync_row_overlaps, o.name.trim().ifEmpty { PanelFormat.startedAt(res, o.startedAt, o.timezone) })
        }
        return Row(
            key = candidate.key,
            title = if (named) candidate.name else date,
            meta = meta,
            overlap = overlap,
            type = candidate.activityType,
            recording = candidate.isRecording,
            ticked = candidate.key in ticked,
            hidden = candidate.key in hidden,
            highlighted = candidate.key == highlight,
        )
    }

    private fun candidate(key: String): Candidate? = all().firstOrNull { it.key == key }

    /** What one row shows — compared, unlike a [Candidate], whose line is an array. */
    private data class Row(
        val key: String,
        val title: String,
        val meta: String,
        /** "Overlaps Morning walk", when it does (`docs/SPEC.md` FR-3.7). */
        val overlap: String?,
        val type: String,
        val recording: Boolean,
        val ticked: Boolean,
        val hidden: Boolean,
        val highlighted: Boolean,
    )

    private inner class RowAdapter : ListAdapter<Row, RowHolder>(
        object : DiffUtil.ItemCallback<Row>() {
            override fun areItemsTheSame(old: Row, new: Row) = old.key == new.key
            override fun areContentsTheSame(old: Row, new: Row) = old == new
        },
    ) {
        override fun getItemViewType(position: Int) = TYPE_ROW

        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int) =
            RowHolder(LayoutInflater.from(parent.context).inflate(R.layout.item_sync_candidate, parent, false))

        override fun onBindViewHolder(holder: RowHolder, position: Int) = holder.bind(getItem(position))
    }

    private inner class RowHolder(view: View) : RecyclerView.ViewHolder(view) {
        private val tick: View = view.findViewById(R.id.candidate_tick)
        private val icon: ImageView = view.findViewById(R.id.candidate_icon)
        private val text: View = view.findViewById(R.id.candidate_text)
        private val title: TextView = view.findViewById(R.id.candidate_title)
        private val meta: TextView = view.findViewById(R.id.candidate_meta)
        private val overlap: TextView = view.findViewById(R.id.candidate_overlap)
        private val hiddenBadge: View = view.findViewById(R.id.candidate_hidden)
        private val edit: View = view.findViewById(R.id.candidate_edit)
        private val delete: View = view.findViewById(R.id.candidate_delete)

        var key: String? = null
            private set

        fun bind(row: Row) {
            key = row.key
            itemView.isSelected = row.highlighted
            title.text = row.title
            meta.text = row.meta
            overlap.text = row.overlap
            overlap.visibility = if (row.overlap != null) View.VISIBLE else View.GONE
            val alpha = if (row.hidden) HIDDEN_ALPHA else 1f
            title.alpha = alpha
            meta.alpha = alpha
            overlap.alpha = alpha
            hiddenBadge.visibility = if (row.hidden) View.VISIBLE else View.GONE

            val context = itemView.context
            icon.setImageResource(if (row.ticked) R.drawable.ic_check else kindIcon(ActivityKind.of(row.type)))
            icon.backgroundTintList = if (row.ticked) ColorStateList.valueOf(context.getColor(R.color.hmt_accent)) else null
            icon.imageTintList = ColorStateList.valueOf(context.getColor(if (row.ticked) R.color.hmt_on_accent else R.color.hmt_ink_secondary))
            val tickable = !row.hidden && !Session.isDemo
            tick.isEnabled = tickable
            tick.isClickable = tickable
            tick.contentDescription = res.getString(if (row.ticked) R.string.sync_row_untick else R.string.sync_row_tick, row.title)
            tick.setOnClickListener { toggle(row.key) }

            text.contentDescription = listOfNotNull(res.getString(R.string.sync_row_show, row.title), row.meta, row.overlap).joinToString(". ")
            text.setOnClickListener { candidate(row.key)?.let(::pick) }
            text.setOnLongClickListener {
                if (tickable) toggle(row.key)
                tickable
            }

            edit.visibility = if (row.recording) View.VISIBLE else View.GONE
            delete.visibility = if (row.recording) View.VISIBLE else View.GONE
            if (row.recording) {
                // Named per row: TalkBack reads each control on its own, and "Delete" alone
                // doesn't say which recording goes.
                edit.contentDescription = res.getString(R.string.recorded_row_edit_named, row.title)
                delete.contentDescription = res.getString(R.string.recorded_row_delete_named, row.title)
                val id = row.key.substringAfter(':')
                edit.setOnClickListener {
                    activity.startActivity(
                        Intent(activity, RecordingActivity::class.java).putExtra(RecordingActivity.EXTRA_RECORDING_ID, id),
                    )
                }
                // Every recording here is unsynced, so Delete discards the only copy — the
                // confirmation says so.
                delete.setOnClickListener { candidate(row.key)?.let(::confirmDelete) }
            }
        }
    }

    /** A row swiped either way is hidden — or, a hidden one, back. The head and foot stay. */
    private inner class SwipeToHide : ItemTouchHelper.SimpleCallback(0, ItemTouchHelper.START or ItemTouchHelper.END) {
        override fun getSwipeDirs(recyclerView: RecyclerView, holder: RecyclerView.ViewHolder): Int =
            if (holder is RowHolder && runJob == null) super.getSwipeDirs(recyclerView, holder) else 0

        override fun onMove(recyclerView: RecyclerView, holder: RecyclerView.ViewHolder, target: RecyclerView.ViewHolder) = false

        override fun onSwiped(holder: RecyclerView.ViewHolder, direction: Int) {
            val key = (holder as RowHolder).key ?: return
            swiped(key)
        }
    }

    /** One fixed view as a list's item — the head and the foot. */
    private class ViewAdapter(private val view: View) : RecyclerView.Adapter<RecyclerView.ViewHolder>() {
        override fun getItemCount() = 1
        override fun getItemViewType(position: Int) = System.identityHashCode(view)
        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int) = object : RecyclerView.ViewHolder(view) {}
        override fun onBindViewHolder(holder: RecyclerView.ViewHolder, position: Int) = Unit
    }

    private companion object {
        const val TYPE_ROW = 1
        const val HIDDEN_ALPHA = 0.55f
        val DATE_FORMAT: DateTimeFormatter = DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT)
    }
}
