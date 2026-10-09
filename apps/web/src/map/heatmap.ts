import type { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, TILES_V1 } from '../api';
import { versionedTileURL } from './coverageVersion';
import { BOUNDARIES_ATTRIBUTION, CITY_MIN_ZOOM, COUNTRY_MAX_ZOOM, PIXELS_MAX_ZOOM, REGION_MAX_ZOOM, REGION_MIN_ZOOM } from './zoomTiers';

/**
 * The Heatmap raster layer (IMPLEMENTATION.md §4.2.2) — mirrors fog.ts in every way that's
 * shared (tile source shape, same insertion point), differing only in which endpoint it
 * points at. Unlike Fog of War's true all-time coverage, Heatmap answers "where do I go
 * *now*" — a fixed rolling window (services/server/internal/fog's HeatmapWindowDays),
 * computed server-side and never client-supplied, so this still has no query to build or
 * refresh here either: one fixed tile URL, set once and never changed.
 *
 * Below city zoom (§4.2.4), this per-pixel raster is replaced by the Country/Region fill
 * layers below — a flat "you've been here" highlight, not graded by how much, since a
 * whole-country intensity gradient would be a second scoring dimension nobody asked for.
 */
export const HEATMAP_SOURCE_ID = 'heatmap';
export const HEATMAP_LAYER_ID = 'heatmap-raster';
export const COUNTRY_HEATMAP_SOURCE_ID = 'country-heatmap';
export const COUNTRY_HEATMAP_LAYER_ID = 'country-heatmap-fill';
export const REGION_HEATMAP_SOURCE_ID = 'region-heatmap';
export const REGION_HEATMAP_LAYER_ID = 'region-heatmap-fill';
export const HEATMAP_DIM_LAYER_ID = 'heatmap-dim';

const HEATMAP_TILE_URL = `${API_BASE_URL}${TILES_V1}/heatmap/{z}/{x}/{y}.png`;
const COUNTRY_HEATMAP_TILE_URL = `${API_BASE_URL}${TILES_V1}/country-heatmap/{z}/{x}/{y}.mvt`;
const REGION_HEATMAP_TILE_URL = `${API_BASE_URL}${TILES_V1}/region-heatmap/{z}/{x}/{y}.mvt`;

// The heatmap ramp's own base colour (internal/fog/raster.go's heatmapRamp, the deep red a
// single visit is drawn in), so nothing shifts colour crossing into city zoom, at a fixed
// opacity: "you've been somewhere in this country," not graded by how much.
const HEATMAP_FILL_COLOR = '#b3261e';
const HEATMAP_FILL_OPACITY = 0.55;

// Heatmap mode's wash over the basemap, beneath the heat: the heat reads against a quieter map,
// as Fog mode quiets the labels (mapMode.ts). Cream on the light flavors, black on the dark
// ones — the fog veils' own pairing, lightened.
const LIGHT_DIM = { color: '#f7f4ec', opacity: 0.45 };
const DARK_DIM = { color: '#000000', opacity: 0.35 };

/**
 * Adds the heatmap source and layer if not already present — idempotent for the same
 * `styledata`-discards-custom-layers reason ensureFogLayer and ensureTrackLayer are.
 *
 * `beforeId` is the same insertion point fog and tracks use (layers.ts) — beneath the
 * basemap's first symbol layer, so labels stay legible over the glow too.
 *
 * `dark` picks the dim wash under the heat (style.ts's isDarkBase, as for the fog veil).
 */
export function ensureHeatmapLayer(map: MapLibreMap, beforeId: string | undefined, dark: boolean): void {
  // The wash first, so every heat layer added after it — each before `beforeId` too — lands
  // above it. On a theme swap its colour follows.
  const dim = dark ? DARK_DIM : LIGHT_DIM;
  if (!map.getLayer(HEATMAP_DIM_LAYER_ID)) {
    const firstHeat = [COUNTRY_HEATMAP_LAYER_ID, REGION_HEATMAP_LAYER_ID, HEATMAP_LAYER_ID].find((id) => map.getLayer(id));
    map.addLayer(
      {
        id: HEATMAP_DIM_LAYER_ID,
        type: 'background',
        paint: { 'background-color': dim.color, 'background-opacity': dim.opacity },
        layout: { visibility: 'none' },
      },
      firstHeat ?? beforeId,
    );
  } else if (map.getPaintProperty(HEATMAP_DIM_LAYER_ID, 'background-color') !== dim.color) {
    map.setPaintProperty(HEATMAP_DIM_LAYER_ID, 'background-color', dim.color);
    map.setPaintProperty(HEATMAP_DIM_LAYER_ID, 'background-opacity', dim.opacity);
  }

  if (!map.getSource(HEATMAP_SOURCE_ID)) {
    map.addSource(HEATMAP_SOURCE_ID, {
      type: 'raster',
      tiles: [versionedTileURL(HEATMAP_TILE_URL)],
      tileSize: 512,
      minzoom: 0,
      maxzoom: 14,
    });
  }
  if (!map.getLayer(HEATMAP_LAYER_ID)) {
    map.addLayer(
      {
        id: HEATMAP_LAYER_ID,
        type: 'raster',
        source: HEATMAP_SOURCE_ID,
        minzoom: CITY_MIN_ZOOM,
        layout: { visibility: 'none' }, // starts hidden — Normal is the default mode
        // Square pixels below z14, where each tile pixel shows 8 screen pixels wide however far out
        // the map is; smoothed from z14, where there's no finer tile and the z14 one is stretched
        // further with every zoom (ADR-0038).
        paint: { 'raster-resampling': ['step', ['zoom'], 'nearest', PIXELS_MAX_ZOOM, 'linear'] },
      },
      beforeId,
    );
  }

  if (!map.getSource(COUNTRY_HEATMAP_SOURCE_ID)) {
    map.addSource(COUNTRY_HEATMAP_SOURCE_ID, {
      type: 'vector',
      tiles: [versionedTileURL(COUNTRY_HEATMAP_TILE_URL)],
      attribution: BOUNDARIES_ATTRIBUTION,
      minzoom: 0,
      maxzoom: COUNTRY_MAX_ZOOM,
    });
  }
  if (!map.getLayer(COUNTRY_HEATMAP_LAYER_ID)) {
    map.addLayer(
      {
        id: COUNTRY_HEATMAP_LAYER_ID,
        type: 'fill',
        source: COUNTRY_HEATMAP_SOURCE_ID,
        'source-layer': 'countries',
        minzoom: 0,
        maxzoom: COUNTRY_MAX_ZOOM,
        paint: { 'fill-color': HEATMAP_FILL_COLOR, 'fill-opacity': HEATMAP_FILL_OPACITY },
        layout: { visibility: 'none' },
      },
      beforeId,
    );
  }

  if (!map.getSource(REGION_HEATMAP_SOURCE_ID)) {
    map.addSource(REGION_HEATMAP_SOURCE_ID, {
      type: 'vector',
      tiles: [versionedTileURL(REGION_HEATMAP_TILE_URL)],
      attribution: BOUNDARIES_ATTRIBUTION,
      minzoom: REGION_MIN_ZOOM,
      maxzoom: REGION_MAX_ZOOM,
    });
  }
  if (!map.getLayer(REGION_HEATMAP_LAYER_ID)) {
    map.addLayer(
      {
        id: REGION_HEATMAP_LAYER_ID,
        type: 'fill',
        source: REGION_HEATMAP_SOURCE_ID,
        'source-layer': 'regions',
        minzoom: REGION_MIN_ZOOM,
        maxzoom: REGION_MAX_ZOOM,
        paint: { 'fill-color': HEATMAP_FILL_COLOR, 'fill-opacity': HEATMAP_FILL_OPACITY },
        layout: { visibility: 'none' },
      },
      beforeId,
    );
  }
}

/** Re-points this mode's sources at the current coverage version (coverageVersion.ts), so
 *  MapLibre refetches every tile — the caller sets the new version first. A no-op for a source
 *  that isn't on the map yet; ensureHeatmapLayer will create it at the current version. */
export function refreshHeatmapLayers(map: MapLibreMap): void {
  for (const [sourceId, url] of [
    [HEATMAP_SOURCE_ID, HEATMAP_TILE_URL],
    [COUNTRY_HEATMAP_SOURCE_ID, COUNTRY_HEATMAP_TILE_URL],
    [REGION_HEATMAP_SOURCE_ID, REGION_HEATMAP_TILE_URL],
  ] as const) {
    (map.getSource(sourceId) as { setTiles?: (tiles: string[]) => unknown } | undefined)?.setTiles?.([versionedTileURL(url)]);
  }
}
