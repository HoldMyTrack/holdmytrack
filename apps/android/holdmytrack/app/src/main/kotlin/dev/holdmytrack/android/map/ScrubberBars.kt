package dev.holdmytrack.android.map

import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlin.math.sqrt

/**
 * The date scrubber's pure parts (`DayScrubberView`, `DateRangeSlider`): how tall each activity
 * day's bar stands, what's written under it, and the selected range as a heading.
 */
object ScrubberBars {

    /** The shortest bar, as a share of the tallest: a day with any activity always shows. */
    const val MIN_LEVEL = 0.15f

    /**
     * Each day's bar height, 0..1, from its distance: the square root of its share of the
     * window's longest day, so a short walk beside a long ride still reads as more than a
     * sliver; never under [MIN_LEVEL]. All [MIN_LEVEL] when nothing in the window has a distance.
     */
    fun levels(distances: List<Double>): List<Float> {
        val max = distances.maxOrNull() ?: return emptyList()
        if (max <= 0.0) return distances.map { MIN_LEVEL }
        return distances.map { d -> maxOf(MIN_LEVEL, sqrt((d.coerceAtLeast(0.0) / max)).toFloat()) }
    }

    /** What's written under one bar: its day of the month, and its month too where the window
     *  starts or a new month does. */
    data class Label(val day: Int, val month: Int?)

    /** The label under each of [days] (`YYYY-MM-DD`, ascending). */
    fun labels(days: List<String>): List<Label> = days.mapIndexed { i, date ->
        val day = LocalDate.parse(date)
        val newMonth = i == 0 || days[i - 1].take(7) != date.take(7)
        Label(day.dayOfMonth, if (newMonth) day.monthValue else null)
    }

    /**
     * The selection as the sheet's heading, the year said once: "3 Oct 2026", "24 – 28 Sep
     * 2026", "24 Sep – 3 Oct 2026", or "28 Dec 2025 – 3 Jan 2026" across a new year. Months in
     * [locale]'s short form.
     */
    fun rangeLabel(from: LocalDate, to: LocalDate, locale: Locale): String {
        val dayMonth = DateTimeFormatter.ofPattern("d MMM", locale)
        val dayMonthYear = DateTimeFormatter.ofPattern("d MMM yyyy", locale)
        return when {
            from == to -> dayMonthYear.format(to)
            from.year != to.year -> "${dayMonthYear.format(from)} – ${dayMonthYear.format(to)}"
            from.month == to.month -> "${from.dayOfMonth} – ${dayMonthYear.format(to)}"
            else -> "${dayMonth.format(from)} – ${dayMonthYear.format(to)}"
        }
    }
}
