package dev.holdmytrack.android.panel

import android.content.res.Resources
import dev.holdmytrack.android.R
import dev.holdmytrack.android.recording.RecordingFormat
import java.text.NumberFormat
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/**
 * The Activities panel's numbers and dates, the web's `apps/web/src/ui/format.ts` rules in the
 * app's language, so a row reads the same in both clients: one decimal for an activity's own
 * distance, a total rounded to whole units from 10 up, `1h 2m` / `42m` / `30s` durations, and a
 * start time as a medium date with a short time. Units follow the account's Country
 * (`RecordingFormat.imperial`).
 */
object PanelFormat {

    private const val METERS_PER_MILE = 1609.344
    private const val EM_DASH = "—"

    private fun locale(res: Resources): Locale = res.configuration.locales[0] ?: Locale.getDefault()

    /** A count, grouped in the app's language, as the web's `toLocaleString`. */
    fun count(res: Resources, n: Int): String = NumberFormat.getIntegerInstance(locale(res)).format(n)

    fun unit(res: Resources): String =
        res.getString(if (RecordingFormat.imperial()) R.string.panel_unit_mi else R.string.panel_unit_km)

    private fun converted(meters: Double) =
        if (RecordingFormat.imperial()) meters / METERS_PER_MILE else meters / 1000

    /** The web's `distanceValue`: the number alone, one decimal, no grouping. */
    fun distanceValue(res: Resources, meters: Double): String =
        NumberFormat.getNumberInstance(locale(res)).apply {
            minimumFractionDigits = 1
            maximumFractionDigits = 1
            isGroupingUsed = false
        }.format(converted(meters))

    fun distance(res: Resources, meters: Double?): String =
        if (meters == null) EM_DASH else "${distanceValue(res, meters)} ${unit(res)}"

    /** The web's `formatTotalDistance`: a small total keeps a decimal, so a short history
     *  never reads as "0 km". */
    fun totalDistance(res: Resources, meters: Double): String {
        val value = converted(meters)
        val number = NumberFormat.getNumberInstance(locale(res)).apply {
            minimumFractionDigits = 0
            maximumFractionDigits = if (value < 10) 1 else 0
        }.format(value)
        return "$number ${unit(res)}"
    }

    fun duration(res: Resources, seconds: Long?): String {
        if (seconds == null) return EM_DASH
        val h = seconds / 3600
        val m = (seconds % 3600) / 60
        return when {
            h > 0 -> res.getString(R.string.panel_duration_hm, h, m)
            m > 0 -> res.getString(R.string.panel_duration_m, m)
            else -> res.getString(R.string.panel_duration_s, seconds)
        }
    }

    /** "Sep 24, 2026, 4:00 AM", in the phone's timezone, as the web shows it in the browser's. */
    fun startedAt(res: Resources, iso: String): String =
        runCatching {
            DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT)
                .withLocale(locale(res))
                .withZone(ZoneId.systemDefault())
                .format(OffsetDateTime.parse(iso))
        }.getOrDefault(iso)
}
