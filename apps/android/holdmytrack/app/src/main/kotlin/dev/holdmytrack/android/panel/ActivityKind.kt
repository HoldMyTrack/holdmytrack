package dev.holdmytrack.android.panel

/**
 * What kind of moving an activity type is, for what its row and card show: an icon, and pace or
 * speed. Types are free text (`RecordingTypes`), so this reads the words in them rather than a
 * fixed list — "gravel_cycling" and "e_biking" are on wheels, "trail_running" on foot.
 */
enum class ActivityKind {
    /** Walking, hiking, running: pace, minutes per km or mile. */
    FOOT,

    /** Cycling, skating, a solowheel: speed. */
    WHEELS,

    /** Driving, motorcycling: speed. */
    MOTOR,

    /** Everything else — paddling, skiing, an untyped activity: speed, the safer reading. */
    OTHER;

    /** Pace reads naturally only on foot; everything faster reads as a speed. */
    val showsPace: Boolean get() = this == FOOT

    companion object {
        private val FOOT_WORDS = listOf("walk", "hik", "run", "jog", "trek", "stroll", "march")
        private val MOTOR_WORDS = listOf("driv", "motor", "car")
        private val WHEEL_WORDS = listOf("cycl", "bik", "skat", "wheel", "scoot", "board")

        fun of(type: String): ActivityKind {
            val t = type.lowercase()
            return when {
                MOTOR_WORDS.any { it in t } -> MOTOR
                WHEEL_WORDS.any { it in t } -> WHEELS
                FOOT_WORDS.any { it in t } -> FOOT
                else -> OTHER
            }
        }
    }
}

/** Pace and speed from a distance and a moving time, in a unit [unitMeters] long (a km or a mile). */
object Pace {

    /** Seconds per unit, or null with no distance or no time. */
    fun secondsPerUnit(meters: Double?, seconds: Long?, unitMeters: Double): Double? {
        if (meters == null || seconds == null || meters <= 0 || seconds <= 0) return null
        return seconds / (meters / unitMeters)
    }

    /** "5:27": minutes and seconds, rounded to the second. */
    fun format(secondsPerUnit: Double): String {
        val total = Math.round(secondsPerUnit)
        return "${total / 60}:${(total % 60).toString().padStart(2, '0')}"
    }

    /** Units per hour, or null with no distance or no time. */
    fun speed(meters: Double?, seconds: Long?, unitMeters: Double): Double? {
        if (meters == null || seconds == null || meters <= 0 || seconds <= 0) return null
        return (meters / unitMeters) / (seconds / 3600.0)
    }
}
