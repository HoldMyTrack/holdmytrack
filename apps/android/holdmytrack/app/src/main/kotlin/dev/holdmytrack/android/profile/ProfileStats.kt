package dev.holdmytrack.android.profile

import dev.holdmytrack.android.net.ActivityDay
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.temporal.ChronoUnit
import java.time.temporal.TemporalAdjusters
import kotlin.math.max
import kotlin.math.min

/** The four all-time numbers the You tab shows, as the web's Profile page does. */
data class GridStats(val count: Int, val distanceMeters: Double, val activeDays: Int, val longestStreakDays: Int)

/**
 * The You tab's numbers and its glance at the activity graph, by the web Profile page's rules
 * (`services/server/internal/httpapi/profile_page.go`). The days are the histogram's buckets —
 * the account's own local days, ascending, never one without activity.
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

    /**
     * The last [weeks] weeks through [today], Sunday to Saturday column by column as a year's
     * grid is, the last column this week. Each cell is shaded by count, as the grid's
     * `shadeLevel` does: 0 for none, then 1, 2, and 3 for three or more; the days after [today]
     * are -1, left blank.
     */
    fun recentWeeks(days: List<ActivityDay>, today: LocalDate, weeks: Int): IntArray {
        val byDate = days.associateBy { it.date }
        val start = today.with(TemporalAdjusters.previousOrSame(DayOfWeek.SUNDAY)).minusWeeks(weeks - 1L)
        return IntArray(weeks * 7) { i ->
            val day = start.plusDays(i.toLong())
            if (day.isAfter(today)) -1 else min(byDate[day.toString()]?.count ?: 0, 3)
        }
    }
}
