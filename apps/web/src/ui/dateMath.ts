/**
 * Plain YYYY-MM-DD day strings: the date range the Activities tab's slider selects, and the
 * two ways of naming "which day" it needs. The strings sort lexicographically in chronological
 * order, so comparing and clamping dates needs no helper at all, and the slider pages by
 * days-with-activity (useActivityDays), which is index arithmetic over an array rather than
 * calendar arithmetic.
 */

/** The selected date range, both ends inclusive — MapView's `selectedRange`, the list, totals
 *  and tracks filter while the Activities tab is showing. */
export interface DateRange {
  from: string;
  to: string;
}

/** "What day is it" for the caller's own browser — the local calendar day, matching what a
 *  person looking at the app understands "today" to mean, and the account's own local day the
 *  server now buckets activities by (docs/KNOWN_ISSUES.md's fixed "UTC-day bucketing" entry).
 *  Only ever the *fallback* `today` a zero-activity account's degenerate range collapses to
 *  (MapView.tsx) — real activity data always takes precedence once there is any. */
export function todayLocal(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
}

/** The calendar day an instant falls on in `timeZone` (an IANA name) — the account's own
 *  timezone, since that's the day the server reads a bare `from`/`to` as. Slicing the ISO
 *  string instead gives the UTC day, which is a day late for an evening activity west of
 *  Greenwich and puts it outside a one-day range the server then answers. */
export function dayInZone(iso: string, timeZone: string): string {
  const parts = new Intl.DateTimeFormat('en-US', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' })
    .formatToParts(new Date(iso));
  const part = (type: Intl.DateTimeFormatPartTypes) => parts.find((p) => p.type === type)!.value;
  return `${part('year').padStart(4, '0')}-${part('month')}-${part('day')}`;
}
