package dev.holdmytrack.android.sync

import android.content.Context
import android.util.Log
import android.view.View
import android.widget.Button
import android.widget.TextView
import androidx.browser.customtabs.CustomTabsIntent
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import dev.holdmytrack.android.R
import dev.holdmytrack.android.health.HealthConnect
import dev.holdmytrack.android.net.HoldMyTrackApi

/**
 * Health Connect's setup (`include_health_connect_card`): where it stands — a dot, a title and
 * the line under it — and its one remaining step as the card's button. What is read and what it
 * is for sits behind the info button beside the title ([showRationale]) — the same text Health
 * Connect asks for as this app's permission rationale (`SyncActivity`), since a rationale that
 * said something different from the app's own explanation would be the wrong kind of surprise.
 * Under the Sync tab's list (`panel/SyncTab`), and the whole of `SyncActivity`.
 *
 * **Granting access is two flows, not one, and the second cannot be automated.** Reading
 * sessions is an ordinary runtime permission. Reading their routes is not requestable at all:
 * measured on a device, asking for it simply returns without it, and the user has to grant it
 * inside Health Connect, on a screen two levels below the app's own permission page and not
 * linked from it. So this card names the taps rather than saying "grant permissions".
 */
class HealthConnectCard(
    private val root: View,
    /** Asks for Health Connect permissions — the host's registered launcher. */
    private val requestPermissions: (Set<String>) -> Unit,
) {
    private val context: Context = root.context
    private val statusDot: View = root.findViewById(R.id.sync_status_dot)
    private val statusTitle: TextView = root.findViewById(R.id.sync_status_title)
    private val status: TextView = root.findViewById(R.id.sync_status)
    private val instructions: TextView = root.findViewById(R.id.sync_instructions)
    private val primary: Button = root.findViewById(R.id.sync_primary)

    init {
        root.findViewById<Button>(R.id.sync_health_connect_info).setOnClickListener { showRationale() }
    }

    var visible: Boolean
        get() = root.visibility == View.VISIBLE
        set(shown) {
            root.visibility = if (shown) View.VISIBLE else View.GONE
        }

    /** What is read from Health Connect and what it is for — the permission rationale — with
     *  the privacy policy a button away: Health Connect requires the policy this leads to to be
     *  the one on the Play listing, and both are the web's /privacy. */
    fun showRationale() {
        MaterialAlertDialogBuilder(context)
            .setTitle(R.string.sync_health_connect_info)
            .setMessage(R.string.sync_rationale)
            .setPositiveButton(android.R.string.ok, null)
            .setNeutralButton(R.string.privacy_policy) { _, _ ->
                CustomTabsIntent.Builder().build().launchUrl(context, HoldMyTrackApi.webPageUri("/privacy"))
            }
            .show()
    }

    /** Where [readiness] stands, and its one remaining step. */
    fun render(readiness: HealthConnect.Readiness) {
        primary.visibility = View.VISIBLE
        instructions.visibility = View.GONE
        val (title, dot) = when (readiness) {
            HealthConnect.Readiness.READY -> R.string.sync_hc_ready to R.color.hmt_success
            HealthConnect.Readiness.NEEDS_HISTORY_PERMISSION -> R.string.sync_hc_ready_30_days to R.color.hmt_success
            HealthConnect.Readiness.UNAVAILABLE -> R.string.sync_hc_unavailable to R.color.hmt_ink_faint
            HealthConnect.Readiness.UPDATE_REQUIRED -> R.string.sync_hc_update to R.color.hmt_accent
            else -> R.string.sync_hc_setup to R.color.hmt_accent
        }
        statusTitle.setText(title)
        statusDot.backgroundTintList = context.getColorStateList(dot)

        when (readiness) {
            HealthConnect.Readiness.UNAVAILABLE -> {
                showStatus(context.getString(R.string.sync_unavailable))
                primary.visibility = View.GONE
            }
            HealthConnect.Readiness.UPDATE_REQUIRED -> {
                showStatus(context.getString(R.string.sync_update_required))
                primary.setText(R.string.sync_open_health_connect)
                primary.setOnClickListener { openSettings() }
            }
            HealthConnect.Readiness.NEEDS_EXERCISE_PERMISSION -> {
                showStatus(context.getString(R.string.sync_needs_exercise_permission))
                primary.setText(R.string.sync_allow_access)
                primary.setOnClickListener {
                    requestPermissions(setOf(HealthConnect.READ_EXERCISE, HealthConnect.READ_HISTORY))
                }
            }
            HealthConnect.Readiness.NEEDS_ROUTES_PERMISSION -> {
                showStatus(context.getString(R.string.sync_needs_routes_permission))
                // The one place this app spells out another app's menu path. It is not
                // hand-holding: on a device the screen proved is two levels down and unlinked, so a
                // user sent to Health Connect without it has no reason to find it.
                instructions.setText(R.string.sync_routes_steps)
                instructions.visibility = View.VISIBLE
                primary.setText(R.string.sync_open_health_connect)
                primary.setOnClickListener { openSettings() }
            }
            HealthConnect.Readiness.NEEDS_HISTORY_PERMISSION -> {
                showStatus(context.getString(R.string.sync_hc_reads))
                instructions.setText(R.string.sync_history_hint)
                instructions.visibility = View.VISIBLE
                // Unlike the routes permission, this one *is* requestable, so the app asks rather
                // than sending the user off to find a toggle. The list still works without it —
                // it just can't reach back as far — and declining a permission is a real answer.
                primary.setText(R.string.sync_allow_history)
                primary.setOnClickListener { requestPermissions(setOf(HealthConnect.READ_HISTORY)) }
            }
            HealthConnect.Readiness.READY -> {
                showStatus(context.getString(R.string.sync_hc_reads))
                primary.visibility = View.GONE
            }
        }
    }

    /** Where Health Connect stands, said in full under its title — in the danger color when
     *  it's a failure. */
    private fun showStatus(text: CharSequence, failed: Boolean = false) {
        status.text = text
        status.setTextColor(context.getColor(if (failed) R.color.hmt_danger else R.color.hmt_ink_secondary))
    }

    /**
     * Catches broadly rather than just `ActivityNotFoundException`, because the failure that
     * actually happened here was a `SecurityException`: an earlier version aimed at the
     * platform's per-app permission screen, which is guarded by a signature permission and takes
     * the whole process down when an ordinary app launches it. A button that cannot open a screen
     * should say so, not crash the app around it.
     */
    private fun openSettings() {
        try {
            context.startActivity(HealthConnect.settingsIntent())
        } catch (e: Exception) {
            Log.w(TAG, "could not open Health Connect", e)
            showStatus(context.getString(R.string.sync_unavailable), failed = true)
        }
    }

    companion object {
        private const val TAG = "HoldMyTrackSync"

        /** Health Connect can be read — fully, or the last 30 days of it. */
        fun HealthConnect.Readiness.isReadable() =
            this == HealthConnect.Readiness.READY || this == HealthConnect.Readiness.NEEDS_HISTORY_PERMISSION
    }
}
