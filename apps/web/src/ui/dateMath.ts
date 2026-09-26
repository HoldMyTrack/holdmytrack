/**
 * Day arithmetic on plain YYYY-MM-DD strings.
 *
 * Deliberately small: the desktop range picker pages by days-with-activity
 * (useActivityDays), which is index arithmetic over an array, and the strings sort
 * lexicographically in chronological order, so comparing and clamping dates needs no helper
 * at all. What's here answers questions about elapsed calendar time: MapView's "N-day range"
 * stat (`dayDiff`), and the mobile date slider's calendar-day scale (`dayDiff`/`addDays`).
 */

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

/** Whole calendar days from `from` to `to`, inclusive of neither end — `dayDiff(a, a) === 0`.
 *  UTC-parsed on purpose, unlike todayLocal above: this diffs two already-resolved date-only
 *  strings, and parsing a date-only string as UTC (rather than the browser's own zone) is what
 *  avoids an off-by-one from a DST transition landing inside the range. */
export function dayDiff(from: string, to: string): number {
  return Math.round((Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86_400_000);
}

/** `date` moved by `days` calendar days (negative for earlier) — UTC-parsed for the same DST
 *  reason as dayDiff. The mobile date slider (DateRangeSlider.tsx) works in day offsets from
 *  its first day and needs this to turn a knob position back into a date. */
export function addDays(date: string, days: number): string {
  return new Date(Date.parse(`${date}T00:00:00Z`) + days * 86_400_000).toISOString().slice(0, 10);
}
