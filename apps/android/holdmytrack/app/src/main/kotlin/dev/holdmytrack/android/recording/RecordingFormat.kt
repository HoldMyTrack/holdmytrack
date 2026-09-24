package dev.holdmytrack.android.recording

import java.util.Locale

/** The recording stats as text — shared by `RecordingService`'s notification and
 *  `RecordingActivity`'s Edit screen, so the two never disagree about how a number reads. */
object RecordingFormat {
    fun duration(elapsedMs: Long): String {
        val totalSeconds = elapsedMs / 1000
        return String.format(Locale.US, "%d:%02d:%02d", totalSeconds / 3600, (totalSeconds % 3600) / 60, totalSeconds % 60)
    }

    fun distance(meters: Double): String = String.format(Locale.US, "%.2f km", meters / 1000.0)

    fun altitude(meters: Double): String = String.format(Locale.US, "%.0f m", meters)

    fun speed(metersPerSecond: Double): String = String.format(Locale.US, "%.1f km/h", metersPerSecond * 3.6)
}
