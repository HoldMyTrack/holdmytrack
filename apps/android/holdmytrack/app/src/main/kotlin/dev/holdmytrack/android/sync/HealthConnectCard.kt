package dev.holdmytrack.android.sync

import android.content.Context
import android.content.res.ColorStateList
import android.util.Log
import android.view.View
import android.widget.Button
import android.widget.TextView
import androidx.browser.customtabs.CustomTabsIntent
import androidx.core.widget.TextViewCompat
import com.google.android.material.materialswitch.MaterialSwitch
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import dev.holdmytrack.android.R
import dev.holdmytrack.android.health.HealthConnect
import dev.holdmytrack.android.net.HoldMyTrackApi

/**
 * Health Connect on Settings › Sync (`include_health_connect_card`, `SyncActivity`): its switch —
 * off until the user turns it on ([SyncSources]) — where it stands, a dot, a title and the line
 * under it, and, while it's on, its three permissions as a checklist with the one remaining step
 * as the card's button. What is read and what it is for sits behind the info button beside the
 * title ([showRationale]) — the same text Health Connect asks for as this app's permission
 * rationale, since a rationale that said something different from the app's own explanation
 * would be the wrong kind of surprise.
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
    /** The switch, flipped by the user. */
    private val onSwitched: (on: Boolean) -> Unit,
) {
    private val context: Context = root.context
    private val statusDot: View = root.findViewById(R.id.sync_status_dot)
    private val statusTitle: TextView = root.findViewById(R.id.sync_status_title)
    private val status: TextView = root.findViewById(R.id.sync_status)
    private val instructions: TextView = root.findViewById(R.id.sync_instructions)
    private val primary: Button = root.findViewById(R.id.sync_primary)
    private val switch: MaterialSwitch = root.findViewById(R.id.sync_health_connect_switch)
    private val checklist: View = root.findViewById(R.id.sync_checklist)
    private val firstLoad: TextView = root.findViewById(R.id.sync_first_load)
    /** [render] setting the switch, which isn't the user flipping it. */
    private var rendering = false
    private val checks: List<Pair<TextView, String>> = listOf(
        root.findViewById<TextView>(R.id.sync_check_sessions) to HealthConnect.READ_EXERCISE,
        root.findViewById<TextView>(R.id.sync_check_routes) to HealthConnect.READ_EXERCISE_ROUTES,
        root.findViewById<TextView>(R.id.sync_check_history) to HealthConnect.READ_HISTORY,
    )

    init {
        root.findViewById<Button>(R.id.sync_health_connect_info).setOnClickListener { showRationale() }
        switch.setOnCheckedChangeListener { _, on -> if (!rendering) onSwitched(on) }
    }

    /** Asks for whatever's next that can be asked for here — sessions and history together, or
     *  history alone. The routes permission can't be asked for; the card says where it is. */
    fun requestNext(readiness: HealthConnect.Readiness) {
        when (readiness) {
            HealthConnect.Readiness.NEEDS_EXERCISE_PERMISSION -> requestPermissions(setOf(HealthConnect.READ_EXERCISE, HealthConnect.READ_HISTORY))
            HealthConnect.Readiness.NEEDS_HISTORY_PERMISSION -> requestPermissions(setOf(HealthConnect.READ_HISTORY))
            else -> Unit
        }
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

    /** Where Health Connect stands: off, or — switched [on] — [access]'s permissions and the one
     *  remaining step. With no Health Connect on the phone the switch has nothing to turn on. */
    fun render(on: Boolean, access: HealthConnect.Access) {
        val readiness = access.readiness
        val available = readiness != HealthConnect.Readiness.UNAVAILABLE
        switch.isEnabled = available
        rendering = true
        switch.isChecked = on && available
        rendering = false
        if (!on || !available) {
            renderOff(readiness)
            return
        }
        checklist.visibility = if (readiness == HealthConnect.Readiness.UPDATE_REQUIRED) View.GONE else View.VISIBLE
        renderChecklist(access.granted)
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
                primary.setOnClickListener { requestNext(readiness) }
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
                showStatus(context.getString(R.string.sync_hc_listed))
                instructions.setText(R.string.sync_history_hint)
                instructions.visibility = View.VISIBLE
                // Unlike the routes permission, this one *is* requestable, so the app asks rather
                // than sending the user off to find a toggle. The list still works without it —
                // it just can't reach back as far — and declining a permission is a real answer.
                primary.setText(R.string.sync_allow_history)
                primary.setOnClickListener { requestNext(readiness) }
            }
            HealthConnect.Readiness.READY -> {
                showStatus(context.getString(R.string.sync_hc_listed))
                primary.visibility = View.GONE
            }
        }
    }

    /** The first load after switching on (`SyncActivity`): what it's doing or found; null hides
     *  it. */
    fun showFirstLoad(text: CharSequence?) {
        firstLoad.text = text
        firstLoad.visibility = if (text == null) View.GONE else View.VISIBLE
    }

    /** Switched off, or nothing to switch on: nothing is read, and there's nothing to set up. */
    private fun renderOff(readiness: HealthConnect.Readiness) {
        showFirstLoad(null)
        checklist.visibility = View.GONE
        instructions.visibility = View.GONE
        primary.visibility = View.GONE
        if (readiness == HealthConnect.Readiness.UNAVAILABLE) {
            statusTitle.setText(R.string.sync_hc_unavailable)
            showStatus(context.getString(R.string.sync_unavailable))
        } else {
            statusTitle.setText(R.string.sync_hc_off)
            showStatus(context.getString(R.string.sync_hc_off_status))
        }
        statusDot.backgroundTintList = context.getColorStateList(R.color.hmt_ink_faint)
    }

    /** Each permission ticked once Health Connect has granted it. */
    private fun renderChecklist(granted: Set<String>) {
        for ((row, permission) in checks) {
            val allowed = permission in granted
            row.setCompoundDrawablesRelativeWithIntrinsicBounds(if (allowed) R.drawable.ic_check else R.drawable.ic_circle_dot, 0, 0, 0)
            TextViewCompat.setCompoundDrawableTintList(
                row,
                ColorStateList.valueOf(context.getColor(if (allowed) R.color.hmt_success else R.color.hmt_ink_faint)),
            )
            row.setTextColor(context.getColor(if (allowed) R.color.hmt_ink else R.color.hmt_ink_secondary))
            row.contentDescription = context.getString(
                if (allowed) R.string.sync_check_allowed else R.string.sync_check_missing,
                row.text,
            )
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
