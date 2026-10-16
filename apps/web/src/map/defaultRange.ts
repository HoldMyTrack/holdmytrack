import type { DateRange } from '../ui/dateMath';

/** The default range's length in activity-days (docs/SPEC.md FR-6.1). */
export const DEFAULT_RANGE_DAYS = 5;

/**
 * The default date range: the most recent activity-days in the slider's window, through today.
 * What opening the map asks about, quick to draw, and both knobs inside the window. With no days
 * in the window it starts at the earliest activity, else today. `visibleDays` is ascending.
 * Android: `map/DefaultRange.kt`.
 */
export function defaultRange(visibleDays: readonly { date: string }[], earliest: string | null, today: string): DateRange {
  const recent = visibleDays.slice(-DEFAULT_RANGE_DAYS);
  return { from: recent[0]?.date ?? earliest ?? today, to: today };
}
