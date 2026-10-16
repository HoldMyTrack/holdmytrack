// With its extension, so the unit tests' Node can load it too (tsconfig's allowImportingTsExtensions).
import { TRACKS_MIN_ZOOM } from './config.ts';

/**
 * Grouping photo markers that would overlap on screen — the photos of a whole trip
 * seen zoomed out, or several taken at one spot. Done in screen pixels, after each move of the
 * map, since what overlaps depends on the zoom; tens of photos per route make that cheap.
 */

export interface ScreenPoint {
  id: string;
  x: number;
  y: number;
}

export interface PhotoCluster {
  /** In the order given (route order), the first being the one the group shows. */
  ids: string[];
  x: number;
  y: number;
}

/** How close two markers' centres may be, in CSS pixels, before they're one group: a little
 *  under a marker's width (36 px), so markers that would merely touch stay apart. */
export const CLUSTER_RADIUS_PX = 32;

/** The same for a small marker (22 px). */
export const SMALL_CLUSTER_RADIUS_PX = 18;

/** From this zoom up a marker is drawn full size; below it, small, so the tracks — thin lines
 *  across a whole region at that scale — still show under the photos. */
export const PHOTO_MARKERS_FULL_ZOOM = 10;

export type PhotoMarkerSize = 'hidden' | 'small' | 'full';

/** How photo markers are drawn at `zoom`: not at all where tracks aren't either (below
 *  TRACKS_MIN_ZOOM, FR-4.1) — a photo would sit over a continent with no route under it —
 *  small below PHOTO_MARKERS_FULL_ZOOM, full size from it. */
export function photoMarkerSize(zoom: number): PhotoMarkerSize {
  if (zoom < TRACKS_MIN_ZOOM) return 'hidden';
  return zoom < PHOTO_MARKERS_FULL_ZOOM ? 'small' : 'full';
}

/**
 * Greedy grouping in the given order: each point joins the first group whose anchor — its first
 * point, where the group's marker sits — is within `radius`, or starts a group of its own.
 * Anchoring on a real point rather than a moving centroid keeps a group's marker on the route
 * and the result stable as points are added.
 */
export function clusterPoints(points: readonly ScreenPoint[], radius = CLUSTER_RADIUS_PX): PhotoCluster[] {
  const clusters: PhotoCluster[] = [];
  for (const p of points) {
    const near = clusters.find((c) => (c.x - p.x) ** 2 + (c.y - p.y) ** 2 <= radius ** 2);
    if (near) near.ids.push(p.id);
    else clusters.push({ ids: [p.id], x: p.x, y: p.y });
  }
  return clusters;
}
