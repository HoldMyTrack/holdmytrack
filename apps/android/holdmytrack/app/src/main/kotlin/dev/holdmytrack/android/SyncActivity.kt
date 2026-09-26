package dev.holdmytrack.android

import android.content.Intent
import android.os.Bundle
import android.util.Log
import android.view.View
import android.widget.Button
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContract
import androidx.appcompat.app.AppCompatActivity
import androidx.health.connect.client.PermissionController
import androidx.lifecycle.lifecycleScope
import dev.holdmytrack.android.health.HealthConnect
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import dev.holdmytrack.android.recording.db.toSyncJson
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
 * Health Connect onboarding and the sync run itself — Path 2's on-device half
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

    private lateinit var status: TextView
    private lateinit var instructions: TextView
    private lateinit var results: TextView
    private lateinit var primary: Button
    private lateinit var secondary: Button
    private lateinit var openHealthConnect: Button
    private lateinit var history: Button

    private var syncJob: Job? = null

    private val permissionLauncher = registerForActivityResult(
        @Suppress("UNCHECKED_CAST")
        PermissionController.createRequestPermissionResultContract()
            as ActivityResultContract<Set<String>, Set<String>>,
    ) { refresh() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_sync)

        status = findViewById(R.id.sync_status)
        instructions = findViewById(R.id.sync_instructions)
        results = findViewById(R.id.sync_results)
        primary = findViewById(R.id.sync_primary)
        secondary = findViewById(R.id.sync_secondary)
        openHealthConnect = findViewById(R.id.sync_open_health_connect)
        history = findViewById(R.id.sync_history)

        openHealthConnect.setOnClickListener { openSettings() }
        history.setOnClickListener { startActivity(Intent(this, SyncStatusActivity::class.java)) }
    }

    override fun onResume() {
        super.onResume()
        // Re-read on every resume, not once: the routes permission is granted in another app,
        // so returning from it is the only moment this screen can learn that it changed.
        refresh()
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

        if (!Session.isSignedIn) {
            status.setText(R.string.sync_needs_account)
            instructions.visibility = View.GONE
            primary.visibility = View.GONE
            secondary.visibility = View.GONE
            history.visibility = View.GONE
            return
        }

        // Reachable without the map, from Health Connect's own rationale link, so it can't
        // count on the map having sent an unconfirmed account to VerifyEmailActivity first —
        // and the server refuses such an account's sync (requireVerified) and its history.
        if (!Session.emailVerified) {
            status.setText(R.string.sync_needs_verified_email)
            instructions.visibility = View.GONE
            primary.visibility = View.GONE
            secondary.visibility = View.GONE
            openHealthConnect.visibility = View.GONE
            history.visibility = View.GONE
            return
        }

        // The server's requireNotDemo (services/server/internal/httpapi/auth.go) rejects
        // POST /sync/activities for a demo account regardless of what this screen offers, the
        // same way it rejects upload/edit/delete for every other client — but the web app
        // doesn't rely on that alone: it disables the Upload control up front, with an
        // explanation, rather than letting the user discover the block from a server error
        // (UploadPanel.tsx's readOnly prop). This is that same treatment on Android: Sync Now
        // and the Health Connect permission flow are hidden rather than left to fail, since a
        // demo account can never actually sync no matter what it grants. History stays
        // visible — reading the demo account's own (shared, seeded) history is not a mutation.
        if (Session.isDemo) {
            status.setText(R.string.sync_demo_read_only)
            instructions.visibility = View.GONE
            primary.visibility = View.GONE
            secondary.visibility = View.GONE
            openHealthConnect.visibility = View.GONE
            history.visibility = View.VISIBLE
            return
        }

        primary.visibility = View.VISIBLE
        primary.isEnabled = true
        history.visibility = View.VISIBLE
        secondary.visibility = View.GONE
        instructions.visibility = View.GONE

        when (readiness) {
            HealthConnect.Readiness.UNAVAILABLE -> {
                status.setText(R.string.sync_unavailable)
                primary.visibility = View.GONE
            }
            HealthConnect.Readiness.UPDATE_REQUIRED -> {
                status.setText(R.string.sync_update_required)
                primary.setText(R.string.sync_open_health_connect)
                primary.setOnClickListener { openSettings() }
            }
            HealthConnect.Readiness.NEEDS_EXERCISE_PERMISSION -> {
                status.setText(R.string.sync_needs_exercise_permission)
                primary.setText(R.string.sync_allow_access)
                primary.setOnClickListener {
                    permissionLauncher.launch(
                        setOf(HealthConnect.READ_EXERCISE, HealthConnect.READ_HISTORY),
                    )
                }
            }
            HealthConnect.Readiness.NEEDS_ROUTES_PERMISSION -> {
                status.setText(R.string.sync_needs_routes_permission)
                // The one place this app spells out another app's menu path. It is not
                // hand-holding: Phase 1 found the screen is two levels down and unlinked, so a
                // user sent to Health Connect without it has no reason to find it.
                instructions.setText(R.string.sync_routes_steps)
                instructions.visibility = View.VISIBLE
                primary.setText(R.string.sync_open_health_connect)
                primary.setOnClickListener { openSettings() }
            }
            HealthConnect.Readiness.NEEDS_HISTORY_PERMISSION -> {
                status.text = getString(R.string.sync_needs_history_permission, lastSyncedLabel())
                instructions.setText(R.string.sync_history_hint)
                instructions.visibility = View.VISIBLE
                // Unlike the routes permission, this one *is* requestable, so the app asks
                // rather than sending the user off to find a toggle.
                primary.setText(R.string.sync_allow_history)
                primary.setOnClickListener {
                    permissionLauncher.launch(setOf(HealthConnect.READ_HISTORY))
                }
                // Syncing still works without it — it just can't reach back — and declining a
                // permission is a real answer, so the shallower sync stays one tap away rather
                // than being held hostage to granting this.
                secondary.setText(R.string.sync_now_anyway)
                secondary.setOnClickListener { startSync() }
                secondary.visibility = View.VISIBLE
            }
            HealthConnect.Readiness.READY -> {
                status.text = getString(R.string.sync_ready, lastSyncedLabel())
                primary.setText(R.string.sync_now)
                primary.setOnClickListener { startSync() }
            }
        }
    }

    private fun lastSyncedLabel(): String {
        val at = cursor().at ?: return getString(R.string.sync_never)
        return DATE_FORMAT.withZone(ZoneId.systemDefault()).format(at)
    }

    /** Keyed by account so two people on one device never inherit each other's position. */
    private fun cursor() = SyncCursor(this, Session.email)

    /**
     * Health Connect sync, then whatever GPS recordings `RecordedActivitiesActivity`'s
     * checkbox has queued — one button, both sources, since submitting a queued recording is
     * exactly the same batched endpoint Health Connect sync already posts to
     * (`docs/IMPLEMENTATION.md` §4.0.4). The recorded flush runs even when Health Connect
     * itself throws (a dead network says nothing about whether the *local* recordings can
     * still go out), just without a combined summary in that rarer case — the next resume's
     * Recorded Activities list reflects whatever did or didn't land either way.
     */
    private fun startSync() {
        val client = HealthConnect.clientOrNull(this) ?: return
        primary.isEnabled = false
        secondary.isEnabled = false
        results.visibility = View.GONE
        status.setText(R.string.sync_running)

        syncJob = lifecycleScope.launch {
            val runner = SyncRunner(client, cursor(), resources)
            try {
                val report = runner.run { progress -> showProgress(progress) }
                show(report, flushRecordedQueue())
            } catch (e: Exception) {
                status.text = getString(R.string.sync_failed, e.message.orEmpty())
                flushRecordedQueue()
            } finally {
                syncJob = null
                primary.isEnabled = true
                secondary.isEnabled = true
                refresh()
            }
        }
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
        status.text = getString(R.string.sync_progress, progress.scanned, progress.synced)
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
    private fun show(report: SyncReport, recordedResult: RecordedSyncResult) {
        val lines = mutableListOf<String>()
        lines += getString(R.string.sync_summary, report.synced, report.alreadyPresent, report.scanned)
        if (report.skippedNoRoute > 0) {
            lines += getString(R.string.sync_skipped_no_route, report.skippedNoRoute)
        }
        for (rejection in report.rejected) {
            lines += getString(
                R.string.sync_rejected_row,
                DATE_FORMAT.withZone(ZoneId.systemDefault()).format(rejection.startedAt),
                rejection.activityType,
                rejection.reason,
            )
        }
        report.stoppedBecause?.let { lines += "\n" + getString(R.string.sync_stopped, it) }
        if (recordedResult.synced > 0 || recordedResult.failed > 0) {
            lines += if (recordedResult.failed > 0) {
                getString(R.string.sync_recorded_summary_with_failed, recordedResult.synced, recordedResult.failed)
            } else {
                getString(R.string.sync_recorded_summary, recordedResult.synced)
            }
        }

        status.setText(R.string.sync_done)
        results.text = lines.joinToString("\n")
        results.visibility = View.VISIBLE
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
            status.setText(R.string.sync_unavailable)
        }
    }

    private companion object {
        const val TAG = "HoldMyTrackSync"
        val DATE_FORMAT: DateTimeFormatter = DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM)
    }
}
