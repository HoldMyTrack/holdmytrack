import type { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, TILES_V1 } from '../api';
import { CITY_MIN_ZOOM, COUNTRY_MAX_ZOOM, REGION_MAX_ZOOM, REGION_MIN_ZOOM } from './zoomTiers';

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

const HEATMAP_TILE_URL = `${API_BASE_URL}${TILES_V1}/heatmap/{z}/{x}/{y}.png`;
const COUNTRY_HEATMAP_TILE_URL = `${API_BASE_URL}${TILES_V1}/country-heatmap/{z}/{x}/{y}.mvt`;
const REGION_HEATMAP_TILE_URL = `${API_BASE_URL}${TILES_V1}/region-heatmap/{z}/{x}/{y}.mvt`;

// The heatmap ramp's own base hue (internal/fog/raster.go's heatmapRamp zero-stop, also
// tracks.ts's TRACK_COLOR — a single visit and a low-intensity heatmap cell read as the same
// colour everywhere else in the app) at a fixed moderate opacity: "you've been somewhere in
// this country," not graded by how much.
const HEATMAP_FILL_COLOR = '#b07e2e';
const HEATMAP_FILL_OPACITY = 0.45;

/**
 * Adds the heatmap source and layer if not already present — idempotent for the same
 * `styledata`-discards-custom-layers reason ensureFogLayer and ensureTrackLayer are.
 *
 * `beforeId` is the same insertion point fog and tracks use (layers.ts) — beneath the
 * basemap's first symbol layer, so labels stay legible over the glow too.
 */
export function ensureHeatmapLayer(map: MapLibreMap, beforeId: string | undefined): void {
  if (!map.getSource(HEATMAP_SOURCE_ID)) {
    map.addSource(HEATMAP_SOURCE_ID, {
      type: 'raster',
      tiles: [HEATMAP_TILE_URL],
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
      },
      beforeId,
    );
  }

  if (!map.getSource(COUNTRY_HEATMAP_SOURCE_ID)) {
    map.addSource(COUNTRY_HEATMAP_SOURCE_ID, {
      type: 'vector',
      tiles: [COUNTRY_HEATMAP_TILE_URL],
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
      tiles: [REGION_HEATMAP_TILE_URL],
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
