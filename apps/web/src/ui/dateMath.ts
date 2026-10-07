/**
 * Plain YYYY-MM-DD day strings: the date range the Activities tab's slider selects, and "today".
 * The strings sort lexicographically in chronological order, so comparing and clamping dates
 * needs no helper at all, and the slider pages by days-with-activity (useActivityDays), which is
 * index arithmetic over an array rather than calendar arithmetic.
 */

/** The selected date range, both ends inclusive — MapView's `selectedRange`, the list and
 *  tracks filter while the Activities tab is showing. */
export interface DateRange {
  from: string;
  to: string;
}

/** "What day is it" for the caller's own browser — the local calendar day, matching what a
 *  person looking at the app understands "today" to mean. Only ever the *fallback* `today` a
 *  zero-activity account's degenerate range collapses to (MapView.tsx) — real activity data,
 *  whose days the server gives in each activity's own zone, always takes precedence once
 *  there is any. */
export function todayLocal(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
}
