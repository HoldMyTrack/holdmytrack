package dev.fitmap.android.sync

import android.content.Context
import android.content.SharedPreferences
import java.time.Instant

/**
 * The sync watermark: how far through the Health Connect store this account has been read.
 *
 * **This is the correctness requirement that makes foreground-only sync viable at all**
 * (`docs/IMPLEMENTATION.md` §4.0). The trap it exists to avoid is named there: a sync that
 * advances past records whose routes were never read produces a history "complete in every
 * respect except the map" — worse than no sync, because nothing afterwards knows to go back
 * for them. So the rule this class enforces is narrow and absolute: the cursor only ever moves
 * to a record whose outcome is **terminal** — either the server confirmed it, or it can never
 * have a route to fetch. A route that merely could not be read right now stops it dead, and
 * `SyncRunner` is the code that decides which is which.
 *
 * Simple "don't double-submit" bookkeeping would not be enough, and the server's idempotency on
 * `(user_id, source, external_id)` does not substitute for it either: idempotency stops a
 * record arriving twice, it has no opinion about a record that never arrived.
 *
 * **[at] alone is not a complete position**, which is why [handledAtCursor] exists. Health
 * Connect's `TimeRangeFilter.after` is inclusive of its instant, so the record the cursor
 * stands on is read again on the next run — and two sessions can genuinely share a start
 * instant, so simply excluding that instant would lose a sibling. Naming the ids already dealt
 * with at exactly that instant is what lets the next run skip those and nothing else.
 *
 * Kept per account: signing into a different account must not inherit a position through
 * someone else's history, and signing back into the same one should resume rather than re-walk
 * it. A demo account has no email and shares the empty key, which is harmless — its data is
 * deleted within the day either way.
 */
class SyncCursor(context: Context, account: String) {

    private val prefs: SharedPreferences =
        context.applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    private val atKey = "at.$account"
    private val idsKey = "ids.$account"

    /** Null when this account has never been synced — the first run reads from the beginning. */
    val at: Instant?
        get() = prefs.getLong(atKey, NEVER).takeIf { it != NEVER }?.let(Instant::ofEpochMilli)

    /** Record ids already dealt with at exactly [at] — see the class comment. */
    val handledAtCursor: Set<String>
        get() = prefs.getStringSet(idsKey, emptySet()).orEmpty()

    /**
     * Moves the cursor to [instant], having dealt with [idsAtInstant] there. Never call this
     * for a record whose route could not be read — that is the whole point of the class.
     *
     * Refuses to move backwards. A run that read fewer records than a previous one (a narrower
     * time filter, a store that dropped history) must not re-open a window already closed.
     */
    fun advanceTo(instant: Instant, idsAtInstant: Set<String>) {
        val current = at
        if (current != null && instant.isBefore(current)) return
        // Staying put rather than moving means the ids already recorded here are still ids
        // dealt with here, so they are kept rather than replaced. Two sessions sharing one
        // start instant can land in different batches within a run, and dropping the earlier
        // one would leave it to be read and re-sent on every run from then on.
        val ids = if (instant == current) handledAtCursor + idsAtInstant else idsAtInstant
        prefs.edit()
            .putLong(atKey, instant.toEpochMilli())
            .putStringSet(idsKey, ids)
            .apply()
    }

    /** Back to the beginning. The server's idempotency makes a re-walk cost traffic, not rows. */
    fun reset() {
        prefs.edit().remove(atKey).remove(idsKey).apply()
    }

    private companion object {
        const val PREFS_NAME = "fitmap.sync"
        const val NEVER = Long.MIN_VALUE
    }
}
