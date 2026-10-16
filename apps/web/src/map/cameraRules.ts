/** Where the camera goes when the map's first data lands (FR-4.5), as plain values. */
import type { Activity } from '../api';

/**
 * The first loaded list: fit the most recent drawn activity, or a whole Story (a trip). Fitting
 * the whole default range could zoom out to the world. A URL view (`#…`) keeps the camera.
 * Returns null to wait for a later list (a Story with nothing drawn yet leaves it to the range),
 * else the activities to fit, empty for none.
 */
export function firstListFit(activities: readonly Activity[], storyId: string | null, urlView: boolean): Activity[] | null {
  const drawn = activities.filter((a) => a.bbox !== null && !a.pending);
  if (storyId !== null && drawn.length === 0) return null;
  if (urlView || drawn.length === 0) return [];
  if (storyId !== null) return drawn;
  return [drawn.reduce((latest, a) => (a.startedAt > latest.startedAt ? a : latest))];
}

/** An account with no history at all gets a fallback view (its Country, else the world). Trap:
 *  the list is empty for any range without activity, so "no history" is the days' `earliest`. */
export function needsFallbackView(urlView: boolean, listed: number, daysReady: boolean, earliest: string | null): boolean {
  return !urlView && listed === 0 && daysReady && earliest == null;
}
