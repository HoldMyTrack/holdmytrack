package dev.holdmytrack.android.sync

import android.content.Context
import android.content.SharedPreferences

/**
 * Which sources the Sync Source screen's "Sync now" includes, beyond the recordings the user
 * checked row by row (those keep their own per-row status in `RecordedActivityStore`). Today
 * that is Health Connect alone: on by default, and an untick sticks until the user ticks it
 * again, so a user who only wants to send a recording isn't made to re-untick it every visit.
 *
 * Kept per account, like `SyncCursor` — one person's choice is not another's on a shared
 * device.
 */
class SyncSources(context: Context, account: String) {

    private val prefs: SharedPreferences =
        context.applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    private val healthConnectKey = "healthconnect.$account"

    var healthConnect: Boolean
        get() = prefs.getBoolean(healthConnectKey, true)
        set(value) = prefs.edit().putBoolean(healthConnectKey, value).apply()

    private companion object {
        const val PREFS_NAME = "holdmytrack.sync_sources"
    }
}
