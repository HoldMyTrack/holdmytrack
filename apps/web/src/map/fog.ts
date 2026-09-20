import type { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, TILES_V1 } from '../api';

/**
 * The Fog of War raster layer (IMPLEMENTATION.md §4.2). Unlike tracks, Fog of War is never
 * scoped by the date range/TYPE/DISTANCE/hidden-track state that drives Normal mode — it
 * shows true all-time coverage, unconditionally (a place once cleared stays cleared, which is
 * the whole point of the mechanic). There is therefore no query to build or refresh: one
 * fixed tile URL, set once and never changed. See heatmap.ts, which has its own fixed URL for
 * the same reason, just a different fixed rolling window computed server-side.
 */
export const FOG_SOURCE_ID = 'fog';
export const FOG_LAYER_ID = 'fog-raster';

const FOG_TILE_URL = `${API_BASE_URL}${TILES_V1}/fog/{z}/{x}/{y}.png`;

/**
 * Adds the fog source and layer if not already present — idempotent for the same reason
 * ensureTrackLayer is (tracks.ts): `styledata` fires on every theme setStyle, which
 * discards custom layers, so this has to be safe to call repeatedly.
 *
 * `beforeId` is the same insertion point tracks uses (layers.ts) — beneath the basemap's
 * first symbol layer, so place labels stay legible over the fog (§4.2's own reasoning).
 * Layer order between fog and tracks matters too: fog is added first here and MapView adds
 * tracks after it, both with the same `beforeId`, which makes tracks paint *above* fog —
 * a cleared route should be visible through the veil, not hidden under it.
 */
export function ensureFogLayer(map: MapLibreMap, beforeId: string | undefined): void {
  if (!map.getSource(FOG_SOURCE_ID)) {
    map.addSource(FOG_SOURCE_ID, {
      type: 'raster',
      tiles: [FOG_TILE_URL],
      tileSize: 512,
      minzoom: 0,
      maxzoom: 14,
    });
  }
  if (!map.getLayer(FOG_LAYER_ID)) {
    map.addLayer(
      {
        id: FOG_LAYER_ID,
        type: 'raster',
        source: FOG_SOURCE_ID,
        layout: { visibility: 'none' }, // starts hidden — Normal is the default mode
      },
      beforeId,
    );
  }
}
