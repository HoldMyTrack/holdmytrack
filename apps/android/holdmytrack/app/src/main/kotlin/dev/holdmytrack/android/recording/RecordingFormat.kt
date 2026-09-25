package dev.holdmytrack.android.recording

import android.content.res.Resources
import dev.holdmytrack.android.R
import java.util.Locale

/** The recording stats as text — shared by `RecordingService`'s notification and
 *  `RecordingActivity`'s Edit screen, so the two never disagree about how a number reads. The
 *  numbers follow the app's language (a decimal comma in Russian), and so do the units, which
 *  are string resources. */
object RecordingFormat {
    fun duration(elapsedMs: Long): String {
        val totalSeconds = elapsedMs / 1000
        return String.format(Locale.ROOT, "%d:%02d:%02d", totalSeconds / 3600, (totalSeconds % 3600) / 60, totalSeconds % 60)
    }

    fun distance(res: Resources, meters: Double): String = res.getString(R.string.unit_km, meters / 1000.0)

    fun altitude(res: Resources, meters: Double): String = res.getString(R.string.unit_m, meters)

    fun speed(res: Resources, metersPerSecond: Double): String = res.getString(R.string.unit_kmh, metersPerSecond * 3.6)
}
