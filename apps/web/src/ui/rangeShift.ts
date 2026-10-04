import type { DateRange } from './dateMath';

/**
 * Where the date slider's range-shift buttons move a selection (`SPEC.md` FR-6.7): the same
 * number of activity-days, packed right up against the old range on the side it moves toward.
 * Counted in activity-days like the rest of the slider, so a shift never lands on days with
 * nothing recorded.
 *
 * `days` is the loaded run of activity-days (YYYY-MM-DD, ascending, ending at the newest);
 * `hasEarlier` says whether more history sits before it, unloaded. Near either end of the
 * history the shifted range stops at that end at its full size rather than shrinking, so it
 * overlaps the old one there.
 *
 * Returns the shifted range; `null` when there's nothing further that way; `'load'` when the
 * days it needs are older than what's loaded — the caller fetches the next page and asks
 * again.
 */
export function shiftRange(
  days: readonly string[],
  hasEarlier: boolean,
  range: DateRange,
  dir: -1 | 1,
): DateRange | null | 'load' {
  const n = days.length;
  if (n === 0) return null;
  // The selection's first and last activity-days, as indices. A range holding none still
  // counts as one day wide, so the shift reaches the next day that has one.
  let first = days.findIndex((d) => d >= range.from);
  if (first === -1) first = n;
  let last = -1;
  for (let i = n - 1; i >= 0; i--) {
    if (days[i]! <= range.to) {
      last = i;
      break;
    }
  }
  if (first === 0 && range.from < days[0]! && hasEarlier) return 'load';
  const size = Math.max(1, last - first + 1);

  if (dir < 0) {
    if (first === 0) return hasEarlier ? 'load' : null;
    let start = first - size;
    if (start < 0) {
      if (hasEarlier) return 'load';
      start = 0;
    }
    return { from: days[start]!, to: days[Math.min(start + size, n) - 1]! };
  }

  if (last >= n - 1) return null;
  let end = last + size;
  if (end > n - 1) end = n - 1;
  return { from: days[Math.max(end - size + 1, 0)]!, to: days[end]! };
}
