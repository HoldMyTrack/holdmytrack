package dev.fitmap.android.recording

/**
 * The activity types a recording can be labeled with — a small default set (walk/hike/run/
 * ride/drive, `apps/android/docs/ROADMAP.md` Phase 7's own suggestion) rather than Health
 * Connect's full sport list, since there's no platform exercise-type field to read here the
 * way Path 2 sync has one.
 *
 * Values are `health/ExerciseTypes.kt`'s own output vocabulary, not a second one invented for
 * this screen — same strings `internal/parse/fit_sport.go` already recognizes server-side, so
 * a recorded walk and a `.FIT`-uploaded walk land as the same `activity_type` and neither
 * fragments the Activities panel's TYPE facet nor defeats cross-source deduplication
 * (`docs/IMPLEMENTATION.md` §4.6).
 */
object RecordingTypes {

    /** Display label to the wire `activity_type` value, in the order they're offered. */
    val OPTIONS: List<Pair<String, String>> = listOf(
        "Walk" to "walking",
        "Hike" to "hiking",
        "Run" to "running",
        "Ride" to "cycling",
        "Drive" to "driving",
    )

    val LABELS: List<String> = OPTIONS.map { it.first }

    fun wireValue(labelIndex: Int): String = OPTIONS[labelIndex].second
}
