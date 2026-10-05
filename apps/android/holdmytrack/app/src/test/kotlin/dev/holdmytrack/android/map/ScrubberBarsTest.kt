package dev.holdmytrack.android.map

import java.time.LocalDate
import java.util.Locale
import org.junit.Assert.assertEquals
import org.junit.Test

class ScrubberBarsTest {

    @Test
    fun levelsAreTheSquareRootOfEachDaysShareOfTheLongest() {
        assertEquals(listOf(1f, 0.5f), ScrubberBars.levels(listOf(16_000.0, 4_000.0)))
    }

    @Test
    fun aShortDayStillShows() {
        assertEquals(ScrubberBars.MIN_LEVEL, ScrubberBars.levels(listOf(100_000.0, 10.0))[1])
    }

    @Test
    fun noDistanceAnywhereGivesTheShortestBars() {
        assertEquals(listOf(ScrubberBars.MIN_LEVEL, ScrubberBars.MIN_LEVEL), ScrubberBars.levels(listOf(0.0, 0.0)))
        assertEquals(emptyList<Float>(), ScrubberBars.levels(emptyList()))
    }

    @Test
    fun theMonthIsWrittenWhereTheWindowStartsAndWhereANewOneDoes() {
        assertEquals(
            listOf(
                ScrubberBars.Label(27, 9),
                ScrubberBars.Label(28, null),
                ScrubberBars.Label(1, 10),
                ScrubberBars.Label(3, null),
            ),
            ScrubberBars.labels(listOf("2026-09-27", "2026-09-28", "2026-10-01", "2026-10-03")),
        )
    }

    @Test
    fun theRangeSaysItsYearOnce() {
        fun label(from: String, to: String) = ScrubberBars.rangeLabel(LocalDate.parse(from), LocalDate.parse(to), Locale.ENGLISH)
        assertEquals("3 Oct 2026", label("2026-10-03", "2026-10-03"))
        assertEquals("24 – 28 Sep 2026", label("2026-09-24", "2026-09-28"))
        assertEquals("24 Sep – 3 Oct 2026", label("2026-09-24", "2026-10-03"))
        assertEquals("28 Dec 2025 – 3 Jan 2026", label("2025-12-28", "2026-01-03"))
    }
}
