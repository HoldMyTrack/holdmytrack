package dev.holdmytrack.android.profile

import dev.holdmytrack.android.net.ActivityDay
import java.time.LocalDate
import org.junit.Assert.assertEquals
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
    fun `recent weeks end with this week and leave the days after today blank`() {
        // Tuesday, October 6 2026: two weeks start on Sunday, September 27.
        val days = listOf(day("2026-09-26", 2), day("2026-09-27", 1), day("2026-09-28", 2), day("2026-10-06", 4))
        val levels = ProfileStats.recentWeeks(days, LocalDate.of(2026, 10, 6), 2)
        assertEquals(14, levels.size)
        assertEquals(1, levels[0])
        assertEquals(2, levels[1])
        assertEquals(0, levels[2])
        assertEquals(3, levels[9])
        assertEquals(listOf(-1, -1, -1, -1), levels.drop(10))
    }
}