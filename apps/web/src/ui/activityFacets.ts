import type { Activity } from '../api';

/**
 * The TYPE and DISTANCE filters are pure client-side facets
 * over the activities already fetched for the current date range — no backend involvement,
 * since §4.7's list endpoint no longer paginates and the panel always holds the full range's
 * rows already (see AGENTS.md on why that made this possible). "Reset filters" is instant
 * for the same reason: there is nothing to refetch.
 */

export interface DistanceRange {
  min: number;
  max: number;
}

export interface TypeFacet {
  type: string;
  count: number;
}

/** The real min/max distance across the range's rows, or null when none have a distance at
 *  all — the slider has nothing to bound itself by in that case. */
export function distanceBounds(activities: Activity[]): DistanceRange | null {
  let min = Infinity;
  let max = -Infinity;
  for (const a of activities) {
    if (a.distanceMeters === null) continue;
    if (a.distanceMeters < min) min = a.distanceMeters;
    if (a.distanceMeters > max) max = a.distanceMeters;
  }
  return Number.isFinite(min) ? { min, max } : null;
}

/** A row with no recorded distance can't be said to lie inside a distance band — same
 *  nullability stance format.ts and the backend already take on this field. */
export function passesDistance(activity: Activity, filter: DistanceRange | null): boolean {
  if (!filter) return true;
  return activity.distanceMeters !== null && activity.distanceMeters >= filter.min && activity.distanceMeters <= filter.max;
}

/**
 * Type counts, over whatever survives the *distance* filter — so narrowing distance updates
 * a row's count — but not the type exclusions themselves, so removing "Run" doesn't also
 * erase its own count (moot anyway, since an excluded type's row still renders, just unchecked).
 * Sorted by count descending, busiest type first.
 */
export function typeFacets(activities: Activity[], distanceFilter: DistanceRange | null): TypeFacet[] {
  const counts = new Map<string, number>();
  for (const a of activities) {
    if (!passesDistance(a, distanceFilter)) continue;
    counts.set(a.activityType, (counts.get(a.activityType) ?? 0) + 1);
  }
  return [...counts.entries()].map(([type, count]) => ({ type, count })).sort((a, b) => b.count - a.count);
}

/** Whether a row survives both filters at once — what the list renders, what the map draws,
 *  and what the ACTIVITIES badge counts. */
export function passesFilters(
  activity: Activity,
  excludedTypes: ReadonlySet<string>,
  distanceFilter: DistanceRange | null,
): boolean {
  if (excludedTypes.has(activity.activityType)) return false;
  return passesDistance(activity, distanceFilter);
}
