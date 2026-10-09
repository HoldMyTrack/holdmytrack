package dev.holdmytrack.android.sync

import android.content.Context
import android.content.SharedPreferences

/**
 * The Sync tab's hidden rows (`docs/SPEC.md` FR-3.6): candidates the user swiped aside, by
 * [Candidate.key], kept on this phone only. Per account, as everything the phone keeps for an
 * account is — two people on one phone each set aside their own; the demo account has no email
 * and shares the empty key, which is harmless.
 */
class HiddenCandidates(context: Context, account: String) {

    private val prefs: SharedPreferences =
        context.applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    private val key = "hidden.$account"

    fun all(): Set<String> = prefs.getStringSet(key, emptySet()).orEmpty().toSet()

    fun hide(candidateKey: String) = write(all() + candidateKey)

    fun unhide(candidateKey: String) = write(all() - candidateKey)

    /** Keeps only the [present] keys: a hidden recording deleted or synced, or a session that has
     *  aged out of the window, needn't be remembered. */
    fun retainOnly(present: Set<String>) {
        val current = all()
        val kept = current intersect present
        if (kept.size != current.size) write(kept)
    }

    fun clear() {
        prefs.edit().remove(key).apply()
    }

    private fun write(keys: Set<String>) {
        prefs.edit().putStringSet(key, keys).apply()
    }

    private companion object {
        const val PREFS_NAME = "holdmytrack.sync"
    }
}
