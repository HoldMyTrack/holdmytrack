import type { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, TILES_V1 } from '../api';
import { versionedTileURL } from './coverageVersion';
import { HEAT_POINTS_INTENSITY, HEAT_POINTS_MAX_ZOOM, HEAT_RAMP, heatPointsRadius } from './heatPoints';

/**
 * The Heatmap layer (IMPLEMENTATION.md §4.2.2): the server's heat-points tiles, weighted points
 * made from each account's pass counts, drawn as MapLibre heatmap circles at every zoom — a
 * place far from home is a small circle across the globe, a street a soft line close in.
 * Unlike Fog of War's true all-time coverage, Heatmap answers "where do I go *now*" — a fixed
 * rolling window (services/server/internal/fog's HeatmapWindowDays), computed server-side and
 * never client-supplied, so there is no query to build here: one fixed tile URL.
 */
export const HEAT_POINTS_SOURCE_ID = 'heat-points';
export const HEAT_POINTS_LAYER_ID = 'heat-points-circles';
export const HEATMAP_DIM_LAYER_ID = 'heatmap-dim';

const HEAT_POINTS_TILE_URL = `${API_BASE_URL}${TILES_V1}/heat-points/{z}/{x}/{y}.mvt`;

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
 * basemap's first symbol layer, so labels stay legible over the heat too.
 *
 * `dark` picks the dim wash under the heat (style.ts's isDarkBase, as for the fog veil).
 */
export function ensureHeatmapLayer(map: MapLibreMap, beforeId: string | undefined, dark: boolean): void {
  // The wash first, so the heat added after it — before `beforeId` too — lands above it. On a
  // theme swap its colour follows.
  const dim = dark ? DARK_DIM : LIGHT_DIM;
  if (!map.getLayer(HEATMAP_DIM_LAYER_ID)) {
    map.addLayer(
      {
        id: HEATMAP_DIM_LAYER_ID,
        type: 'background',
        paint: { 'background-color': dim.color, 'background-opacity': dim.opacity },
        layout: { visibility: 'none' },
      },
      map.getLayer(HEAT_POINTS_LAYER_ID) ? HEAT_POINTS_LAYER_ID : beforeId,
    );
  } else if (map.getPaintProperty(HEATMAP_DIM_LAYER_ID, 'background-color') !== dim.color) {
    map.setPaintProperty(HEATMAP_DIM_LAYER_ID, 'background-color', dim.color);
    map.setPaintProperty(HEATMAP_DIM_LAYER_ID, 'background-opacity', dim.opacity);
  }

  if (!map.getSource(HEAT_POINTS_SOURCE_ID)) {
    map.addSource(HEAT_POINTS_SOURCE_ID, {
      type: 'vector',
      tiles: [versionedTileURL(HEAT_POINTS_TILE_URL)],
      minzoom: 0,
      maxzoom: HEAT_POINTS_MAX_ZOOM,
    });
  }
  if (!map.getLayer(HEAT_POINTS_LAYER_ID)) {
    map.addLayer(
      {
        id: HEAT_POINTS_LAYER_ID,
        type: 'heatmap',
        source: HEAT_POINTS_SOURCE_ID,
        'source-layer': 'heat',
        layout: { visibility: 'none' }, // starts hidden — Normal is the default mode
        paint: {
          'heatmap-weight': ['get', 'w'],
          'heatmap-intensity': HEAT_POINTS_INTENSITY,
          'heatmap-radius': heatPointsRadius() as never,
          'heatmap-color': ['interpolate', ['linear'], ['heatmap-density'], ...HEAT_RAMP.flat()] as never,
        },
      },
      beforeId,
    );
  }
}

/** Re-points the heat source at the current coverage version (coverageVersion.ts), so MapLibre
 *  refetches every tile — the caller sets the new version first. A no-op before the source is
 *  on the map; ensureHeatmapLayer will create it at the current version. */
export function refreshHeatmapLayers(map: MapLibreMap): void {
  (map.getSource(HEAT_POINTS_SOURCE_ID) as { setTiles?: (tiles: string[]) => unknown } | undefined)?.setTiles?.([
    versionedTileURL(HEAT_POINTS_TILE_URL),
  ]);
}
