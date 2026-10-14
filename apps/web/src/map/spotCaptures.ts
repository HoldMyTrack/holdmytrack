import type { SpotCapture } from '../api';

/**
 * What the map asks `GET /v1/spots/captures` for on a return to its tab: `since` the newest
 * capture it already has, so the answer is usually empty (FR-15.2). Undefined, for the whole
 * list, when it has none. The newest is chosen by time but sent back as the server wrote it,
 * so no precision is lost to `Date`'s milliseconds.
 */
export function capturesSince(known: readonly SpotCapture[]): string | undefined {
  let newest: SpotCapture | undefined;
  for (const c of known) {
    if (!newest || Date.parse(c.captured_at) > Date.parse(newest.captured_at)) newest = c;
  }
  return newest?.captured_at;
}

/**
 * [known] with [fresh] added, one per place, newest first. A capture is never changed or
 * removed (IMPLEMENTATION.md §4.25), so what an answer `since` the newest leaves out is still
 * as the map has it; the newest itself comes back again and is kept once.
 */
export function mergeSpotCaptures(known: readonly SpotCapture[], fresh: readonly SpotCapture[]): SpotCapture[] {
  const byPlace = new Map<number, SpotCapture>();
  for (const c of known) byPlace.set(c.spot_id, c);
  for (const c of fresh) byPlace.set(c.spot_id, c);
  return [...byPlace.values()].sort((a, b) => Date.parse(b.captured_at) - Date.parse(a.captured_at) || a.spot_id - b.spot_id);
}
