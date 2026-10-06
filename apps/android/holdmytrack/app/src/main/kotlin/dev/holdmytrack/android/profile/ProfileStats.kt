package dev.holdmytrack.android.profile

import dev.holdmytrack.android.net.ActivityDay
import dev.holdmytrack.android.net.TrendPeriod
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.temporal.ChronoUnit
import java.time.temporal.TemporalAdjusters
import kotlin.math.floor
import kotlin.math.ln
import kotlin.math.max
import kotlin.math.min

/** The four numbers the web's Profile page shows all-time and under each year's grid. */
data class GridStats(val count: Int, val distanceMeters: Double, val activeDays: Int, val longestStreakDays: Int)

/** Which of a day's two numbers shades its cell — the web's `?shade=count|distance`. */
enum class Shade { COUNT, DISTANCE }

/**
 * One year laid out as whole weeks, Sunday to Saturday, column by column — so the first and last
 * columns carry padding days outside the year. [levels] is 0–3 per cell, -1 for padding;
 * [monthColumns] the column each month's first day falls in, January first.
 */
class YearLayout(
    val year: Int,
    val gridStart: LocalDate,
    val weeks: Int,
    val levels: IntArray,
    val monthColumns: IntArray,
) {
    /** The day in [column], [row] (0 is Sunday), or null for a padding cell. */
    fun dayAt(column: Int, row: Int): LocalDate? {
        if (column !in 0 until weeks || row !in 0..6) return null
        val day = gridStart.plusDays(column * 7L + row)
        return if (day.year == year) day else null
    }

    fun columnOf(day: LocalDate): Int = (ChronoUnit.DAYS.between(gridStart, day) / 7).toInt()
}

/**
 * The web Profile page's rules (`services/server/internal/httpapi/profile_page.go`), ported:
 * each year's and the all-time stats derived from the daily totals, the grid's layout and its
 * shading, and Trends' bar heights. The days are the histogram's buckets — the account's own
 * local days, ascending, never one without activity.
 */
object ProfileStats {

    /** `statsOf`: count and distance are sums, active days the number of days, the streak the
     *  longest run of consecutive ones. */
    fun statsOf(days: List<ActivityDay>): GridStats {
        var count = 0
        var distance = 0.0
        var run = 0
        var longest = 0
        var prev: LocalDate? = null
        for (d in days) {
            count += d.count
            distance += d.distanceMeters
            val day = LocalDate.parse(d.date)
            run = if (prev != null && ChronoUnit.DAYS.between(prev, day) == 1L) run + 1 else 1
            longest = max(longest, run)
            prev = day
        }
        return GridStats(count, distance, days.size, longest)
    }

    /** The days that fall in [year]. */
    fun daysInYear(days: List<ActivityDay>, year: Int): List<ActivityDay> {
        val prefix = "%04d-".format(year)
        return days.filter { it.date.startsWith(prefix) }
    }

    /** `distanceThresholds`: the 1/3 and 2/3 points of a year's non-zero day distances, so
     *  distance shading is relative to that year's own spread. */
    fun distanceThresholds(days: List<ActivityDay>): Pair<Double, Double> {
        val ds = days.map { it.distanceMeters }.filter { it > 0 }.sorted()
        if (ds.isEmpty()) return 0.0 to 0.0
        fun at(p: Double) = ds[min(ds.size - 1, floor(p * ds.size).toInt())]
        return at(1.0 / 3) to at(2.0 / 3)
    }

    /** `shadeLevel`: 0–3 — by count, 1, 2, and 3-or-more activities; by distance, the year's thirds. */
    fun shadeLevel(day: ActivityDay?, shade: Shade, t1: Double, t2: Double): Int {
        if (day == null) return 0
        if (shade == Shade.COUNT) return min(day.count, 3)
        return when {
            day.distanceMeters <= 0 -> 0
            day.distanceMeters <= t1 -> 1
            day.distanceMeters <= t2 -> 2
            else -> 3
        }
    }

    /** `buildYearGrid`'s layout, for [days] already narrowed to [year]. */
    fun layout(year: Int, days: List<ActivityDay>, shade: Shade): YearLayout {
        val byDate = days.associateBy { it.date }
        val jan1 = LocalDate.of(year, 1, 1)
        val dec31 = LocalDate.of(year, 12, 31)
        val gridStart = jan1.with(TemporalAdjusters.previousOrSame(DayOfWeek.SUNDAY))
        val weeks = ((ChronoUnit.DAYS.between(gridStart, dec31) + 1 + 6) / 7).toInt()
        val (t1, t2) = distanceThresholds(days)
        val levels = IntArray(weeks * 7) { i ->
            val day = gridStart.plusDays(i.toLong())
            if (day.year != year) -1 else shadeLevel(byDate[day.toString()], shade, t1, t2)
        }
        val months = IntArray(12) { m ->
            (ChronoUnit.DAYS.between(gridStart, LocalDate.of(year, m + 1, 1)) / 7).toInt()
        }
        return YearLayout(year, gridStart, weeks, levels, months)
    }

    /**
     * The You tab's preview of the graph: the last [weeks] weeks through [today], Sunday to
     * Saturday column by column as a year's grid is, the last column this week. Each cell is
     * shaded by count (0–3); the days after [today] are -1, left blank.
     */
    fun recentWeeks(days: List<ActivityDay>, today: LocalDate, weeks: Int): IntArray {
        val byDate = days.associateBy { it.date }
        val start = today.with(TemporalAdjusters.previousOrSame(DayOfWeek.SUNDAY)).minusWeeks(weeks - 1L)
        return IntArray(weeks * 7) { i ->
            val day = start.plusDays(i.toLong())
            if (day.isAfter(today)) -1 else shadeLevel(byDate[day.toString()], Shade.COUNT, 0.0, 0.0)
        }
    }

    /** `buildTrendBars`' heights, 0–1: a log curve against the busiest period, so one huge week
     *  doesn't flatten every other bar to nothing. */
    fun trendHeights(periods: List<TrendPeriod>): List<Double> {
        val peak = periods.maxOfOrNull { it.distanceMeters } ?: 0.0
        return periods.map { if (peak > 0) ln(it.distanceMeters + 1) / ln(peak + 1) else 0.0 }
    }
}