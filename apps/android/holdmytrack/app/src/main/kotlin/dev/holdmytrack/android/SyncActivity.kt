package dev.holdmytrack.android

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.text.TextUtils
import android.util.Log
import android.view.View
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContract
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.browser.customtabs.CustomTabsIntent
import androidx.health.connect.client.PermissionController
import androidx.lifecycle.lifecycleScope
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import dev.holdmytrack.android.health.HealthConnect
import dev.holdmytrack.android.imports.FileImports
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.RecordedActivityRows
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import dev.holdmytrack.android.recording.db.toSyncJson
import dev.holdmytrack.android.sync.ImportHistory
import dev.holdmytrack.android.sync.SyncCursor
import dev.holdmytrack.android.sync.SyncProgress
import dev.holdmytrack.android.sync.SyncReport
import dev.holdmytrack.android.sync.SyncRunner
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * Sync: every place activities come from on this device and one "Sync now" that sends all of
 * it — Health Connect, once it can be read, and every GPS recording on the device
 * (`RecordedActivityRows`) — then files to upload, the web's Upload menu ([FileImports]), and
 * everything imported so far, the web's `/sync` page ([ImportHistory]). Health Connect's
 * onboarding lives here too — Path 2's on-device half
 * (`docs/adr/0001-three-independent-ingest-paths.md`). Another app's file handed to this app to
 * open or share lands here too, and is uploaded as if picked.
 *
 * Also the screen Health Connect opens as this app's permission *rationale*. What is read and
 * what it is for sits behind the info button beside Health Connect's title, and opens by
 * itself when Health Connect launched this screen to ask exactly that. The same text serves
 * both purposes; a rationale that says something different from the app's own explanation
 * would be the wrong kind of surprise.
 *
 * **Granting access is two flows, not one, and the second cannot be automated.** Reading
 * sessions is an ordinary runtime permission. Reading their routes is not requestable at all:
 * measured on a device, asking for it simply returns without it, and the user has to grant it
 * inside Health Connect, on a screen two levels below the app's own permission page and not
 * linked from it. So this screen names the taps rather than saying "grant permissions".
 *
 * **The run is tied to this screen being on it.** `syncJob` is cancelled in `onStop`, which is
 * the real foreground boundary — routes read in the background come back `ConsentRequired`
 * regardless of what is granted. Cancelling mid-run is safe by construction rather than by
 * cleanup: the watermark only ever moved over activities the server had already confirmed.
 */
class SyncActivity : AppCompatActivity() {

    private lateinit var accountNotice: TextView
    private lateinit var recordedSection: View
    private lateinit var recordedRows: RecordedActivityRows
    private lateinit var healthConnectSection: View
    private lateinit var filesSection: View
    private lateinit var transfers: LinearLayout
    private lateinit var fileNotes: TextView
    private lateinit var fileErrors: TextView
    private lateinit var historySection: View
    private lateinit var history: ImportHistory
    private lateinit var status: TextView
    private lateinit var instructions: TextView
    private lateinit var results: TextView
    private lateinit var problems: TextView
    private lateinit var primary: Button
    private lateinit var syncNow: Button

    private var syncJob: Job? = null

    /** The last readiness `render` saw; null until the first read comes back. */
    private var readiness: HealthConnect.Readiness? = null

    /** [FileImports.sentCount] as of the last history read: when it moves, a file has gone and
     *  its jobs are worth reading. */
    private var filesSeen = FileImports.sentCount

    private val filesListener: () -> Unit = {
        renderFiles()
        if (FileImports.sentCount != filesSeen && historySection.visibility == View.VISIBLE) {
            filesSeen = FileImports.sentCount
            history.start()
        }
    }

    // Any type: providers label .gpx and .fit all sorts of ways, and FileImports says which it won't send.
    private val filePicker = registerForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { uris ->
        if (uris.isNotEmpty()) importFiles(uris)
    }

    private val permissionLauncher = registerForActivityResult(
        @Suppress("UNCHECKED_CAST")
        PermissionController.createRequestPermissionResultContract()
            as ActivityResultContract<Set<String>, Set<String>>,
    ) { refresh() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_sync)

        accountNotice = findViewById(R.id.sync_account_notice)
        recordedSection = findViewById(R.id.sync_recorded_section)
        recordedRows = RecordedActivityRows(
            this,
            findViewById<LinearLayout>(R.id.sync_recorded_rows),
            findViewById(R.id.sync_recorded_empty),
            onChanged = ::updateSyncNow,
        )
        healthConnectSection = findViewById(R.id.sync_health_connect_section)
        filesSection = findViewById(R.id.sync_files_section)
        transfers = findViewById(R.id.sync_files_transfers)
        fileNotes = findViewById(R.id.sync_files_notes)
        fileErrors = findViewById(R.id.sync_files_errors)
        historySection = findViewById(R.id.sync_history_section)
        history = ImportHistory(findViewById(android.R.id.content), onViewOnMap = ::viewOnMap)
        status = findViewById(R.id.sync_status)
        instructions = findViewById(R.id.sync_instructions)
        results = findViewById(R.id.sync_results)
        problems = findViewById(R.id.sync_problems)
        primary = findViewById(R.id.sync_primary)
        syncNow = findViewById(R.id.sync_now)

        findViewById<Button>(R.id.sync_health_connect_info).setOnClickListener { showRationale() }
        syncNow.setOnClickListener { startSync() }
        findViewById<Button>(R.id.sync_files_choose).setOnClickListener { filePicker.launch(arrayOf("*/*")) }
        findViewById<Button>(R.id.sync_files_guide_google_health).setOnClickListener { openPage(GOOGLE_HEALTH_GUIDE_PATH) }
        findViewById<Button>(R.id.sync_files_guide_timeline).setOnClickListener { openPage(TimelineImportActivity.GUIDE_PATH) }

        // Health Connect asked "why does this app want my data" — answer it straight away,
        // rather than making the user find the info button. Only on a fresh start, so a
        // rotation after dismissing it doesn't bring it back.
        if (savedInstanceState == null && intent?.action in RATIONALE_ACTIONS) showRationale()
        if (savedInstanceState == null) importShared(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        importShared(intent)
    }

    /** Files another app handed this one to open or share: uploaded as if picked here, when
     *  this account can (the notice says why not otherwise). */
    private fun importShared(intent: Intent?) {
        val uris = when (intent?.action) {
            Intent.ACTION_VIEW -> listOfNotNull(intent.data)
            Intent.ACTION_SEND -> listOfNotNull(intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java))
            Intent.ACTION_SEND_MULTIPLE -> intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java).orEmpty()
            else -> emptyList()
        }
        if (uris.isNotEmpty() && canUpload()) importFiles(uris)
    }

    /** The server refuses a demo account's uploads (`requireNotDemo`) and an unconfirmed one's. */
    private fun canUpload() = Session.isSignedIn && Session.emailVerified && !Session.isDemo

    /** Picked or shared files to [FileImports]; a Timeline export among them opens its own screen. */
    private fun importFiles(uris: List<Uri>) {
        FileImports.enqueue(this, uris)?.let { TimelineImportActivity.open(this, it) }
    }

    private fun openPage(path: String) {
        CustomTabsIntent.Builder().build().launchUrl(this, HoldMyTrackApi.webPageUri(path))
    }

    /** Each file queued or uploading, then the uploads' notes and errors. */
    private fun renderFiles() {
        transfers.removeAllViews()
        for (transfer in FileImports.transfers) {
            val row = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
            val name = TextView(this).apply {
                text = transfer.name
                maxLines = 1
                ellipsize = TextUtils.TruncateAt.MIDDLE
            }
            val status = TextView(this).apply {
                setTextColor(getColor(R.color.hmt_ink_meta))
                text = when {
                    transfer.sent < 0 -> getString(R.string.upload_queued)
                    transfer.size > 0 -> getString(R.string.upload_uploading, (transfer.sent * 100 / transfer.size).toInt())
                    else -> getString(R.string.upload_uploading_mb, (transfer.sent shr 20).toInt())
                }
            }
            row.addView(name, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
            row.addView(status)
            transfers.addView(row)
        }
        val (errors, notes) = FileImports.notes.partition { it.error }
        fileNotes.text = notes.joinToString("\n") { it.text }
        fileNotes.visibility = if (notes.isEmpty()) View.GONE else View.VISIBLE
        fileErrors.text = errors.joinToString("\n") { it.text }
        fileErrors.visibility = if (errors.isEmpty()) View.GONE else View.VISIBLE
    }

    override fun onStart() {
        super.onStart()
        FileImports.addListener(filesListener)
        renderFiles()
    }

    /** What is read from Health Connect and what it is for — the permission rationale — with
     *  the privacy policy a button away: Health Connect requires the policy this screen leads to
     *  to be the one on the Play listing, and both are the web's /privacy. */
    private fun showRationale() {
        MaterialAlertDialogBuilder(this)
            .setTitle(R.string.sync_health_connect_info)
            .setMessage(R.string.sync_rationale)
            .setPositiveButton(android.R.string.ok, null)
            .setNeutralButton(R.string.privacy_policy) { _, _ ->
                CustomTabsIntent.Builder().build().launchUrl(this, HoldMyTrackApi.webPageUri("/privacy"))
            }
            .show()
    }

    override fun onResume() {
        super.onResume()
        // Re-read on every resume, not once: the routes permission is granted in another app,
        // so returning from it is the only moment this screen can learn that it changed — and
        // returning from a recording's Edit is how this screen learns that one changed.
        refresh()
        if (syncJob == null) recordedRows.reload()
    }

    override fun onStop() {
        syncJob?.cancel()
        syncJob = null
        history.stop()
        FileImports.removeListener(filesListener)
        super.onStop()
    }

    /** A history row's View on map: the map, on that activity (`MainActivity.viewOnMap`). */
    private fun viewOnMap(activityId: String, startedAt: String) {
        MainActivity.viewOnMap(this, activityId, startedAt)
        finish()
    }

    private fun refresh() {
        if (syncJob != null) return
        lifecycleScope.launch {
            val readiness = HealthConnect.readiness(this@SyncActivity)
            render(readiness)
        }
    }

    private fun render(readiness: HealthConnect.Readiness) {
        this.readiness = readiness

        if (!Session.isSignedIn) {
            showAccountBlock(getString(R.string.sync_needs_account))
            return
        }

        // Reachable without the map, from Health Connect's own rationale link, so it can't
        // count on the map having sent an unconfirmed account to VerifyEmailActivity first —
        // and the server refuses such an account's sync (requireVerified) and its history.
        if (!Session.emailVerified) {
            showAccountBlock(getString(R.string.sync_needs_verified_email))
            return
        }

        // The server's requireNotDemo (services/server/internal/httpapi/auth.go) rejects
        // POST /sync/activities for a demo account regardless of what this screen offers, the
        // same way it rejects upload/edit/delete for every other client — but the web app
        // doesn't rely on that alone: it disables the Upload control up front, with an
        // explanation, rather than letting the user discover the block from a server error.
        // This is that same treatment on Android: Sync now and the Health Connect section are
        // hidden rather than left to fail. Recordings stay listed — a demo account can still
        // record, edit and delete them locally — and so does its history, as on the web.
        historySection.visibility = View.VISIBLE
        filesSeen = FileImports.sentCount
        history.start()
        if (Session.isDemo) {
            showAccountNotice(getString(R.string.sync_demo_read_only))
            recordedSection.visibility = View.VISIBLE
            healthConnectSection.visibility = View.GONE
            filesSection.visibility = View.GONE
            syncNow.visibility = View.GONE
            return
        }

        accountNotice.visibility = View.GONE
        recordedSection.visibility = View.VISIBLE
        healthConnectSection.visibility = View.VISIBLE
        filesSection.visibility = View.VISIBLE
        syncNow.visibility = View.VISIBLE
        renderHealthConnect(readiness)
        updateSyncNow()
    }

    /** Signed out or unconfirmed: nothing here can sync, so only the reason is shown. */
    private fun showAccountBlock(text: String) {
        showAccountNotice(text)
        recordedSection.visibility = View.GONE
        healthConnectSection.visibility = View.GONE
        filesSection.visibility = View.GONE
        syncNow.visibility = View.GONE
        historySection.visibility = View.GONE
    }

    private fun showAccountNotice(text: String) {
        accountNotice.text = text
        accountNotice.visibility = View.VISIBLE
    }

    /**
     * Where Health Connect's setup stands, and its one remaining step as the section's button.
     * Sync now includes Health Connect whenever it can be read — READY, or READY-but-30-days,
     * which still syncs, just not as far back.
     */
    private fun renderHealthConnect(readiness: HealthConnect.Readiness) {
        primary.visibility = View.VISIBLE
        instructions.visibility = View.GONE

        when (readiness) {
            HealthConnect.Readiness.UNAVAILABLE -> {
                showStatus(getString(R.string.sync_unavailable))
                primary.visibility = View.GONE
            }
            HealthConnect.Readiness.UPDATE_REQUIRED -> {
                showStatus(getString(R.string.sync_update_required))
                primary.setText(R.string.sync_open_health_connect)
                primary.setOnClickListener { openSettings() }
            }
            HealthConnect.Readiness.NEEDS_EXERCISE_PERMISSION -> {
                showStatus(getString(R.string.sync_needs_exercise_permission))
                primary.setText(R.string.sync_allow_access)
                primary.setOnClickListener {
                    permissionLauncher.launch(
                        setOf(HealthConnect.READ_EXERCISE, HealthConnect.READ_HISTORY),
                    )
                }
            }
            HealthConnect.Readiness.NEEDS_ROUTES_PERMISSION -> {
                showStatus(getString(R.string.sync_needs_routes_permission))
                // The one place this app spells out another app's menu path. It is not
                // hand-holding: on a device the screen proved is two levels down and unlinked, so a
                // user sent to Health Connect without it has no reason to find it.
                instructions.setText(R.string.sync_routes_steps)
                instructions.visibility = View.VISIBLE
                primary.setText(R.string.sync_open_health_connect)
                primary.setOnClickListener { openSettings() }
            }
            HealthConnect.Readiness.NEEDS_HISTORY_PERMISSION -> {
                showStatus(getString(R.string.sync_needs_history_permission, lastSyncedLabel()))
                instructions.setText(R.string.sync_history_hint)
                instructions.visibility = View.VISIBLE
                // Unlike the routes permission, this one *is* requestable, so the app asks
                // rather than sending the user off to find a toggle. Syncing still works
                // without it — it just can't reach back — and declining a permission is a real
                // answer, so Sync now takes the shallower sync rather than holding it hostage
                // to granting this.
                primary.setText(R.string.sync_allow_history)
                primary.setOnClickListener {
                    permissionLauncher.launch(setOf(HealthConnect.READ_HISTORY))
                }
            }
            HealthConnect.Readiness.READY -> {
                showStatus(getString(R.string.sync_ready, lastSyncedLabel()))
                // Sync now, below both sections, is the action here.
                primary.visibility = View.GONE
            }
        }
    }

    private fun HealthConnect.Readiness.isSyncable() =
        this == HealthConnect.Readiness.READY || this == HealthConnect.Readiness.NEEDS_HISTORY_PERMISSION

    /** Health Connect is in this run whenever it can be read. */
    private fun includesHealthConnect() = readiness?.isSyncable() == true

    /** Enabled when there's something to send and no run already going. */
    private fun updateSyncNow() {
        syncNow.isEnabled = syncJob == null && (includesHealthConnect() || recordedRows.count > 0)
    }

    private fun lastSyncedLabel(): String {
        val at = cursor().at ?: return getString(R.string.sync_never)
        return DATE_FORMAT.withZone(ZoneId.systemDefault()).format(at)
    }

    /** Keyed by account so two people on one device never inherit each other's position. */
    private fun cursor() = SyncCursor(this, Session.email)

    /**
     * Health Connect sync when it can be read, then every GPS recording on the device — one
     * button, both sources, since submitting a checked recording is exactly the same
     * batched endpoint Health Connect sync already posts to (`docs/IMPLEMENTATION.md` §4.0.4).
     * The recorded flush runs even when Health Connect itself throws (a dead network says
     * nothing about whether the *local* recordings can still go out), just without a combined
     * summary in that rarer case — the list re-read afterwards reflects whatever did or didn't
     * land either way.
     */
    private fun startSync() {
        val client = if (includesHealthConnect()) HealthConnect.clientOrNull(this) else null
        problems.visibility = View.GONE
        showResults(getString(if (client != null) R.string.sync_running else R.string.sync_running_recorded))

        syncJob = lifecycleScope.launch {
            try {
                val report = client?.let { SyncRunner(it, cursor(), resources).run { progress -> showProgress(progress) } }
                show(report, flushRecordedQueue())
            } catch (e: Exception) {
                showProblems(listOf(getString(R.string.sync_failed, e.message.orEmpty())))
                results.visibility = View.GONE
                flushRecordedQueue()
            } finally {
                syncJob = null
                recordedRows.reload()
                refresh()
            }
        }
        updateSyncNow()
    }

    /** Submits every GPS recording on the device (`RecordedActivityStore`) and deletes each
     *  one from the device on success — it lives on the server from then on, in the activity
     *  list and on the map. A rejected or failed submit is left where it is — the same
     *  resumable-retry posture `SyncRunner`'s own watermark already uses, so the next "Sync
     *  now" tries it again with no action needed from the user. */
    private suspend fun flushRecordedQueue(): RecordedSyncResult {
        val store = RecordedActivityStore(this)
        var synced = 0
        var failed = 0
        for (record in store.all()) {
            val status = runCatching { HoldMyTrackApi.syncActivities(listOf(record.toSyncJson()), HoldMyTrackApi.SOURCE_RECORDED) }
                .getOrNull()?.firstOrNull()?.status
            if (status == "enqueued" || status == "already_processed") {
                store.delete(record.id)
                synced++
            } else {
                failed++
            }
        }
        return RecordedSyncResult(synced, failed)
    }

    private data class RecordedSyncResult(val synced: Int, val failed: Int)

    private fun showProgress(progress: SyncProgress) {
        showResults(getString(R.string.sync_progress, progress.scanned, progress.synced))
    }

    private fun showResults(text: CharSequence) {
        results.text = text
        results.visibility = View.VISIBLE
    }

    /**
     * The per-activity outcome of a run, which is the whole of Phase 3's rejection feedback:
     * why a session the user can see in Health Connect never appeared in HoldMyTrack. It says which
     * were skipped for having no route — an ordinary indoor workout, not a fault — separately
     * from which the server actually refused, and with the server's own reason, since the two
     * are different things and a single "couldn't sync" would conflate them.
     *
     * This is the run you just watched; the history under it ([ImportHistory]) is the
     * browsable record.
     */
    private fun show(report: SyncReport?, recordedResult: RecordedSyncResult) {
        val lines = mutableListOf(getString(R.string.sync_done))
        val trouble = mutableListOf<String>()
        if (report != null) {
            lines += getString(R.string.sync_summary, report.synced, report.alreadyPresent, report.scanned)
            if (report.skippedNoRoute > 0) {
                lines += getString(R.string.sync_skipped_no_route, report.skippedNoRoute)
            }
            for (rejection in report.rejected) {
                trouble += getString(
                    R.string.sync_rejected_row,
                    DATE_FORMAT.withZone(ZoneId.systemDefault()).format(rejection.startedAt),
                    rejection.activityType,
                    rejection.reason,
                )
            }
            report.stoppedBecause?.let { trouble += getString(R.string.sync_stopped, it) }
        }
        if (recordedResult.synced > 0 || recordedResult.failed > 0) {
            lines += if (recordedResult.failed > 0) {
                getString(R.string.sync_recorded_summary_with_failed, recordedResult.synced, recordedResult.failed)
            } else {
                getString(R.string.sync_recorded_summary, recordedResult.synced)
            }
        }

        showResults(lines.joinToString("\n"))
        showProblems(trouble)
    }

    /** What went wrong in the run — rejected activities, why it stopped early, or the whole
     *  run failing — in the error box, which stays after the status line has moved on. */
    private fun showProblems(lines: List<String>) {
        problems.text = lines.joinToString("\n")
        problems.visibility = if (lines.isEmpty()) View.GONE else View.VISIBLE
    }

    /** Where Health Connect stands, in a notice — or in the error box's colors when it's a
     *  failure. */
    private fun showStatus(text: CharSequence, failed: Boolean = false) {
        status.text = text
        status.setBackgroundResource(if (failed) R.drawable.bg_notice_error else R.drawable.bg_notice)
        status.setTextColor(getColor(if (failed) R.color.hmt_danger else R.color.hmt_ink))
    }

    /**
     * Catches broadly rather than just `ActivityNotFoundException`, because the failure that
     * actually happened here was a `SecurityException`: an earlier version aimed at the
     * platform's per-app permission screen, which is guarded by a signature permission and
     * takes the whole process down when an ordinary app launches it. A button that cannot open
     * a screen should say so, not crash the app around it.
     */
    private fun openSettings() {
        try {
            startActivity(HealthConnect.settingsIntent())
        } catch (e: Exception) {
            Log.w(TAG, "could not open Health Connect", e)
            showStatus(getString(R.string.sync_unavailable), failed = true)
        }
    }

    private companion object {
        const val TAG = "HoldMyTrackSync"

        /** The step-by-step Google Health (Takeout) export guide (root `docs/SPEC.md` FR-10.5). */
        const val GOOGLE_HEALTH_GUIDE_PATH = "/help/google-health-export"

        /** Health Connect's rationale request, and its "see how this app used your data" link
         *  (the manifest's `ViewPermissionUsageActivity` alias). */
        val RATIONALE_ACTIONS = setOf(
            "androidx.health.ACTION_SHOW_PERMISSIONS_RATIONALE",
            Intent.ACTION_VIEW_PERMISSION_USAGE,
        )
        val DATE_FORMAT: DateTimeFormatter = DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM)
    }
}
