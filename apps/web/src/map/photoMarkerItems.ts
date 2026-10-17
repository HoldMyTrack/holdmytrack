/**
 * Which photos the map shows and what a marker's click does (FR-16), as plain values.
 * Android: `photos/PhotoMarkerScope.kt`.
 */
import type { Photo } from '../api';
import type { PhotoScope } from '../ui/usePhotos';
import type { MapMode } from './mapMode';
import type { PhotoMarkerItem, PhotoMarkerOverlay } from './photos';

/** How far apart, in metres, a group's photos must be for zooming in to separate them (§4.27). */
export const GROUP_SPREAD_M = 15;

/** The zoom a separable group fits to at most, where GROUP_SPREAD_M is wider than a marker. */
export const GROUP_MAX_ZOOM = 19;

/** Whose photos show, in Normal mode only: the Edit window's single activity (none for a group),
 *  else the focused activity, else the open Story. */
export function photoScopeFor(mode: MapMode, editWindowIds: readonly string[] | null, focused: string | null, storyId: string | null): PhotoScope {
  if (mode !== 'normal') return null;
  if (editWindowIds !== null) return editWindowIds.length === 1 ? { activity: editWindowIds[0]! } : null;
  if (focused !== null) return { activity: focused };
  if (storyId !== null) return { story: storyId };
  return null;
}

/** The markers: the scope's located photos on their routes, not while the Track tab has the map
 *  or the route is hidden. The Photos tab's unsaved moves and new photos are overlaid. */
export function photoMarkerItems(
  scope: PhotoScope,
  trackTabShown: boolean,
  mapHidden: ReadonlySet<string>,
  photos: readonly Photo[],
  overlay: PhotoMarkerOverlay | null,
  thumbBase: string,
): PhotoMarkerItem[] {
  if (scope === null || trackTabShown) return [];
  if ('activity' in scope && mapHidden.has(scope.activity)) return [];
  const items: PhotoMarkerItem[] = photos
    .filter((p) => p.lon !== null && p.lat !== null && !mapHidden.has(p.activityId))
    .map((p) => ({ id: p.id, lon: p.lon!, lat: p.lat!, thumbSrc: thumbBase + p.thumbUrl, caption: p.caption }));
  if (overlay === null) return items;
  const upserts = new Map(overlay.upserts.map((item) => [item.id, item]));
  const kept = items.filter((item) => !overlay.hidden.includes(item.id)).map((item) => upserts.get(item.id) ?? item);
  const keptIds = new Set(kept.map((item) => item.id));
  return [...kept, ...overlay.upserts.filter((item) => !keptIds.has(item.id))];
}

/** A marker's click: a group spread over more than GROUP_SPREAD_M zooms to fit its bounds
 *  (`[west, south, east, north]`); a single photo or a tighter group opens the popup. */
export function photoGroupAction(group: readonly { lon: number; lat: number }[]): { fit: [number, number, number, number] } | 'open' {
  const spread = Math.max(0, ...group.flatMap((a) => group.map((b) => haversineM(a.lat, a.lon, b.lat, b.lon))));
  if (group.length > 1 && spread > GROUP_SPREAD_M) {
    const lons = group.map((p) => p.lon);
    const lats = group.map((p) => p.lat);
    return { fit: [Math.min(...lons), Math.min(...lats), Math.max(...lons), Math.max(...lats)] };
  }
  return 'open';
}

export function haversineM(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const rad = Math.PI / 180;
  const h = Math.sin(((lat2 - lat1) * rad) / 2) ** 2 + Math.cos(lat1 * rad) * Math.cos(lat2 * rad) * Math.sin(((lon2 - lon1) * rad) / 2) ** 2;
  return 2 * 6_371_008.8 * Math.asin(Math.min(1, Math.sqrt(h)));
}
