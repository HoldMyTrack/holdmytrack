package dev.holdmytrack.android.profile

import dev.holdmytrack.android.net.ActivityDay
import dev.holdmytrack.android.net.TrendPeriod
import java.time.LocalDate
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ProfileStatsTest {

    private fun day(date: String, count: Int = 1, meters: Double = 1000.0) = ActivityDay(date, count, meters)

    @Test
    fun `stats sum the days and find the longest run of consecutive ones`() {
        val days = listOf(
            day("2025-12-30", 2, 500.0),
            day("2025-12-31"),
            day("2026-01-01"),
            day("2026-01-03"),
            day("2026-01-04"),
        )
        assertEquals(GridStats(count = 6, distanceMeters = 4500.0, activeDays = 5, longestStreakDays = 3), ProfileStats.statsOf(days))
    }

    @Test
    fun `no days is all zeros`() {
        assertEquals(GridStats(0, 0.0, 0, 0), ProfileStats.statsOf(emptyList()))
    }

    @Test
    fun `a year's days are only its own`() {
        val days = listOf(day("2025-12-31"), day("2026-01-01"), day("2026-12-31"), day("2027-01-01"))
        assertEquals(listOf("2026-01-01", "2026-12-31"), ProfileStats.daysInYear(days, 2026).map { it.date })
    }

    @Test
    fun `count shading is one, two, three or more`() {
        assertEquals(0, ProfileStats.shadeLevel(null, Shade.COUNT, 0.0, 0.0))
        assertEquals(1, ProfileStats.shadeLevel(day("2026-01-01", 1), Shade.COUNT, 0.0, 0.0))
        assertEquals(2, ProfileStats.shadeLevel(day("2026-01-01", 2), Shade.COUNT, 0.0, 0.0))
        assertEquals(3, ProfileStats.shadeLevel(day("2026-01-01", 7), Shade.COUNT, 0.0, 0.0))
    }

    @Test
    fun `distance shading is the year's own thirds`() {
        val days = listOf(1.0, 2.0, 3.0, 4.0, 5.0, 6.0).mapIndexed { i, km -> day("2026-01-0${i + 1}", meters = km * 1000) }
        val (t1, t2) = ProfileStats.distanceThresholds(days)
        assertEquals(3000.0, t1, 0.0)
        assertEquals(5000.0, t2, 0.0)
        assertEquals(1, ProfileStats.shadeLevel(days[0], Shade.DISTANCE, t1, t2))
        assertEquals(1, ProfileStats.shadeLevel(days[2], Shade.DISTANCE, t1, t2))
        assertEquals(2, ProfileStats.shadeLevel(days[3], Shade.DISTANCE, t1, t2))
        assertEquals(3, ProfileStats.shadeLevel(days[5], Shade.DISTANCE, t1, t2))
        assertEquals(0, ProfileStats.shadeLevel(day("2026-02-01", meters = 0.0), Shade.DISTANCE, t1, t2))
    }

    @Test
    fun `a year is whole weeks from the Sunday before January 1`() {
        // 2026 starts on a Thursday and ends on a Thursday: 53 columns.
        val layout = ProfileStats.layout(2026, listOf(day("2026-01-01", 2), day("2026-03-01", 5)), Shade.COUNT)
        assertEquals(LocalDate.of(2025, 12, 28), layout.gridStart)
        assertEquals(53, layout.weeks)
        assertEquals(-1, layout.levels[0]) // Sunday, December 28
        assertEquals(2, layout.levels[4]) // Thursday, January 1
        assertEquals(0, layout.levels[5])
        assertNull(layout.dayAt(0, 3))
        assertEquals(LocalDate.of(2026, 1, 1), layout.dayAt(0, 4))
        // March 1 2026 is a Sunday, the first row of column 9.
        assertEquals(9, layout.monthColumns[2])
        assertEquals(3, layout.levels[9 * 7])
        assertEquals(0, layout.monthColumns[0])
        assertEquals(52, layout.columnOf(LocalDate.of(2026, 12, 31)))
    }

    @Test
    fun `trend bars are a log curve against the busiest period`() {
        fun period(meters: Double) = TrendPeriod("2026-01-05", 1, meters, 0, 0.0)
        val heights = ProfileStats.trendHeights(listOf(period(0.0), period(99.0), period(9999.0)))
        assertEquals(0.0, heights[0], 1e-9)
        assertEquals(0.5, heights[1], 1e-9)
        assertEquals(1.0, heights[2], 1e-9)
        assertEquals(listOf(0.0), ProfileStats.trendHeights(listOf(period(0.0))))
    }

    @Test
    fun `recent weeks end with this week and leave the days after today blank`() {
        // Tuesday, October 6 2026: two weeks start on Sunday, September 27.
        val days = listOf(day("2026-09-26", 2), day("2026-09-27", 1), day("2026-10-06", 4))
        val levels = ProfileStats.recentWeeks(days, LocalDate.of(2026, 10, 6), 2)
        assertEquals(14, levels.size)
        assertEquals(1, levels[0])
        assertEquals(0, levels[1])
        assertEquals(3, levels[9])
        assertEquals(listOf(-1, -1, -1, -1), levels.drop(10))
    }
}