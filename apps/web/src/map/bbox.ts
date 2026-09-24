import type { LngLatBoundsLike, Map as MapLibreMap } from 'maplibre-gl';
import type { BBox } from '../api';
import type { ViewState } from './viewState';

/**
 * Flying the map to an activity's bounds.
 *
 * The bounds come from `GET /v1/activities`'s per-row `bbox` (§4.7) rather than from the
 * rendered tiles: a track outside the current viewport isn't in any loaded tile, so asking
 * the map where it is would only ever work for activities already on screen.
 */

/**
 * Never zoom past this, however small the box. A short activity — or one clipped to almost
 * nothing by a Private location — has near-zero extent, and fitBounds on a degenerate box
 * happily zooms to MapLibre's z22 maximum, where the overzoomed z14 basemap has nothing left
 * to place the track against. z18 is still readable street-level detail.
 */
const MAX_FLY_ZOOM = 18;

/**
 * The share of the map container the fitted bounds should span along its tighter axis — the
 * rest is split evenly as padding on each side, which also keeps the track clear of the
 * docked header and the bottom timeline.
 */
const FLY_FILL = 0.75;

/** Long enough to read as a flight rather than a cut, short enough not to feel slow. */
const FLY_DURATION_MS = 900;

/** The smallest box containing all of them, or null if there were none. */
export function unionBBox(boxes: readonly BBox[]): BBox | null {
  if (boxes.length === 0) return null;
  return boxes.reduce<BBox>(
    (acc, b) => [Math.min(acc[0], b[0]), Math.min(acc[1], b[1]), Math.max(acc[2], b[2]), Math.max(acc[3], b[3])],
    [...boxes[0]!] as BBox,
  );
}

export function flyToBBox(map: MapLibreMap, bbox: BBox): void {
  const bounds: LngLatBoundsLike = [
    [bbox[0], bbox[1]],
    [bbox[2], bbox[3]],
  ];
  const { clientWidth, clientHeight } = map.getContainer();
  const x = (clientWidth * (1 - FLY_FILL)) / 2;
  const y = (clientHeight * (1 - FLY_FILL)) / 2;
  const padding = { top: y, bottom: y, left: x, right: x };
  map.fitBounds(bounds, { padding, maxZoom: MAX_FLY_ZOOM, duration: FLY_DURATION_MS });
}

/** For a bare point+zoom target (a country or world view) rather than an activity's bbox —
 *  MapView's zero-history fallback, docs/SPEC.md FR-4.5. */
export function flyToView(map: MapLibreMap, view: ViewState): void {
  map.flyTo({ center: [view.longitude, view.latitude], zoom: view.zoom, duration: FLY_DURATION_MS });
}
