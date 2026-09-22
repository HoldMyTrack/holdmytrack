/**
 * Day arithmetic on plain YYYY-MM-DD strings.
 *
 * This was a larger module — addDays/dayDiff/clampDate/clampWindow — back when the range
 * picker panned by calendar days and had to keep a `[earliest, today]` window's span intact
 * while clamping it. It pages by days-with-activity now (useActivityDays), which is index
 * arithmetic over an array, so most of that stopped being reachable: the strings sort
 * lexicographically in chronological order, so comparing and clamping dates elsewhere needs
 * no helper at all. `dayDiff` earns its keep back on its own — MapView still needs a real
 * calendar-day count for the histogram header's "N days" stat, which is a question about
 * elapsed time, not about paging through bars.
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

/** Whole calendar days from `from` to `to`, inclusive of neither end — `dayDiff(a, a) === 0`.
 *  UTC-parsed on purpose, unlike todayLocal above: this diffs two already-resolved date-only
 *  strings, and parsing a date-only string as UTC (rather than the browser's own zone) is what
 *  avoids an off-by-one from a DST transition landing inside the range. */
export function dayDiff(from: string, to: string): number {
  return Math.round((Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86_400_000);
}
