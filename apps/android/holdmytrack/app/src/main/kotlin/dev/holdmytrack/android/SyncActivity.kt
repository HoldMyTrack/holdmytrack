package dev.holdmytrack.android

import android.content.Intent
import android.os.Bundle
import android.view.View
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContract
import androidx.appcompat.app.AppCompatActivity
import androidx.health.connect.client.PermissionController
import androidx.lifecycle.lifecycleScope
import dev.holdmytrack.android.health.HealthConnect
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.sync.HealthConnectCard
import kotlinx.coroutines.launch

/**
 * Health Connect's setup on its own ([HealthConnectCard]), for Health Connect: the screen it opens
 * as this app's permission *rationale*, and from "see how this app used your data". It opens
 * even signed out, which the app's own Sync tab (`MainActivity`, `panel/SyncTab`) never is.
 * Launched to answer exactly the rationale question, it opens the rationale straight away; for a
 * signed-in account a button leads on to the Sync tab, where what's on the phone is chosen.
 */
class SyncActivity : AppCompatActivity() {

    private lateinit var card: HealthConnectCard
    private lateinit var accountNotice: TextView
    private lateinit var openList: View

    private val permissionLauncher = registerForActivityResult(
        @Suppress("UNCHECKED_CAST")
        PermissionController.createRequestPermissionResultContract()
            as ActivityResultContract<Set<String>, Set<String>>,
    ) { refresh() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_sync)
        card = HealthConnectCard(findViewById(R.id.sync_health_connect_section), permissionLauncher::launch)
        accountNotice = findViewById(R.id.sync_account_notice)
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

    override fun onResume() {
        super.onResume()
        // Re-read on every resume: the routes permission is granted in another app, so returning
        // from it is the only moment this screen can learn that it changed.
        refresh()
    }

    private fun refresh() {
        lifecycleScope.launch { render(HealthConnect.readiness(this@SyncActivity)) }
    }

    private fun render(readiness: HealthConnect.Readiness) {
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
        openList.visibility = if (block == null) View.VISIBLE else View.GONE
        if (block == null) card.render(readiness)
    }

    private companion object {
        /** Health Connect's rationale request, and its "see how this app used your data" link
         *  (the manifest's `ViewPermissionUsageActivity` alias). */
        val RATIONALE_ACTIONS = setOf(
            "androidx.health.ACTION_SHOW_PERMISSIONS_RATIONALE",
            Intent.ACTION_VIEW_PERMISSION_USAGE,
        )
    }
}
