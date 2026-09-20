package dev.fitmap.android.recording

/**
 * The activity-type field is free text — a custom type is the whole point — but still offers
 * these five as tap-to-fill suggestions (`apps/android/docs/ROADMAP.md` Phase 7's own
 * walk/hike/run/ride/drive default set) so most recordings stay on the shared vocabulary
 * `health/ExerciseTypes.kt` already uses, rather than fragmenting the Activities panel's TYPE
 * facet or cross-source deduplication (`docs/IMPLEMENTATION.md` §4.6) with one-off spellings.
 * Values are the exact wire strings `internal/parse/fit_sport.go` already recognizes
 * server-side, so a suggestion tapped here lands identically to a `.FIT`-uploaded one.
 */
object RecordingTypes {
    val PRESETS: List<String> = listOf("walking", "hiking", "running", "cycling", "driving")

    /** What a fresh recording starts with — never blank: an untyped activity is still
     *  something, and this is the same fallback `internal/parse/parse.go`'s `ParseJSON`
     *  already applies server-side to an empty `activity_type`. */
    const val DEFAULT = "unknown"
}
