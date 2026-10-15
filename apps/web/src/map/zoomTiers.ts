import type { MessageKey } from '../i18n/en';

/**
 * The three zoom-dependent tiers Fog falls back to below city zoom (IMPLEMENTATION.md §4.2.4) —
 * shared by fog.ts and the level notice so the boundary can't drift between the layers that
 * switch on it and what names it. Heatmap draws the same way at every zoom (heatPoints.ts). Adopted starting bands, tunable
 * visually — the same "adopted, not derived" convention internal/fog/raster.go's own
 * blur/feather constants use — not scientifically derived numbers.
 *
 * CITY_MIN_ZOOM is also the zoom the fog raster layer starts painting at. Normal
 * mode's tracks don't use it — they have no Country/Region fallback, so they draw from a much
 * lower zoom of their own (tracks.ts's TRACKS_MIN_ZOOM).
 */
export const COUNTRY_MAX_ZOOM = 3;
export const REGION_MIN_ZOOM = 3;
export const REGION_MAX_ZOOM = 7;
export const CITY_MIN_ZOOM = 7;
/** Fog tiles show as square pixels below this zoom and are smoothed from it up: the zoom their
 *  finest tiles are stored at (IMPLEMENTATION.md §4.2). */
export const PIXELS_MAX_ZOOM = 14;

/** Which of the three tiers Fog draws at `zoom` — the same bands the layers above switch on (a
 *  layer shows at minzoom <= zoom < maxzoom), for anything that has to name the tier in view,
 *  like ZoomLevelNotice.tsx. */
export type ZoomTier = 'country' | 'region' | 'city';

export function zoomTier(zoom: number): ZoomTier {
  if (zoom < COUNTRY_MAX_ZOOM) return 'country';
  if (zoom < REGION_MAX_ZOOM) return 'region';
  return 'city';
}

/** The Country/Region tier sources' credit: their outlines are Overture Maps' divisions, built
 *  from OpenStreetMap (ODbL, ADR-0034). MapLibre shows a source's attribution while it's in use,
 *  beside the basemap's OpenStreetMap credit. */
export const BOUNDARIES_ATTRIBUTION = '<a href="https://overturemaps.org">Overture Maps</a>';

/** The level notice's message (ZoomLevelNotice.tsx) for `mode` at `tier`: Fog's names the tier,
 *  since a veil lifted from a whole country needs explaining; Heatmap reads the same way at every
 *  zoom, so it has one message, and a zoom across a tier doesn't bring it back. */
export function levelNoticeKey(mode: 'fog' | 'heatmap', tier: ZoomTier): MessageKey {
  return mode === 'fog' ? `map.level.fog.${tier}` : 'map.level.heatmap';
}
