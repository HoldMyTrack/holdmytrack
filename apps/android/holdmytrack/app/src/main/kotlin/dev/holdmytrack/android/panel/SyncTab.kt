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
import dev.holdmytrack.android.MainActivity
import dev.holdmytrack.android.R
import dev.holdmytrack.android.SyncActivity
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
import dev.holdmytrack.android.sync.HealthConnectCard.Companion.isReadable
import dev.holdmytrack.android.sync.HiddenCandidates
import dev.holdmytrack.android.sync.Similar
import dev.holdmytrack.android.sync.SimilarTracksDialog
import dev.holdmytrack.android.sync.SyncCandidates
import dev.holdmytrack.android.sync.SyncProgress
import dev.holdmytrack.android.sync.SyncReport
import dev.holdmytrack.android.sync.SyncRunner
import dev.holdmytrack.android.sync.SyncSources
import dev.holdmytrack.android.sync.WaitingLoader
import dev.holdmytrack.android.sync.WaitingStore
import dev.holdmytrack.android.ui.LargeText
import java.time.Instant
import java.time.OffsetDateTime
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
 * an unticked row never reaches the server. A row that covers the same time as another, or as an
 * activity the account has, carries an ⓘ ([SimilarTracksDialog]) — a hint, never a choice made
 * for the user. A row swiped aside is hidden ([HiddenCandidates]) —
 * Show hidden brings it back. Under the list, Upload files and Sync history, each a screen of its
 * own, and the way to Settings › Sync (`SyncActivity`), where Health Connect is switched on and
 * set up: it isn't read here until it is ([SyncSources]).
 *
 * Health Connect is read only when the user taps **Load** ([WaitingLoader]) — or once by
 * itself after it's switched on — and its sessions are kept as last read ([WaitingStore]), so
 * opening the tab lists them again, with when they were read, without reading anything.
 * Recordings are on the phone already, and listed as they are each time.
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
    private val loadColumn: View = content.findViewById(R.id.sync_load_column)
    private val checkedLine: TextView = content.findViewById(R.id.sync_checked)
    private val loadUpdates: Button = content.findViewById(R.id.sync_load)
    private val waiting = WaitingStore(activity)

    private val head: View = LayoutInflater.from(activity).inflate(R.layout.include_sync_head, list, false)
    private val accountNotice: TextView = head.findViewById(R.id.sync_account_notice)
    private val results: TextView = head.findViewById(R.id.sync_results)
    private val problems: TextView = head.findViewById(R.id.sync_problems)
    private val sourceNote: TextView = head.findViewById(R.id.sync_source_note)
    private val sourceSettings: Button = head.findViewById(R.id.sync_source_settings)
    private val filters: View = head.findViewById(R.id.sync_filters)
    private val typeChips: ChipGroup = head.findViewById(R.id.sync_types)
    private val selectAll: Button = head.findViewById(R.id.sync_select_all)

    private val foot: View = LayoutInflater.from(activity).inflate(R.layout.include_sync_foot, list, false)
    private val listNote: TextView = foot.findViewById(R.id.sync_list_note)
    private val showHiddenButton: Button = foot.findViewById(R.id.sync_show_hidden)
    private val upload: View = foot.findViewById(R.id.sync_upload)

    private val rows = RowAdapter()

    private var showing = false
    private var loading = false
    private var readiness: HealthConnect.Readiness? = null

    /** Health Connect's sessions as last read — this time, or kept from before ([WaitingStore]),
     *  when they have no route with them — and which of them the account has since been sent. */
    private var sessions: List<Candidate> = emptyList()
    private var known: Set<String> = emptySet()
    /** When Health Connect was last read; null before the first Load. */
    private var checkedAt: Instant? = null
    /** Sessions sent whose ingest failed ([showFailed]): back in the kept list ([WaitingStore])
     *  before it's next read, since they left it when the server took them ([landed]). */
    private val keepAgain = mutableListOf<Candidate>()
    private var recordings: List<Candidate> = emptyList()
    private var hidden: Set<String> = emptySet()
    /** The account's activity each candidate overlaps, by key — the hint, nothing more. */
    private var overlaps: Map<String, ActivityOverlap> = emptyMap()
    /** The other waiting rows each one overlaps, by key ([Similar.waiting]), as last rendered. */
    private var similar: Map<String, List<Candidate>> = emptyMap()
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

    /** A row on its way landed — it leaves the list — or failed: it's back, and says so. */
    private val progressListener = SyncProgress.Listener(onChange = ::progressed, onLanded = {})

    init {
        list.adapter = ConcatAdapter(ViewAdapter(head), rows, ViewAdapter(foot))
        ItemTouchHelper(SwipeToHide()).attachToRecyclerView(list)
        syncNow.setOnClickListener { startSync() }
        loadUpdates.setOnClickListener { loadUpdates() }
        selectAll.setOnClickListener { toggleAll() }
        showHiddenButton.setOnClickListener {
            showHidden = !showHidden
            render()
        }
        upload.setOnClickListener { activity.startActivity(Intent(activity, UploadActivity::class.java)) }
        foot.findViewById<View>(R.id.sync_history).setOnClickListener {
            activity.startActivity(Intent(activity, SyncHistoryActivity::class.java))
        }
        for (link in listOf(foot.findViewById<View>(R.id.sync_sources), sourceSettings)) {
            link.setOnClickListener { activity.startActivity(Intent(activity, SyncActivity::class.java)) }
        }
        LargeText.stack(foot.findViewById(R.id.sync_tiles))
        LargeText.stack(content.findViewById(R.id.sync_buttons))
    }

    /** The tab showed: what's kept listed again, nothing ticked. */
    fun start() {
        showing = true
        framed = false
        ticked.clear()
        types.clear()
        highlight = null
        results.visibility = View.GONE
        problems.visibility = View.GONE
        SyncProgress.addListener(progressListener)
        showFailed()
        restore()
    }

    /** The tab went: the map's lines go, and so does anything still reading or sending. */
    fun stop() {
        showing = false
        SyncProgress.removeListener(progressListener)
        pause()
        onCandidates(emptyList(), null)
    }

    /** Back on screen — from Settings › Sync, a recording's Edit, or the app itself: what's kept
     *  listed again, keeping what's ticked. Health Connect isn't read. */
    fun resume() {
        if (!showing) return
        restore()
    }

    /** Off screen: Health Connect's routes can't be read from the background. */
    fun pause() {
        loadJob?.cancel()
        loadJob = null
        runJob?.cancel()
        runJob = null
        loading = false
    }

    /** A line tapped on the map: its row picked out and scrolled to. */
    fun focus(key: String) {
        highlight = key
        render()
        val index = rows.currentList.indexOfFirst { it.key == key }
        if (index >= 0) list.scrollToPosition(index + 1)
    }

    /**
     * Lists what's kept: the recordings on the phone and Health Connect's sessions as Load
     * last read them, and asks the server which activities they overlap. Then, if Health Connect
     * was just switched on and hasn't been read yet, it reads it — the one load the user didn't
     * tap for ([SyncSources.firstLoadPending]).
     */
    private fun restore() {
        if (runJob != null || loading) return
        loadJob?.cancel()
        loadJob = scope().launch {
            if (keepAgain.isNotEmpty()) {
                waiting.put(keepAgain.toList())
                keepAgain.clear()
            }
            readiness = HealthConnect.readiness(activity)
            recordings = store.all().map { SyncCandidates.fromRecording(it) }
            val sources = SyncSources(activity, Session.email)
            if (!Session.isDemo && sources.healthConnect) {
                val saved = waiting.read()
                // Read this time, a session still has its route: kept rather than its copy.
                val read = sessions.associateBy { it.key }
                sessions = saved.sessions.map { read[it.key] ?: it }
                checkedAt = saved.checkedAt
                sourceProblem = saved.refused.takeIf { it > 0 }?.let { res.getQuantityString(R.plurals.sync_routes_refused, it, it) }
            } else {
                sessions = emptyList()
                checkedAt = null
                sourceProblem = null
            }
            // A hidden recording that's gone — synced, or deleted — needn't be remembered. Hidden
            // sessions are pruned by a whole Load ([WaitingLoader]).
            val hiddenStore = hiddenStore()
            val recordedPrefix = HoldMyTrackApi.SOURCE_RECORDED + ":"
            hiddenStore.retainOnly(recordings.map { it.key }.toSet() + hiddenStore.all().filterNot { it.startsWith(recordedPrefix) })
            hidden = hiddenStore.all()
            ticked.retainAll(all().map { it.key }.toSet())
            overlaps = askOverlaps()
            render()
            frameOnce()
            loadJob = null
            if (sources.healthConnect && sources.firstLoadPending && readiness?.isReadable() == true) loadUpdates()
        }
    }

    /** Load: Health Connect read now, and kept until the next one. A failure keeps what
     *  was listed, and says why over the list. */
    private fun loadUpdates() {
        if (runJob != null || loading) return
        loadJob?.cancel()
        loadJob = scope().launch {
            loading = true
            render()
            try {
                when (val result = WaitingLoader(activity).load()) {
                    is WaitingLoader.Result.Loaded -> {
                        sessions = result.sessions
                        known = emptySet()
                        checkedAt = result.checkedAt
                        sourceProblem = result.refused.takeIf { it > 0 }?.let { res.getQuantityString(R.plurals.sync_routes_refused, it, it) }
                        hidden = hiddenStore().all()
                        ticked.retainAll(all().map { it.key }.toSet())
                        overlaps = askOverlaps()
                        framed = false
                    }
                    is WaitingLoader.Result.Failed -> sourceProblem = result.reason
                    WaitingLoader.Result.Unreadable -> readiness = HealthConnect.readiness(activity)
                }
            } finally {
                loading = false
                loadJob = null
            }
            render()
            frameOnce()
        }
        render()
    }

    /** The first time the tab has something to show, the camera frames all of it. */
    private fun frameOnce() {
        if (framed) return
        val all = drawn()
        if (all.isNotEmpty()) {
            framed = true
            onFrame(all)
        }
    }

    /** The account's activity each listed row overlaps (`POST /v1/activities/overlaps`). A hint
     *  only: when the server can't say, the rows go without it. */
    private suspend fun askOverlaps(): Map<String, ActivityOverlap> {
        if (Session.isDemo) return emptyMap()
        return try {
            // What's on its way would only overlap itself once it lands.
            val listed = all().filterNot { SyncProgress.isProcessing(it.key) }
            HoldMyTrackApi.activityOverlaps(listed.map { OverlapSpan(it.key, it.startedAt, it.endedAt) })
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            emptyMap()
        }
    }

    private fun hiddenStore() = HiddenCandidates(activity, Session.email)

    private fun healthConnectOn() = SyncSources(activity, Session.email).healthConnect

    /** Everything waiting, hidden or not, and what's on its way to the map ([SyncProgress]). */
    private fun all(): List<Candidate> = withInFlight(SyncCandidates.listed(sessions, known, recordings, hidden, showHidden = true))

    /** What the list shows: hidden rows only with Show hidden, and only the chosen types. */
    private fun shown(): List<Candidate> =
        withInFlight(SyncCandidates.listed(sessions, known, recordings, hidden, showHidden))
            .filter { types.isEmpty() || it.activityType in types }

    /** Sent and not landed yet: the server knows a session, a recording is off the phone, so
     *  neither is in [SyncCandidates.listed] — they're listed from [SyncProgress] instead. */
    private fun withInFlight(listed: List<Candidate>): List<Candidate> =
        (listed + SyncProgress.candidates).distinctBy { it.key }.sortedByDescending { it.startedAt }

    private fun processing(candidate: Candidate) = SyncProgress.isProcessing(candidate.key)

    /** What the map draws dashed: what's on its way will be drawn as itself once it lands. */
    private fun drawn(): List<Candidate> = shown().filterNot(::processing)

    private fun sendable(): List<Candidate> = shown().filter { it.key in ticked && it.key !in hidden && !processing(it) }

    private fun render() {
        if (!showing) return
        val demo = Session.isDemo
        accountNotice.text = if (demo) res.getString(R.string.sync_demo_read_only) else null
        accountNotice.visibility = if (demo) View.VISIBLE else View.GONE
        upload.visibility = if (demo) View.GONE else View.VISIBLE

        val everything = all()
        val shown = shown()
        renderTypes(everything)
        // Health Connect off, or on and not set up yet, is said here and set right in Settings ›
        // Sync; read, it's the window the list reaches back over, or what went wrong.
        val readable = readiness?.isReadable() == true
        val setUp = when {
            demo || readiness == null -> null
            !healthConnectOn() -> res.getString(R.string.sync_hc_is_off)
            !readable && readiness != HealthConnect.Readiness.UNAVAILABLE -> res.getString(R.string.sync_hc_needs_setup)
            else -> null
        }
        sourceNote.text = setUp ?: sourceProblem ?: res.getString(R.string.sync_window_note)
        sourceNote.setTextColor(activity.getColor(if (setUp == null && sourceProblem != null) R.color.hmt_danger else R.color.hmt_ink_meta))
        sourceNote.visibility = if (setUp != null || (!demo && readable)) View.VISIBLE else View.GONE
        sourceSettings.visibility = if (setUp != null) View.VISIBLE else View.GONE

        // Load, over when it last ran — or, greyed out, why there's nothing to load from: no
        // source switched on, or Health Connect on and not set up yet.
        val healthConnect = !demo && healthConnectOn()
        val loadable = healthConnect && readable
        loadColumn.visibility = if (demo) View.GONE else View.VISIBLE
        checkedLine.text = when {
            !healthConnect -> res.getString(R.string.sync_no_source)
            !readable -> res.getString(R.string.sync_source_not_set_up)
            loading -> res.getString(R.string.sync_loading_health_connect)
            else -> checkedAt?.let { res.getString(R.string.sync_last_load, PanelFormat.phoneDateTime(res, it)) }
                ?: res.getString(R.string.sync_not_loaded_yet)
        }
        loadUpdates.isEnabled = loadable && !loading && runJob == null

        val tickable = shown.filter { it.key !in hidden && !processing(it) }
        filters.visibility = if (everything.isEmpty()) View.GONE else View.VISIBLE
        selectAll.visibility = if (tickable.isEmpty() || demo) View.GONE else View.VISIBLE
        selectAll.setText(if (tickable.isNotEmpty() && tickable.all { it.key in ticked }) R.string.sync_select_none else R.string.sync_select_all)

        // Rows that can still be sent: what's on its way or hidden isn't offered, so it isn't
        // anyone's double.
        similar = Similar.waiting(everything.filter { it.key !in hidden && !processing(it) })
        rows.submitList(shown.map { row(it) })
        listNote.text = when {
            loading && everything.isEmpty() -> res.getString(R.string.sync_list_loading)
            everything.isEmpty() && healthConnect && checkedAt == null -> res.getString(R.string.sync_list_not_loaded)
            everything.isEmpty() -> res.getString(R.string.sync_list_empty)
            else -> null
        }
        listNote.visibility = if (listNote.text.isNullOrEmpty()) View.GONE else View.VISIBLE
        val hiddenCount = everything.count { it.key in hidden }
        showHiddenButton.visibility = if (hiddenCount > 0 || showHidden) View.VISIBLE else View.GONE
        showHiddenButton.text = if (showHidden) res.getString(R.string.sync_hide_hidden) else res.getString(R.string.sync_show_hidden, hiddenCount)

        val sending = sendable()
        val running = runJob != null
        // Load is there even with nothing listed — greyed out, saying why, with no source set up —
        // and Sync beside it, disabled.
        bar.visibility = if (demo) View.GONE else View.VISIBLE
        progress.visibility = if (running || loading) View.VISIBLE else View.GONE
        // Two ticked copies of one walk: said in the bar, and still sent if the user goes on.
        val sendingKeys = sending.map { it.key }.toSet()
        val doubled = sending.count { c -> similar[c.key].orEmpty().any { it.key in sendingKeys } }
        tickedLine.text = if (doubled > 0) {
            res.getString(R.string.sync_ticked_similar, sending.size, tickable.size, doubled)
        } else {
            res.getString(
                R.string.sync_ticked,
                sending.size,
                tickable.size,
                PanelFormat.totalDistance(res, sending.sumOf { it.distanceM }),
            )
        }
        syncNow.isEnabled = !demo && !running && sending.isNotEmpty()
        syncNow.text = if (sending.isEmpty()) res.getString(R.string.sync_now) else res.getString(R.string.sync_send, sending.size)

        onCandidates(shown.filterNot(::processing), highlight)
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
                val chip = LayoutInflater.from(activity).inflate(R.layout.item_sync_type_chip, typeChips, false) as Chip
                typeChips.addView(chip.apply {
                    tag = type
                    text = RecordingTypes.format(res, type)
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
        val tickable = shown().filter { it.key !in hidden && !processing(it) }.map { it.key }
        if (tickable.all { it in ticked }) ticked -= tickable.toSet() else ticked += tickable
        render()
    }

    private fun toggle(key: String) {
        if (key in hidden || SyncProgress.isProcessing(key)) return
        if (!ticked.remove(key)) ticked += key
        render()
    }

    private fun pick(candidate: Candidate) {
        highlight = candidate.key
        render()
        onFrame(listOf(candidate))
        val index = rows.currentList.indexOfFirst { it.key == candidate.key }
        if (index >= 0) list.scrollToPosition(index + 1)
    }

    /** A row's ⓘ: what else covers its time. A waiting one is picked out here; one on the map
     *  opens there, on its day, as Sync history's View on map does. */
    private fun showSimilar(candidate: Candidate) {
        SimilarTracksDialog.show(
            activity,
            of = candidate,
            waiting = similar[candidate.key].orEmpty(),
            onMap = overlaps[candidate.key],
            onPick = ::pick,
            onOpen = { o ->
                val day = runCatching { OffsetDateTime.parse(o.startedAt).atZoneSameInstant(PanelFormat.zone(o.timezone)).toLocalDate().toString() }.getOrNull()
                if (day != null) MainActivity.viewOnMap(activity, o.id, day)
            },
        )
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
                val report = SyncRunner(store, res, HealthConnect.clientOrNull(activity)).send(sending) { sent ->
                    tickedLine.text = res.getString(R.string.sync_progress, sent, sending.size)
                }
                landed(report, sending)
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

    /** What the server has now is on its way to the map ([SyncProgress]): listed as that until
     *  it lands, no longer as something to tick — a session is known, a recording is gone. */
    private fun landed(report: SyncReport, sent: List<Candidate>) {
        val landedSessions = sessions.filter { it.key in report.landed }
        known = known + landedSessions.map { it.externalId }
        // The account has them: off the kept list too, or a restart would offer them again.
        if (landedSessions.isNotEmpty()) scope().launch { waiting.remove(landedSessions.map { it.key }) }
        recordings = recordings.filterNot { it.key in report.landed }
        ticked -= report.landed
        if (highlight in report.landed) highlight = null
        SyncProgress.add(activity, sent.filter { it.key in report.landed })
    }

    /** [SyncProgress] moved on. A failed one is offered again: a session no longer counts as
     *  known, and a recording is back in the store, so the list is read again. */
    private fun progressed() {
        if (!showing) return
        if (showFailed()) restore()
        render()
    }

    /** Says which sent rows failed since last asked — even while the tab was away — and
     *  whether any did. */
    private fun showFailed(): Boolean {
        val failed = SyncProgress.takeFailed()
        if (failed.isEmpty()) return false
        val format = DATE_FORMAT.withZone(ZoneId.systemDefault())
        val lines = failed.map { res.getString(R.string.sync_row_failed, format.format(it.startedAt), RecordingTypes.format(res, it.activityType)) }
        val earlier = problems.text.takeIf { problems.visibility == View.VISIBLE && it.isNotEmpty() }?.toString()
        showProblems(listOfNotNull(earlier) + lines)
        known = known - failed.map { it.externalId }.toSet()
        keepAgain += failed.filter { it.source == HoldMyTrackApi.SOURCE_HEALTH_CONNECT }
        return true
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
        val processing = processing(candidate)
        val note = if (processing) res.getString(R.string.sync_row_processing) else null
        val similarCount = if (processing) 0 else similar[candidate.key].orEmpty().size + (if (candidate.key in overlaps) 1 else 0)
        return Row(
            key = candidate.key,
            title = if (named) candidate.name else date,
            meta = meta,
            note = note,
            processing = processing,
            similar = similarCount,
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
        /** "Adding to your map…" while it's on its way ([processing]). */
        val note: String?,
        /** Sent, and not on the map yet ([SyncProgress]): nothing to tick, hide or edit. */
        val processing: Boolean,
        /** How many tracks cover the same time (`docs/SPEC.md` FR-3.7): other rows, and the
         *  account's activity it overlaps the most. Its ⓘ shows while there are any. */
        val similar: Int,
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
        private val note: TextView = view.findViewById(R.id.candidate_note)
        private val similarButton: View = view.findViewById(R.id.candidate_similar)
        private val spinner: View = view.findViewById(R.id.candidate_processing)
        private val hiddenBadge: View = view.findViewById(R.id.candidate_hidden)
        private val edit: View = view.findViewById(R.id.candidate_edit)
        private val delete: View = view.findViewById(R.id.candidate_delete)

        var key: String? = null
            private set

        var processing = false
            private set

        fun bind(row: Row) {
            key = row.key
            processing = row.processing
            itemView.isSelected = row.highlighted
            title.text = row.title
            meta.text = row.meta
            note.text = row.note
            note.visibility = if (row.note != null) View.VISIBLE else View.GONE
            val alpha = if (row.hidden) HIDDEN_ALPHA else 1f
            title.alpha = alpha
            meta.alpha = alpha
            note.alpha = alpha
            hiddenBadge.visibility = if (row.hidden) View.VISIBLE else View.GONE

            val context = itemView.context
            icon.setImageResource(if (row.ticked) R.drawable.ic_check else typeIcon(row.type))
            icon.backgroundTintList = if (row.ticked) ColorStateList.valueOf(context.getColor(R.color.hmt_accent)) else null
            icon.imageTintList = ColorStateList.valueOf(context.getColor(if (row.ticked) R.color.hmt_on_accent else R.color.hmt_ink_secondary))
            // On its way: the tile spins instead of offering a tick.
            icon.visibility = if (row.processing) View.INVISIBLE else View.VISIBLE
            spinner.visibility = if (row.processing) View.VISIBLE else View.GONE
            val tickable = !row.hidden && !row.processing && !Session.isDemo
            tick.isEnabled = tickable
            tick.isClickable = tickable
            tick.contentDescription = when {
                row.processing -> row.note
                row.ticked -> res.getString(R.string.sync_row_untick, row.title)
                else -> res.getString(R.string.sync_row_tick, row.title)
            }
            tick.setOnClickListener { toggle(row.key) }

            similarButton.visibility = if (row.similar > 0) View.VISIBLE else View.GONE
            if (row.similar > 0) {
                similarButton.contentDescription = res.getQuantityString(R.plurals.sync_row_similar, row.similar, row.title, row.similar)
                similarButton.setOnClickListener { candidate(row.key)?.let(::showSimilar) }
            }

            text.contentDescription = listOfNotNull(res.getString(R.string.sync_row_show, row.title), row.meta, row.note).joinToString(". ")
            text.setOnClickListener { candidate(row.key)?.let(::pick) }
            text.setOnLongClickListener {
                if (tickable) toggle(row.key)
                tickable
            }

            // A recording sent is off the phone already: nothing here to edit or delete.
            val editable = row.recording && !row.processing
            edit.visibility = if (editable) View.VISIBLE else View.GONE
            delete.visibility = if (editable) View.VISIBLE else View.GONE
            if (editable) {
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
            if (holder is RowHolder && !holder.processing && runJob == null) super.getSwipeDirs(recyclerView, holder) else 0

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
