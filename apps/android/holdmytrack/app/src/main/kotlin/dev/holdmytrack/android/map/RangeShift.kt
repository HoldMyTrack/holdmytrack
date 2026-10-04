package dev.holdmytrack.android.map

/** What [shiftRange] found: the moved range, nowhere to go, or days still to load. */
sealed interface RangeShift {
    data class Shifted(val range: DateRange) : RangeShift
    data object None : RangeShift
    data object Load : RangeShift
}

/**
 * Where the date slider's range-shift buttons move a selection (`docs/SPEC.md` FR-6.7) — the
 * web's `apps/web/src/ui/rangeShift.ts`, line for line: the same number of activity days,
 * packed right up against the old range on the side it moves toward.
 *
 * [days] is the loaded run of activity days (YYYY-MM-DD, ascending, ending at the newest);
 * [hasEarlier] says whether more history sits before it, unloaded. Near either end of the
 * history the shifted range stops at that end at its full size rather than shrinking. [RangeShift.Load]
 * means the days it needs are older than what's loaded — fetch the next page and ask again.
 */
fun shiftRange(days: List<String>, hasEarlier: Boolean, range: DateRange, dir: Int): RangeShift {
    val n = days.size
    if (n == 0) return RangeShift.None
    // The selection's first and last activity days, as indices. A range holding none still
    // counts as one day wide, so the shift reaches the next day that has one.
    val first = days.indexOfFirst { it >= range.from }.let { if (it == -1) n else it }
    val last = days.indexOfLast { it <= range.to }
    if (first == 0 && range.from < days[0] && hasEarlier) return RangeShift.Load
    val size = maxOf(1, last - first + 1)

    if (dir < 0) {
        if (first == 0) return if (hasEarlier) RangeShift.Load else RangeShift.None
        var start = first - size
        if (start < 0) {
            if (hasEarlier) return RangeShift.Load
            start = 0
        }
        return RangeShift.Shifted(DateRange(days[start], days[minOf(start + size, n) - 1]))
    }

    if (last >= n - 1) return RangeShift.None
    val end = minOf(last + size, n - 1)
    return RangeShift.Shifted(DateRange(days[maxOf(end - size + 1, 0)], days[end]))
}
