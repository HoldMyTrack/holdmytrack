package dev.fitmap.android.health

import androidx.health.connect.client.records.ExerciseSessionRecord

/**
 * Health Connect's exercise type (an `Int`) turned into the `activity_type` string
 * `POST /v1/sync/activities` takes.
 *
 * **Written against the library's public constants, deliberately.** The client also ships an
 * `EXERCISE_TYPE_INT_TO_STRING_MAP` that would do most of this for free, but it is annotated
 * `@RestrictTo` — library-group internal, removable without a semantic-version bump — so the
 * table is spelled out here instead. Every left-hand side is a named constant rather than a
 * bare integer, which is what keeps a transcription slip a compile error instead of a silently
 * mislabelled activity; `internal/parse/fit_sport.go` avoids the same hazard the other way,
 * by generating its own enum names from a verified source.
 *
 * **The names are not Health Connect's own everywhere, and that matters.** HoldMyTrack's existing
 * ingest paths speak the FIT vocabulary — a bike ride uploaded as a `.FIT` file lands as
 * `cycling` — while Health Connect calls the same thing `biking`. Left alone, one ride synced
 * from a watch and uploaded from a file would be two different activity types, which splits
 * the Activities panel's TYPE facets and breaks cross-source deduplication, whose `dedupe_key`
 * includes the activity type (`docs/IMPLEMENTATION.md` §4.6). The handful of renames marked
 * below are what keep the two sources describing the same thing the same way.
 *
 * Indoor types are here for completeness rather than need: a session with no route never
 * reaches the payload at all (`sync/SyncRunner.kt`), so in practice only the outdoor entries
 * are ever read.
 */
object ExerciseTypes {

    private val NAMES = mapOf(
        ExerciseSessionRecord.EXERCISE_TYPE_OTHER_WORKOUT to "other_workout",
        ExerciseSessionRecord.EXERCISE_TYPE_BADMINTON to "badminton",
        ExerciseSessionRecord.EXERCISE_TYPE_BASEBALL to "baseball",
        ExerciseSessionRecord.EXERCISE_TYPE_BASKETBALL to "basketball",
        ExerciseSessionRecord.EXERCISE_TYPE_BIKING to "cycling",  // Health Connect calls this "biking"
        ExerciseSessionRecord.EXERCISE_TYPE_BIKING_STATIONARY to "cycling",  // Health Connect calls this "biking_stationary"
        ExerciseSessionRecord.EXERCISE_TYPE_BOOT_CAMP to "boot_camp",
        ExerciseSessionRecord.EXERCISE_TYPE_BOXING to "boxing",
        ExerciseSessionRecord.EXERCISE_TYPE_CALISTHENICS to "calisthenics",
        ExerciseSessionRecord.EXERCISE_TYPE_CRICKET to "cricket",
        ExerciseSessionRecord.EXERCISE_TYPE_DANCING to "dancing",
        ExerciseSessionRecord.EXERCISE_TYPE_ELLIPTICAL to "elliptical",
        ExerciseSessionRecord.EXERCISE_TYPE_EXERCISE_CLASS to "exercise_class",
        ExerciseSessionRecord.EXERCISE_TYPE_FENCING to "fencing",
        ExerciseSessionRecord.EXERCISE_TYPE_FOOTBALL_AMERICAN to "football_american",
        ExerciseSessionRecord.EXERCISE_TYPE_FOOTBALL_AUSTRALIAN to "football_australian",
        ExerciseSessionRecord.EXERCISE_TYPE_FRISBEE_DISC to "frisbee_disc",
        ExerciseSessionRecord.EXERCISE_TYPE_GOLF to "golf",
        ExerciseSessionRecord.EXERCISE_TYPE_GUIDED_BREATHING to "guided_breathing",
        ExerciseSessionRecord.EXERCISE_TYPE_GYMNASTICS to "gymnastics",
        ExerciseSessionRecord.EXERCISE_TYPE_HANDBALL to "handball",
        ExerciseSessionRecord.EXERCISE_TYPE_HIGH_INTENSITY_INTERVAL_TRAINING to "high_intensity_interval_training",
        ExerciseSessionRecord.EXERCISE_TYPE_HIKING to "hiking",
        ExerciseSessionRecord.EXERCISE_TYPE_ICE_HOCKEY to "ice_hockey",
        ExerciseSessionRecord.EXERCISE_TYPE_ICE_SKATING to "ice_skating",
        ExerciseSessionRecord.EXERCISE_TYPE_MARTIAL_ARTS to "martial_arts",
        ExerciseSessionRecord.EXERCISE_TYPE_PADDLING to "paddling",
        ExerciseSessionRecord.EXERCISE_TYPE_PARAGLIDING to "paragliding",
        ExerciseSessionRecord.EXERCISE_TYPE_PILATES to "pilates",
        ExerciseSessionRecord.EXERCISE_TYPE_RACQUETBALL to "racquetball",
        ExerciseSessionRecord.EXERCISE_TYPE_ROCK_CLIMBING to "rock_climbing",
        ExerciseSessionRecord.EXERCISE_TYPE_ROLLER_HOCKEY to "roller_hockey",
        ExerciseSessionRecord.EXERCISE_TYPE_ROWING to "rowing",
        ExerciseSessionRecord.EXERCISE_TYPE_ROWING_MACHINE to "rowing_machine",
        ExerciseSessionRecord.EXERCISE_TYPE_RUGBY to "rugby",
        ExerciseSessionRecord.EXERCISE_TYPE_RUNNING to "running",
        ExerciseSessionRecord.EXERCISE_TYPE_RUNNING_TREADMILL to "running",  // Health Connect calls this "running_treadmill"
        ExerciseSessionRecord.EXERCISE_TYPE_SAILING to "sailing",
        ExerciseSessionRecord.EXERCISE_TYPE_SCUBA_DIVING to "scuba_diving",
        ExerciseSessionRecord.EXERCISE_TYPE_SKATING to "skating",
        ExerciseSessionRecord.EXERCISE_TYPE_SKIING to "skiing",
        ExerciseSessionRecord.EXERCISE_TYPE_SNOWBOARDING to "snowboarding",
        ExerciseSessionRecord.EXERCISE_TYPE_SNOWSHOEING to "snowshoeing",
        ExerciseSessionRecord.EXERCISE_TYPE_SOCCER to "soccer",
        ExerciseSessionRecord.EXERCISE_TYPE_SOFTBALL to "softball",
        ExerciseSessionRecord.EXERCISE_TYPE_SQUASH to "squash",
        ExerciseSessionRecord.EXERCISE_TYPE_STAIR_CLIMBING to "stair_climbing",
        ExerciseSessionRecord.EXERCISE_TYPE_STAIR_CLIMBING_MACHINE to "stair_climbing_machine",
        ExerciseSessionRecord.EXERCISE_TYPE_STRENGTH_TRAINING to "strength_training",
        ExerciseSessionRecord.EXERCISE_TYPE_STRETCHING to "stretching",
        ExerciseSessionRecord.EXERCISE_TYPE_SURFING to "surfing",
        ExerciseSessionRecord.EXERCISE_TYPE_SWIMMING_OPEN_WATER to "swimming",  // Health Connect calls this "swimming_open_water"
        ExerciseSessionRecord.EXERCISE_TYPE_SWIMMING_POOL to "swimming",  // Health Connect calls this "swimming_pool"
        ExerciseSessionRecord.EXERCISE_TYPE_TABLE_TENNIS to "table_tennis",
        ExerciseSessionRecord.EXERCISE_TYPE_TENNIS to "tennis",
        ExerciseSessionRecord.EXERCISE_TYPE_VOLLEYBALL to "volleyball",
        ExerciseSessionRecord.EXERCISE_TYPE_WALKING to "walking",
        ExerciseSessionRecord.EXERCISE_TYPE_WATER_POLO to "water_polo",
        ExerciseSessionRecord.EXERCISE_TYPE_WEIGHTLIFTING to "weightlifting",
        ExerciseSessionRecord.EXERCISE_TYPE_WHEELCHAIR to "wheelchair",
        ExerciseSessionRecord.EXERCISE_TYPE_YOGA to "yoga",
    )

    /** `"unknown"` for a type this version of Health Connect has and this table doesn't —
     *  the same value the server falls back to for an activity that arrives without one. */
    fun name(exerciseType: Int): String = NAMES[exerciseType] ?: "unknown"
}
