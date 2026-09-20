/**
 * The three zoom-dependent tiers Fog and Heatmap fall back to below city zoom
 * (IMPLEMENTATION.md §4.2.4) — shared by fog.ts, heatmap.ts and tracks.ts so the boundary
 * can't drift between the layers that switch on it. Adopted starting bands, tunable
 * visually — the same "adopted, not derived" convention internal/fog/raster.go's own
 * blur/feather constants use — not scientifically derived numbers.
 *
 * CITY_MIN_ZOOM doubles as the zoom the fog/heatmap raster layers and the tracks line layer
 * all start painting at — finally implementing IMPLEMENTATION.md §5.3's previously
 * undocumented-as-built "below roughly z8 tracks are hidden entirely" claim, at the same
 * threshold this feature introduces rather than a second, disconnected one.
 */
export const COUNTRY_MAX_ZOOM = 5;
export const REGION_MIN_ZOOM = 5;
export const REGION_MAX_ZOOM = 8;
export const CITY_MIN_ZOOM = 8;
