package dev.holdmytrack.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.ColorStateList
import android.os.Bundle
import android.view.View
import android.widget.ImageView
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContract
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.health.connect.client.PermissionController
import androidx.lifecycle.lifecycleScope
import dev.holdmytrack.android.health.HealthConnect
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.sync.HealthConnectCard
import dev.holdmytrack.android.sync.HealthConnectCard.Companion.isReadable
import dev.holdmytrack.android.sync.SyncSources
import dev.holdmytrack.android.sync.WaitingLoader
import dev.holdmytrack.android.sync.WaitingStore
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * Settings › Sync (`apps/android/docs/SPEC.md` FR-3.1): where journeys come from. Health Connect,
 * off until switched on here ([SyncSources]) and then its permissions as one checklist
 * ([HealthConnectCard]), and Recorded on this phone, always on. Opened from the You tab and the
 * Sync tab, and by Health Connect itself: the screen it opens as this app's permission
 * *rationale*, and from "see how this app used your data" — so it opens even signed out, which
 * the app's own Sync tab (`MainActivity`, `panel/SyncTab`) never is. Launched to answer exactly
 * the rationale question, it opens the rationale straight away; for a signed-in account a button
 * leads on to the Sync tab, where what's on the phone is chosen.
 */
class SyncActivity : AppCompatActivity() {

    private lateinit var card: HealthConnectCard
    private lateinit var accountNotice: TextView
    private lateinit var recorded: View
    private lateinit var openList: View
    /** The first load after switching on, while it reads ([WaitingLoader]). */
    private var firstLoad: Job? = null

    private val permissionLauncher = registerForActivityResult(
        @Suppress("UNCHECKED_CAST")
        PermissionController.createRequestPermissionResultContract()
            as ActivityResultContract<Set<String>, Set<String>>,
    ) { refresh() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_sync)
        card = HealthConnectCard(findViewById(R.id.sync_health_connect_section), permissionLauncher::launch, ::switched)
        accountNotice = findViewById(R.id.sync_account_notice)
        recorded = findViewById(R.id.sync_recorded_section)
        openList = findViewById(R.id.sync_open_list)
        openList.setOnClickListener {
            MainActivity.openTab(this, MainActivity.Tab.SYNC)
            finish()
        }
        // Health Connect asked "why does this app want my data" — answer it straight away, rather
        // than making the user find the info button. Only on a fresh start, so a rotation after
        // dismissing it doesn't bring it back.
        if (savedInstanceState == null && intent?.action in RATIONALE_ACTIONS) card.showRationale()
    }

    /** Health Connect's routes can't be read in the background: a first load cut off here runs
     *  again on whichever screen is next in front. */
    override fun onStop() {
        super.onStop()
        firstLoad?.cancel()
        firstLoad = null
    }

    override fun onResume() {
        super.onResume()
        // Re-read on every resume: the routes permission is granted in another app, so returning
        // from it is the only moment this screen can learn that it changed.
        refresh()
    }

    private fun sources() = SyncSources(this, Session.email)

    /** Switched on with nothing allowed yet, Health Connect's dialog opens at once: sessions and
     *  history together; and once it can be read, it's read once ([firstLoadIfDue]). Switched
     *  off, what was read from it is no longer listed. */
    private fun switched(on: Boolean) {
        val sources = sources()
        sources.healthConnect = on
        sources.firstLoadPending = on
        if (!on) {
            firstLoad?.cancel()
            firstLoad = null
            card.showFirstLoad(null)
        }
        lifecycleScope.launch {
            if (!on) WaitingStore(this@SyncActivity).clear()
            val access = HealthConnect.access(this@SyncActivity)
            if (on && access.readiness == HealthConnect.Readiness.NEEDS_EXERCISE_PERMISSION) card.requestNext(access.readiness)
            render(access)
        }
    }

    private fun refresh() {
        lifecycleScope.launch { render(HealthConnect.access(this@SyncActivity)) }
    }

    private fun render(access: HealthConnect.Access) {
        val block = when {
            !Session.isSignedIn -> getString(R.string.sync_needs_account)
            // The server refuses an unconfirmed account's sync (requireVerified).
            !Session.emailVerified -> getString(R.string.sync_needs_verified_email)
            Session.isDemo -> getString(R.string.sync_demo_read_only)
            else -> null
        }
        accountNotice.text = block
        accountNotice.visibility = if (block == null) View.GONE else View.VISIBLE
        card.visible = block == null
        recorded.visibility = if (block == null) View.VISIBLE else View.GONE
        openList.visibility = if (block == null) View.VISIBLE else View.GONE
        if (block == null) card.render(sources().healthConnect, access)
        if (block == null) firstLoadIfDue(access.readiness)
        if (block == null) renderRecorded()
    }

    /** The record button's card: green once it has what a recording needs — precise location and
     *  the notification it runs under — which the button asks for on its first tap, not here. */
    private fun renderRecorded() {
        val allowed = RECORDING_PERMISSIONS.all { ContextCompat.checkSelfPermission(this, it) == PackageManager.PERMISSION_GRANTED }
        findViewById<ImageView>(R.id.sync_recorded_icon).imageTintList =
            ColorStateList.valueOf(getColor(if (allowed) R.color.hmt_success else R.color.hmt_ink_secondary))
        findViewById<TextView>(R.id.sync_recorded_status).apply {
            setText(if (allowed) R.string.sync_recorded_allowed else R.string.sync_recorded_not_yet)
            setTextColor(getColor(if (allowed) R.color.hmt_success else R.color.hmt_ink_secondary))
        }
    }

    /** Switched on and readable, and not read yet: read now, on this screen, rather than when
     *  the Sync tab next opens. */
    private fun firstLoadIfDue(readiness: HealthConnect.Readiness) {
        val sources = sources()
        if (firstLoad != null || !sources.healthConnect || !sources.firstLoadPending || !readiness.isReadable()) return
        card.showFirstLoad(getString(R.string.sync_loading_health_connect))
        firstLoad = lifecycleScope.launch {
            val shown = when (val result = WaitingLoader(this@SyncActivity).load()) {
                is WaitingLoader.Result.Loaded ->
                    resources.getQuantityString(R.plurals.sync_first_load_found, result.sessions.size, result.sessions.size)
                is WaitingLoader.Result.Failed -> result.reason
                WaitingLoader.Result.Unreadable -> null
            }
            card.showFirstLoad(shown)
            firstLoad = null
        }
    }

    private companion object {
        /** What the record button asks for before its first recording (`MapFragment.onRecordTap`). */
        val RECORDING_PERMISSIONS = listOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.POST_NOTIFICATIONS)

        /** Health Connect's rationale request, and its "see how this app used your data" link
         *  (the manifest's `ViewPermissionUsageActivity` alias). */
        val RATIONALE_ACTIONS = setOf(
            "androidx.health.ACTION_SHOW_PERMISSIONS_RATIONALE",
            Intent.ACTION_VIEW_PERMISSION_USAGE,
        )
    }
}
