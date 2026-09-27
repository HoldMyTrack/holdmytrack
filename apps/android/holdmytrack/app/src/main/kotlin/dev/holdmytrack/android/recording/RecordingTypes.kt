package dev.holdmytrack.android.recording

import android.content.Context
import android.content.res.Resources
import dev.holdmytrack.android.R
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
     * arrive already typed. Kept in its own preference rather than read off the newest row, because
     * a synced row is deleted from the device and the store can be empty. Scoped per account
     * by [Session.email], the same key `RecordedActivityStore` uses.
     */
    fun lastUsed(context: Context, account: String = Session.email): String =
        prefs(context).getString(KEY_LAST_TYPE_PREFIX + account, null) ?: DEFAULT

    /** Called on every Edit save; [DEFAULT] is never remembered — clearing a type back to
     *  "unknown" is a correction to one row, not a new preference. */
    fun rememberLastUsed(context: Context, type: String) {
        if (type == DEFAULT) return
        prefs(context).edit().putString(KEY_LAST_TYPE_PREFIX + Session.email, type).apply()
    }

    private fun prefs(context: Context) =
        context.applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    /** `formatActivityType` in `apps/web/src/ui/format.ts`, the same behavior: a common
     *  type's display name in the app's language (the web's `activity_type.*` catalog, as
     *  `activity_type_*` strings), matched case-insensitively; any other type spaced out from
     *  `snake_case` and title-cased, without remapping what the value means. */
    fun format(res: Resources, type: String): String {
        LABELS[type.lowercase()]?.let { return res.getString(it) }
        return type.split('_').filter { it.isNotEmpty() }.joinToString(" ") { it.replaceFirstChar(Char::uppercaseChar) }
    }

    private val LABELS: Map<String, Int> = mapOf(
        "alpine_skiing" to R.string.activity_type_alpine_skiing,
        "biking" to R.string.activity_type_biking,
        "biking_stationary" to R.string.activity_type_biking_stationary,
        "cross_country_skiing" to R.string.activity_type_cross_country_skiing,
        "cycling" to R.string.activity_type_cycling,
        "driving" to R.string.activity_type_driving,
        "e_biking" to R.string.activity_type_e_biking,
        "generic" to R.string.activity_type_generic,
        "golf" to R.string.activity_type_golf,
        "gravel_cycling" to R.string.activity_type_gravel_cycling,
        "hiking" to R.string.activity_type_hiking,
        "horseback_riding" to R.string.activity_type_horseback_riding,
        "ice_skating" to R.string.activity_type_ice_skating,
        "inline_skating" to R.string.activity_type_inline_skating,
        "kayaking" to R.string.activity_type_kayaking,
        "motorcycling" to R.string.activity_type_motorcycling,
        "mountain_biking" to R.string.activity_type_mountain_biking,
        "other_workout" to R.string.activity_type_other_workout,
        "paddling" to R.string.activity_type_paddling,
        "road_cycling" to R.string.activity_type_road_cycling,
        "rock_climbing" to R.string.activity_type_rock_climbing,
        "rowing" to R.string.activity_type_rowing,
        "rowing_machine" to R.string.activity_type_rowing_machine,
        "running" to R.string.activity_type_running,
        "running_treadmill" to R.string.activity_type_running_treadmill,
        "sailing" to R.string.activity_type_sailing,
        "skating" to R.string.activity_type_skating,
        "skiing" to R.string.activity_type_skiing,
        "snowboarding" to R.string.activity_type_snowboarding,
        "stand_up_paddleboarding" to R.string.activity_type_stand_up_paddleboarding,
        "surfing" to R.string.activity_type_surfing,
        "swimming" to R.string.activity_type_swimming,
        "swimming_open_water" to R.string.activity_type_swimming_open_water,
        "swimming_pool" to R.string.activity_type_swimming_pool,
        "tennis" to R.string.activity_type_tennis,
        "trail_running" to R.string.activity_type_trail_running,
        "unknown" to R.string.activity_type_unknown,
        "walk" to R.string.activity_type_walk,
        "walking" to R.string.activity_type_walking,
        "yoga" to R.string.activity_type_yoga,
    )

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
