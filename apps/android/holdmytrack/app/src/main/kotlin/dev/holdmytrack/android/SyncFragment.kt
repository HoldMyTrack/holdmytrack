package dev.holdmytrack.android

import android.content.Intent
import android.os.Bundle
import android.util.Log
import android.view.View
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContract
import androidx.appcompat.app.AppCompatActivity
import androidx.fragment.app.Fragment
import androidx.browser.customtabs.CustomTabsIntent
import androidx.health.connect.client.PermissionController
import androidx.lifecycle.lifecycleScope
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import dev.holdmytrack.android.health.HealthConnect
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
 * (`RecordedActivityRows`) — then everything imported so far, the web's `/sync` page
 * ([ImportHistory]). Health Connect's onboarding lives here too — Path 2's on-device half
 * (`docs/adr/0001-three-independent-ingest-paths.md`).
 *
 * The bottom bar's Sync tab in `MainActivity`, and the whole of `SyncActivity`, which is the
 * screen Health Connect opens as this app's permission *rationale* — even signed out, which
 * `MainActivity` never shows. What is read and what it is for sits behind the info button beside
 * Health Connect's title, and opens by itself when Health Connect launched the screen to ask
 * exactly that. The same text serves
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
 * regardless of what is granted — and when another tab hides this one. Cancelling mid-run is safe by construction rather than by
 * cleanup: the watermark only ever moved over activities the server had already confirmed.
 */
class SyncFragment : Fragment(R.layout.fragment_sync) {

    private lateinit var accountNotice: TextView
    private lateinit var recordedSection: View
    private lateinit var recordedRows: RecordedActivityRows
    private lateinit var healthConnectSection: View
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

    private val permissionLauncher = registerForActivityResult(
        @Suppress("UNCHECKED_CAST")
        PermissionController.createRequestPermissionResultContract()
            as ActivityResultContract<Set<String>, Set<String>>,
    ) { refresh() }

    /** The fragment's own views, found the way an Activity finds its own. */
    private fun <T : View> findViewById(id: Int): T = requireView().findViewById(id)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        super.onViewCreated(view, savedInstanceState)
        // Its own title as a tab; SyncActivity has an action bar for it.
        findViewById<View>(R.id.sync_title).visibility = if (activity is MainActivity) View.VISIBLE else View.GONE

        accountNotice = findViewById(R.id.sync_account_notice)
        recordedSection = findViewById(R.id.sync_recorded_section)
        recordedRows = RecordedActivityRows(
            requireActivity() as AppCompatActivity,
            findViewById<LinearLayout>(R.id.sync_recorded_rows),
            findViewById(R.id.sync_recorded_empty),
            onChanged = ::updateSyncNow,
        )
        healthConnectSection = findViewById(R.id.sync_health_connect_section)
        historySection = findViewById(R.id.sync_history_section)
        history = ImportHistory(view, onViewOnMap = ::viewOnMap)
        status = findViewById(R.id.sync_status)
        instructions = findViewById(R.id.sync_instructions)
        results = findViewById(R.id.sync_results)
        problems = findViewById(R.id.sync_problems)
        primary = findViewById(R.id.sync_primary)
        syncNow = findViewById(R.id.sync_now)

        findViewById<Button>(R.id.sync_health_connect_info).setOnClickListener { showRationale() }
        syncNow.setOnClickListener { startSync() }

        // Health Connect asked "why does this app want my data" — answer it straight away,
        // rather than making the user find the info button. Only on a fresh start, so a
        // rotation after dismissing it doesn't bring it back.
        if (savedInstanceState == null && requireActivity().intent?.action in RATIONALE_ACTIONS) showRationale()
    }

    /** What is read from Health Connect and what it is for — the permission rationale — with
     *  the privacy policy a button away: Health Connect requires the policy this screen leads to
     *  to be the one on the Play listing, and both are the web's /privacy. */
    private fun showRationale() {
        MaterialAlertDialogBuilder(requireContext())
            .setTitle(R.string.sync_health_connect_info)
            .setMessage(R.string.sync_rationale)
            .setPositiveButton(android.R.string.ok, null)
            .setNeutralButton(R.string.privacy_policy) { _, _ ->
                CustomTabsIntent.Builder().build().launchUrl(requireContext(), HoldMyTrackApi.webPageUri("/privacy"))
            }
            .show()
    }

    override fun onResume() {
        super.onResume()
        // Re-read on every resume, not once: the routes permission is granted in another app,
        // so returning from it is the only moment this screen can learn that it changed — and
        // returning from a recording's Edit is how this screen learns that one changed. Not
        // while another tab hides this one, which reads it again when it shows.
        if (!isHidden) show()
    }

    override fun onHiddenChanged(hidden: Boolean) {
        super.onHiddenChanged(hidden)
        if (hidden) leave() else show()
    }

    override fun onStop() {
        leave()
        super.onStop()
    }

    private fun show() {
        refresh()
        if (syncJob == null) recordedRows.reload()
    }

    /** Off screen — stopped, or hidden by another tab: the run stops, and so does the history's
     *  polling. */
    private fun leave() {
        syncJob?.cancel()
        syncJob = null
        history.stop()
    }

    /** A history row's View on map: the map, on that activity (`MainActivity.viewOnMap`) — the
     *  Map tab, or, from SyncActivity, the map under it. */
    private fun viewOnMap(activityId: String, startedAt: String) {
        MainActivity.viewOnMap(requireContext(), activityId, startedAt)
        if (activity !is MainActivity) activity?.finish()
    }

    private fun refresh() {
        if (syncJob != null || view == null) return
        viewLifecycleOwner.lifecycleScope.launch {
            val readiness = HealthConnect.readiness(requireContext())
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
        history.start()
        if (Session.isDemo) {
            showAccountNotice(getString(R.string.sync_demo_read_only))
            recordedSection.visibility = View.VISIBLE
            healthConnectSection.visibility = View.GONE
            syncNow.visibility = View.GONE
            return
        }

        accountNotice.visibility = View.GONE
        recordedSection.visibility = View.VISIBLE
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
    private fun cursor() = SyncCursor(requireContext(), Session.email)

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
        val client = if (includesHealthConnect()) HealthConnect.clientOrNull(requireContext()) else null
        problems.visibility = View.GONE
        showResults(getString(if (client != null) R.string.sync_running else R.string.sync_running_recorded))

        syncJob = viewLifecycleOwner.lifecycleScope.launch {
            try {
                val report = client?.let { SyncRunner(it, cursor(), resources).run { progress -> showProgress(progress) } }
                show(report, flushRecordedQueue())
            } catch (e: Exception) {
                showProblems(listOf(getString(R.string.sync_failed, e.message.orEmpty())))
                results.visibility = View.GONE
                flushRecordedQueue()
            } finally {
                syncJob = null
                if (view != null) {
                    recordedRows.reload()
                    refresh()
                }
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
        val store = RecordedActivityStore(requireContext())
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
        status.setTextColor(requireContext().getColor(if (failed) R.color.hmt_danger else R.color.hmt_ink))
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

    companion object {
        private const val TAG = "HoldMyTrackSync"

        /** Health Connect's rationale request, and its "see how this app used your data" link
         *  (the manifest's `ViewPermissionUsageActivity` alias). */
        private val RATIONALE_ACTIONS = setOf(
            "androidx.health.ACTION_SHOW_PERMISSIONS_RATIONALE",
            Intent.ACTION_VIEW_PERMISSION_USAGE,
        )
        private val DATE_FORMAT: DateTimeFormatter = DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM)
    }
}
