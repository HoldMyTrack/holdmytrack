import type { LngLatLike, Map as MapLibreMap } from 'maplibre-gl';
import { sharedArchive } from './protocol';


/**
 * Coverage checking against the archive's own header bounds.
 *
 * The failure mode this exists for: the extract is one state, so sooner or later
 * someone looks at somewhere else and gets a blank grey canvas — which reads as a
 * broken product rather than a missing region. Saying so explicitly is a cheap
 * mitigation for that failure mode, and it costs one range request because the
 * header is already cached by the protocol.
 */

export interface CoverageBounds {
  minLon: number;
  minLat: number;
  maxLon: number;
  maxLat: number;
  minZoom: number;
  maxZoom: number;
}

export async function readCoverageBounds(url: string): Promise<CoverageBounds> {
  const header = await sharedArchive(url).getHeader();
  return {
    minLon: header.minLon,
    minLat: header.minLat,
    maxLon: header.maxLon,
    maxLat: header.maxLat,
    minZoom: header.minZoom,
    maxZoom: header.maxZoom,
  };
}

export function containsPoint(bounds: CoverageBounds, point: LngLatLike): boolean {
  const { lng, lat } = normalize(point);
  return lng >= bounds.minLon && lng <= bounds.maxLon && lat >= bounds.minLat && lat <= bounds.maxLat;
}

/**
 * Whether a track's bounding box overlaps coverage at all. Unused until GPX
 * parsing lands, but it is the check the upload path needs and it belongs next to
 * the header read rather than in the importer.
 */
export function intersectsBox(
  bounds: CoverageBounds,
  box: { minLon: number; minLat: number; maxLon: number; maxLat: number },
): boolean {
  return (
    box.minLon <= bounds.maxLon &&
    box.maxLon >= bounds.minLon &&
    box.minLat <= bounds.maxLat &&
    box.maxLat >= bounds.minLat
  );
}

/** True when the viewport centre has left coverage — what drives the on-screen notice. */
export function centreIsOutside(map: MapLibreMap, bounds: CoverageBounds): boolean {
  return !containsPoint(bounds, map.getCenter());
}

function normalize(point: LngLatLike): { lng: number; lat: number } {
  if (Array.isArray(point)) return { lng: point[0], lat: point[1] };
  if ('lng' in point) return { lng: point.lng, lat: point.lat };
  return { lng: point.lon, lat: point.lat };
}
