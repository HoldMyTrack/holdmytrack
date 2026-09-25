/**
 * The three zoom-dependent tiers Fog and Heatmap fall back to below city zoom
 * (IMPLEMENTATION.md §4.2.4) — shared by fog.ts and heatmap.ts so the boundary
 * can't drift between the layers that switch on it. Adopted starting bands, tunable
 * visually — the same "adopted, not derived" convention internal/fog/raster.go's own
 * blur/feather constants use — not scientifically derived numbers.
 *
 * CITY_MIN_ZOOM is also the zoom the fog/heatmap raster layers start painting at. Normal
 * mode's tracks don't use it — they have no Country/Region fallback, so they draw from a much
 * lower zoom of their own (tracks.ts's TRACKS_MIN_ZOOM).
 */
export const COUNTRY_MAX_ZOOM = 5;
export const REGION_MIN_ZOOM = 5;
export const REGION_MAX_ZOOM = 8;
export const CITY_MIN_ZOOM = 8;
