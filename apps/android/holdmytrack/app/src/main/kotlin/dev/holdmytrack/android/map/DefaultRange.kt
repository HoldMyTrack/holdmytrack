package dev.holdmytrack.android.map

/**
 * The default date range (`docs/SPEC.md` FR-6.1): the most recent [DAYS] activity days in the
 * slider's window, through today. With no days in the window it starts at the earliest
 * activity, else today. The web's `defaultRange.ts`; both run `testdata/map-session`.
 */
object DefaultRange {
    const val DAYS = 5

    /** [visibleDates] ascending. */
    fun of(visibleDates: List<String>, earliest: String?, today: String): DateRange =
        DateRange(visibleDates.takeLast(DAYS).firstOrNull() ?: earliest ?: today, today)
}
