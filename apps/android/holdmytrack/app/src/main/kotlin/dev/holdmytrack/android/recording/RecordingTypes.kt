package dev.holdmytrack.android.recording

import android.content.Context
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.db.RecordedActivityRecord

/**
 * The activity-type field is free text — a custom type is the whole point — but the Type
 * picker (`ActivityTypePicker`) still lists these five even on an account that has never used
 * them (`apps/android/docs/ROADMAP.md` Phase 7's own walk/hike/run/ride/drive default set), so
 * most recordings stay on the shared vocabulary `health/ExerciseTypes.kt` already uses, rather
 * than fragmenting the Activities panel's TYPE facet or cross-source deduplication
 * (`docs/IMPLEMENTATION.md` §4.6) with one-off spellings. Values are the exact wire strings
 * `internal/parse/fit_sport.go` already recognizes server-side, so a preset picked here lands
 * identically to a `.FIT`-uploaded one.
 */
object RecordingTypes {
    val PRESETS: List<String> = listOf("walking", "hiking", "running", "cycling", "driving")

    /** The "no type yet" value — never blank: an untyped activity is still something, and
     *  this is the same fallback `internal/parse/parse.go`'s `ParseJSON` already applies
     *  server-side to an empty `activity_type`. */
    const val DEFAULT = "unknown"

    private const val PREFS_NAME = "holdmytrack.recording"
    private const val KEY_LAST_TYPE_PREFIX = "last_type:"

    /**
     * What a fresh recording starts with: the type this account last gave a recording in Edit,
     * or [DEFAULT] if it never has. Recording asks nothing, so this is how most recordings
     * arrive already typed — and so already queueable past `RecordedActivitiesActivity`'s
     * "unknown" gate. Kept in its own preference rather than read off the newest row, because
     * a synced row is deleted from the device and the store can be empty. Scoped per account
     * by [Session.email], the same key `RecordedActivityStore` uses.
     */
    fun lastUsed(context: Context): String =
        prefs(context).getString(KEY_LAST_TYPE_PREFIX + Session.email, null) ?: DEFAULT

    /** Called on every Edit save; [DEFAULT] is never remembered — clearing a type back to
     *  "unknown" is a correction to one row, not a new preference. */
    fun rememberLastUsed(context: Context, type: String) {
        if (type == DEFAULT) return
        prefs(context).edit().putString(KEY_LAST_TYPE_PREFIX + Session.email, type).apply()
    }

    private fun prefs(context: Context) =
        context.applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    /** `formatActivityType` in `apps/web/src/ui/format.ts`, the same behavior: spaces out
     *  `snake_case` and title-cases each word, without remapping what the value means. */
    fun format(type: String): String =
        type.split('_').filter { it.isNotEmpty() }.joinToString(" ") { it.replaceFirstChar(Char::uppercaseChar) }

    /**
     * What the Type picker lists: every type the account's server-side activities use, plus
     * this device's recordings (every one of them unsynced — a synced row is deleted from the
     * device, and is counted in [server] instead), most-used first, then any [PRESETS] not already present. [server] is empty
     * when it couldn't be read — offline, say — which still leaves a usable list.
     * [DEFAULT] is left out: it is the "no type yet" placeholder, not a choice worth offering.
     */
    fun known(server: Map<String, Int>, local: List<RecordedActivityRecord>): List<TypeCount> {
        val counts = server.toMutableMap()
        for (record in local) counts.merge(record.activityType, 1, Int::plus)
        counts.remove(DEFAULT)
        val used = counts.entries.sortedByDescending { it.value }.map { TypeCount(it.key, it.value) }
        return used + PRESETS.filter { it !in counts }.map { TypeCount(it, 0) }
    }
}
