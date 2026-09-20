import type { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, TILES_V1 } from '../api';

/**
 * The Heatmap raster layer (IMPLEMENTATION.md §4.2.2) — mirrors fog.ts in every way that's
 * shared (tile source shape, same insertion point), differing only in which endpoint it
 * points at. Unlike Fog of War's true all-time coverage, Heatmap answers "where do I go
 * *now*" — a fixed rolling window (services/server/internal/fog's HeatmapWindowDays),
 * computed server-side and never client-supplied, so this still has no query to build or
 * refresh here either: one fixed tile URL, set once and never changed.
 */
export const HEATMAP_SOURCE_ID = 'heatmap';
export const HEATMAP_LAYER_ID = 'heatmap-raster';

const HEATMAP_TILE_URL = `${API_BASE_URL}${TILES_V1}/heatmap/{z}/{x}/{y}.png`;

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
        layout: { visibility: 'none' }, // starts hidden — Normal is the default mode
      },
      beforeId,
    );
  }
}
