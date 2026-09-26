package dev.holdmytrack.android.recording

import android.content.res.Resources
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.Session
import java.util.Locale

/** Distances, heights and speeds as text — shared by `RecordingService`'s notification, the
 *  Edit screen, Recorded Activities and sync history, so none disagrees about how a number
 *  reads. The numbers follow the app's language (a decimal comma in Russian), and so do the
 *  units, which are string resources. The unit system is the account's Country's, as on the
 *  web (`services/server/internal/web/format.go`'s `Imperial`): miles and feet for the United
 *  States, Liberia and Myanmar, metric everywhere else, and metric until a Country is saved. */
object RecordingFormat {

    private const val METERS_PER_MILE = 1609.344
    private const val FEET_PER_METER = 3.28084
    private val IMPERIAL_COUNTRIES = setOf("US", "LR", "MM")

    fun imperial(country: String = Session.country): Boolean = country in IMPERIAL_COUNTRIES

    fun duration(elapsedMs: Long): String {
        val totalSeconds = elapsedMs / 1000
        return String.format(Locale.ROOT, "%d:%02d:%02d", totalSeconds / 3600, (totalSeconds % 3600) / 60, totalSeconds % 60)
    }

    fun distance(res: Resources, meters: Double): String =
        if (imperial()) res.getString(R.string.unit_mi, meters / METERS_PER_MILE) else res.getString(R.string.unit_km, meters / 1000.0)

    fun altitude(res: Resources, meters: Double): String =
        if (imperial()) res.getString(R.string.unit_ft, meters * FEET_PER_METER) else res.getString(R.string.unit_m, meters)

    fun speed(res: Resources, metersPerSecond: Double): String =
        if (imperial()) {
            res.getString(R.string.unit_mph, metersPerSecond * 3600 / METERS_PER_MILE)
        } else {
            res.getString(R.string.unit_kmh, metersPerSecond * 3.6)
        }
}
