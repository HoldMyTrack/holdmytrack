package dev.holdmytrack.android.map

import org.junit.Assert.assertEquals
import org.junit.Test

/** The same cases as the web's `tests/rangeShift.test.mjs`. */
class RangeShiftTest {

    private val days = listOf("2026-09-01", "2026-09-03", "2026-09-06", "2026-09-07", "2026-09-08", "2026-09-09")

    private fun shifted(from: String, to: String) = RangeShift.Shifted(DateRange(from, to))

    @Test
    fun leftMovesTheSameNumberOfActivityDaysEndingJustBeforeTheOldStart() {
        assertEquals(shifted("2026-09-06", "2026-09-07"), shiftRange(days, false, DateRange("2026-09-08", "2026-09-09"), -1))
        // Counted in activity days, not calendar days: the two days before Sep 6–7 are Sep 1 and 3.
        assertEquals(shifted("2026-09-01", "2026-09-03"), shiftRange(days, false, DateRange("2026-09-06", "2026-09-07"), -1))
    }

    @Test
    fun rightMovesTheSameNumberOfActivityDaysStartingJustAfterTheOldEnd() {
        assertEquals(shifted("2026-09-06", "2026-09-07"), shiftRange(days, false, DateRange("2026-09-01", "2026-09-03"), 1))
    }

    @Test
    fun nearAnEndTheRangeKeepsItsSizeAndStopsThere() {
        assertEquals(shifted("2026-09-01", "2026-09-03"), shiftRange(days, false, DateRange("2026-09-03", "2026-09-06"), -1))
        assertEquals(shifted("2026-09-07", "2026-09-09"), shiftRange(days, false, DateRange("2026-09-06", "2026-09-08"), 1))
    }

    @Test
    fun nothingFurtherThatWay() {
        assertEquals(RangeShift.None, shiftRange(days, false, DateRange("2026-09-01", "2026-09-03"), -1))
        // The default range runs to today, past the newest activity day.
        assertEquals(RangeShift.None, shiftRange(days, false, DateRange("2026-09-07", "2026-10-03"), 1))
        assertEquals(RangeShift.None, shiftRange(emptyList(), false, DateRange("2026-09-07", "2026-09-08"), -1))
    }

    @Test
    fun asksForMoreHistoryWhenTheDaysItNeedsAreNotLoaded() {
        assertEquals(RangeShift.Load, shiftRange(days, true, DateRange("2026-09-01", "2026-09-03"), -1))
        assertEquals(RangeShift.Load, shiftRange(days, true, DateRange("2026-09-03", "2026-09-07"), -1))
        assertEquals(RangeShift.Load, shiftRange(days, true, DateRange("2026-08-20", "2026-09-03"), 1))
        assertEquals(shifted("2026-09-06", "2026-09-07"), shiftRange(days, true, DateRange("2026-09-08", "2026-09-09"), -1))
    }

    @Test
    fun aRangeWithNoActivityDaysInItMovesOneDay() {
        assertEquals(shifted("2026-09-03", "2026-09-03"), shiftRange(days, false, DateRange("2026-09-04", "2026-09-05"), -1))
        assertEquals(shifted("2026-09-06", "2026-09-06"), shiftRange(days, false, DateRange("2026-09-04", "2026-09-05"), 1))
    }
}
