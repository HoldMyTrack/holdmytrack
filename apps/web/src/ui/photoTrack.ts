/**
 * A track as the Photos tab's slider walks it (IMPLEMENTATION.md §4.27): the display points with
 * their moments (`GET /v1/activities/track-metrics/{id}`'s `time_s`), measured along their
 * length. The slider runs by distance, not time, so a long stop doesn't take up half its
 * travel; a photo's place is still saved as a moment (`route_at`), which is what the server
 * keeps.
 */

export interface TimedPoint {
  lon: number;
  lat: number;
  /** Epoch seconds. */
  t: number;
}

export interface PhotoTrack {
  points: readonly TimedPoint[];
  /** Metres from the first point to each point. */
  along: readonly number[];
}

const EARTH_RADIUS_M = 6_371_008.8;

function haversineM(a: TimedPoint, b: TimedPoint): number {
  const rad = Math.PI / 180;
  const dLat = (b.lat - a.lat) * rad;
  const dLon = (b.lon - a.lon) * rad;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * rad) * Math.cos(b.lat * rad) * Math.sin(dLon / 2) ** 2;
  return 2 * EARTH_RADIUS_M * Math.asin(Math.min(1, Math.sqrt(h)));
}

export function photoTrack(points: readonly TimedPoint[]): PhotoTrack {
  const along = [0];
  for (let i = 1; i < points.length; i++) along.push(along[i - 1]! + haversineM(points[i - 1]!, points[i]!));
  return { points, along };
}

/** The place `fraction` (0–1) of the way along the track, with its moment. */
export function pointAt(track: PhotoTrack, fraction: number): TimedPoint {
  const { points, along } = track;
  if (points.length === 0) throw new Error('empty track');
  const total = along[along.length - 1]!;
  if (points.length === 1 || total === 0) {
    // No length to walk: go by time instead, from the first moment to the last.
    const first = points[0]!;
    const last = points[points.length - 1]!;
    return { ...first, t: Math.round(first.t + (last.t - first.t) * Math.min(1, Math.max(0, fraction))) };
  }
  const target = Math.min(1, Math.max(0, fraction)) * total;
  let i = 1;
  while (i < points.length - 1 && along[i]! < target) i++;
  const a = points[i - 1]!;
  const b = points[i]!;
  const span = along[i]! - along[i - 1]!;
  const k = span > 0 ? (target - along[i - 1]!) / span : 0;
  return { lon: a.lon + (b.lon - a.lon) * k, lat: a.lat + (b.lat - a.lat) * k, t: Math.round(a.t + (b.t - a.t) * k) };
}

/** How far along the track (0–1) the moment `t` (epoch seconds) is — the inverse of pointAt,
 *  for where a photo's slider starts. A moment before or after the track is at its end. */
export function fractionAt(track: PhotoTrack, t: number): number {
  const { points, along } = track;
  const total = along[along.length - 1] ?? 0;
  if (points.length < 2) return 0;
  if (t <= points[0]!.t) return 0;
  if (t >= points[points.length - 1]!.t) return 1;
  let i = 1;
  while (i < points.length - 1 && points[i]!.t < t) i++;
  const a = points[i - 1]!;
  const b = points[i]!;
  const k = b.t > a.t ? (t - a.t) / (b.t - a.t) : 0;
  if (total === 0) return (t - points[0]!.t) / (points[points.length - 1]!.t - points[0]!.t);
  return (along[i - 1]! + (along[i]! - along[i - 1]!) * k) / total;
}

/** What a photo waiting for a place was picked after: a photo the server placed (its moment), one
 *  that waited too (its key, placed or skipped since), or nothing — the first of its batch. */
export type PlaceAnchor = { t: number } | { waiting: string } | null;

/** How far past the previous photo a waiting one's slider starts: just after it, so photos
 *  picked in the order they were taken walk forward along the route. */
export const NEXT_PHOTO_STEP = 0.01;

/**
 * Where a waiting photo's slider starts (FR-16.6): just after the photo picked before it, which
 * is where the next picture of a walk usually is. A previous one that waited is followed by
 * where the user put it (`placed`), or past it to its own predecessor if it was skipped (null).
 * The first of a batch, or one after nothing placed at all, starts at the beginning of the route.
 */
export function startFraction(
  track: PhotoTrack,
  anchor: PlaceAnchor,
  placed: ReadonlyMap<string, number | null>,
  anchors: ReadonlyMap<string, PlaceAnchor>,
): number {
  for (let seen = 0; anchor !== null && seen <= anchors.size; seen++) {
    if ('t' in anchor) return Math.min(1, fractionAt(track, anchor.t) + NEXT_PHOTO_STEP);
    const at = placed.get(anchor.waiting);
    if (at !== undefined && at !== null) return Math.min(1, at + NEXT_PHOTO_STEP);
    anchor = anchors.get(anchor.waiting) ?? null;
  }
  return 0;
}
