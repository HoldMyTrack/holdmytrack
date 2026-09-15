/**
 * UTC-day arithmetic on plain YYYY-MM-DD strings.
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

export function todayUTC(): string {
  const now = new Date();
  return new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())).toISOString().slice(0, 10);
}

/** Whole calendar days from `from` to `to`, inclusive of neither end — `dayDiff(a, a) === 0`. */
export function dayDiff(from: string, to: string): number {
  return Math.round((Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86_400_000);
}
