import type { Map as MapLibreMap, RasterTileSource } from 'maplibre-gl';
import { API_BASE_URL, TILES_V1 } from '../api';
import { maskTileURL, type MaskQuery } from './fog';

/**
 * The Heatmap raster layer (IMPLEMENTATION.md §4.2.2) — mirrors fog.ts in
 * every way that's shared (tile source shape, filter query params, refresh primitive, same
 * insertion point — see fog.ts's own doc comment for the unfiltered/filtered split), differing
 * only in which endpoint it points at and that it starts hidden the same way fog does, since
 * Normal is the default mode for either overlay.
 */
export const HEATMAP_SOURCE_ID = 'heatmap';
export const HEATMAP_LAYER_ID = 'heatmap-raster';

function heatmapTileURL(query: MaskQuery): string {
  return maskTileURL(`${API_BASE_URL}${TILES_V1}/heatmap/`, query);
}

/**
 * Adds the heatmap source and layer if not already present — idempotent for the same
 * `styledata`-discards-custom-layers reason ensureFogLayer and ensureTrackLayer are.
 *
 * `beforeId` is the same insertion point fog and tracks use (layers.ts) — beneath the
 * basemap's first symbol layer, so labels stay legible over the glow too. `query` seeds the
 * *initial* tiles URL, same convention as ensureFogLayer/ensureTrackLayer — changing it on an
 * already-existing source is refreshHeatmapLayer's job instead.
 */
export function ensureHeatmapLayer(map: MapLibreMap, beforeId: string | undefined, query: MaskQuery): void {
  if (!map.getSource(HEATMAP_SOURCE_ID)) {
    map.addSource(HEATMAP_SOURCE_ID, {
      type: 'raster',
      tiles: [heatmapTileURL(query)],
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

/** Forces MapLibre to refetch every currently-loaded heatmap tile against the current filter
 *  — see fog.ts's refreshFogLayer, the same primitive for the same reason. */
export function refreshHeatmapLayer(map: MapLibreMap, query: MaskQuery): void {
  const source = map.getSource(HEATMAP_SOURCE_ID) as RasterTileSource | undefined;
  source?.setTiles([heatmapTileURL(query)]);
}
