package dev.holdmytrack.android

import android.os.Bundle
import android.util.Log
import android.view.View
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContract
import androidx.appcompat.app.AppCompatActivity
import androidx.health.connect.client.PermissionController
import androidx.lifecycle.lifecycleScope
import com.google.android.material.checkbox.MaterialCheckBox
import dev.holdmytrack.android.health.HealthConnect
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.RecordedActivityRows
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import dev.holdmytrack.android.recording.db.toSyncJson
import dev.holdmytrack.android.sync.SyncCursor
import dev.holdmytrack.android.sync.SyncProgress
import dev.holdmytrack.android.sync.SyncReport
import dev.holdmytrack.android.sync.SyncRunner
import dev.holdmytrack.android.sync.SyncSources
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * Sync Source: every place activities come from on this device, each with its own checkbox,
 * and one "Sync now" that sends whatever is checked — the GPS recordings checked row by row
 * (`RecordedActivityRows`), and Health Connect as a whole (`SyncSources`). Health Connect's
 * onboarding lives here too, under its checkbox — Path 2's on-device half
 * (`docs/adr/0001-three-independent-ingest-paths.md`).
 *
 * Also the screen Health Connect opens as this app's permission *rationale*, which is why it
 * leads with what is read and what it is for before asking for anything. The same text serves
 * both purposes; a rationale that says something different from the app's own explanation
 * would be the wrong kind of surprise.
 *
 * **Granting access is two flows, not one, and the second cannot be automated.** Reading
 * sessions is an ordinary runtime permission. Reading their routes is not requestable at all:
 * Phase 1 measured that asking for it simply returns without it, and the user has to grant it
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
    private lateinit var healthConnectBox: MaterialCheckBox
    private lateinit var status: TextView
    private lateinit var instructions: TextView
    private lateinit var results: TextView
    private lateinit var problems: TextView
    private lateinit var primary: Button
    private lateinit var openHealthConnect: Button
    private lateinit var syncNow: Button

    private var syncJob: Job? = null

    /** The last readiness `render` saw; null until the first read comes back. */
    private var readiness: HealthConnect.Readiness? = null

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
        healthConnectBox = findViewById(R.id.sync_health_connect)
        status = findViewById(R.id.sync_status)
        instructions = findViewById(R.id.sync_instructions)
        results = findViewById(R.id.sync_results)
        problems = findViewById(R.id.sync_problems)
        primary = findViewById(R.id.sync_primary)
        openHealthConnect = findViewById(R.id.sync_open_health_connect)
        syncNow = findViewById(R.id.sync_now)

        openHealthConnect.setOnClickListener { openSettings() }
        syncNow.setOnClickListener { startSync() }
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
        super.onStop()
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
        // explanation, rather than letting the user discover the block from a server error
        // (UploadPanel.tsx's readOnly prop). This is that same treatment on Android: Sync now
        // and the Health Connect section are hidden rather than left to fail. Recordings stay
        // listed — a demo account can still record, edit and delete them locally — just not
        // checkable. (Its history is the map panel's Sync tab, which reading doesn't change.)
        if (Session.isDemo) {
            showAccountNotice(getString(R.string.sync_demo_read_only))
            recordedSection.visibility = View.VISIBLE
            recordedRows.checkable = false
            healthConnectSection.visibility = View.GONE
            syncNow.visibility = View.GONE
            return
        }

        accountNotice.visibility = View.GONE
        recordedSection.visibility = View.VISIBLE
        recordedRows.checkable = true
        healthConnectSection.visibility = View.VISIBLE
        syncNow.visibility = View.VISIBLE
        renderHealthConnect(readiness)
        updateSyncNow()
    }

    /** Signed out or unconfirmed: nothing here can sync, so only the reason is shown. */
    private fun showAccountBlock(text: String) {
        showAccountNotice(text)
        recordedSection.visibility = View.GONE
        healthConnectSection.visibility = View.GONE
        syncNow.visibility = View.GONE
    }

    private fun showAccountNotice(text: String) {
        accountNotice.text = text
        accountNotice.visibility = View.VISIBLE
    }

    /**
     * Health Connect's checkbox and whatever its setup still needs. The checkbox is only
     * enabled while Health Connect can actually be read — READY, or READY-but-30-days, which
     * still syncs, just not as far back — and shows unticked otherwise, without touching the
     * saved choice, so finishing setup brings back whatever the user last picked. The setup
     * step itself is the section's primary button, and "Open Health Connect" stays available
     * alongside it except where the primary already is that button.
     */
    private fun renderHealthConnect(readiness: HealthConnect.Readiness) {
        val syncable = readiness.isSyncable()
        healthConnectBox.setOnCheckedChangeListener(null)
        healthConnectBox.isEnabled = syncable
        healthConnectBox.isChecked = syncable && sources().healthConnect
        healthConnectBox.setOnCheckedChangeListener { _, checked ->
            sources().healthConnect = checked
            updateSyncNow()
        }

        // Shown only where the primary button isn't already this button: in the two states
        // that send the user to Health Connect, the primary says so, and two identical buttons
        // stacked on each other is just a question about which one is the real one.
        openHealthConnect.visibility = when (readiness) {
            HealthConnect.Readiness.NEEDS_EXERCISE_PERMISSION,
            HealthConnect.Readiness.NEEDS_HISTORY_PERMISSION,
            HealthConnect.Readiness.READY,
            -> View.VISIBLE
            else -> View.GONE
        }
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
                // hand-holding: Phase 1 found the screen is two levels down and unlinked, so a
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
                // answer, so the checkbox stays enabled and Sync now takes the shallower sync
                // rather than holding it hostage to granting this.
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

    /** Health Connect is in this run only when it's both ticked and readable. */
    private fun includesHealthConnect() =
        readiness?.isSyncable() == true && sources().healthConnect

    /** Enabled when there's something checked to send and no run already going. */
    private fun updateSyncNow() {
        syncNow.isEnabled = syncJob == null && (includesHealthConnect() || recordedRows.checkedCount > 0)
    }

    private fun lastSyncedLabel(): String {
        val at = cursor().at ?: return getString(R.string.sync_never)
        return DATE_FORMAT.withZone(ZoneId.systemDefault()).format(at)
    }

    /** Keyed by account so two people on one device never inherit each other's position. */
    private fun cursor() = SyncCursor(this, Session.email)

    private fun sources() = SyncSources(this, Session.email)

    /**
     * Health Connect sync when its box is ticked, then whatever GPS recordings are checked —
     * one button, both sources, since submitting a checked recording is exactly the same
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

    /** Submits every locally queued GPS recording (`RecordedActivityStore`,
     *  `SyncStatus.QUEUED`) and deletes each one from the device on success — it lives on the
     *  server from then on, in the activity list and on the map. A rejected or
     *  failed submit is left queued rather than reverted — the same resumable-retry posture
     *  `SyncRunner`'s own watermark already uses, so the next "Sync Now" tries it again with
     *  no action needed from the user. */
    private suspend fun flushRecordedQueue(): RecordedSyncResult {
        val store = RecordedActivityStore(this)
        var synced = 0
        var failed = 0
        for (record in store.queued()) {
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
     * The persistent, browsable version of this belongs to the sync status dashboard in the
     * next phase; this is the run you just watched, not a history.
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
        val DATE_FORMAT: DateTimeFormatter = DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM)
    }
}
