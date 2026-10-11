package dev.holdmytrack.android.sync

import android.content.Context
import android.content.SharedPreferences

/**
 * Which sources Settings › Sync has switched on (`apps/android/docs/SPEC.md` FR-3.1), kept on this
 * phone per account, as [HiddenCandidates] is and in the same file. Only Health Connect has a
 * switch — recordings are the app's own and always listed — and it starts **off**, for a new
 * install and for one that had Health Connect's permissions before there was a switch: a source
 * is read only once the user has turned it on (ADR-0039).
 */
class SyncSources(context: Context, account: String) {

    private val prefs: SharedPreferences =
        context.applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    private val healthConnectKey = "hc_enabled.$account"
    private val firstLoadKey = "hc_first_load.$account"

    var healthConnect: Boolean
        get() = prefs.getBoolean(healthConnectKey, false)
        set(on) {
            prefs.edit().putBoolean(healthConnectKey, on).apply()
        }

    /** Switched on and not read yet: the one load that runs without Load being tapped
     *  ([WaitingLoader]), on whichever of Settings › Sync and the Sync tab is in front once Health
     *  Connect can be read. Cleared by a load that finishes, and by switching off. */
    var firstLoadPending: Boolean
        get() = prefs.getBoolean(firstLoadKey, false)
        set(pending) {
            prefs.edit().putBoolean(firstLoadKey, pending).apply()
        }

    fun clear() {
        prefs.edit().remove(healthConnectKey).remove(firstLoadKey).apply()
    }

    private companion object {
        /** [HiddenCandidates]' file. */
        const val PREFS_NAME = "holdmytrack.sync"
    }
}