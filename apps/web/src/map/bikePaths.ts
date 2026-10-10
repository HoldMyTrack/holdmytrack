import type { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, TILES_V1 } from '../api';
import { versionedTileURL } from './coverageVersion';
import { PATH_COLORS, type Flavor } from './style';

/**
 * The Layers menu's Bike paths and Shared paths (docs/SPEC.md FR-4.13, IMPLEMENTATION.md §4.24):
 * OpenStreetMap's cycleways and bike-designated paths from `/tiles/v1/bike-paths`, not the
 * basemap, which has none below zoom 13. Drawn from zoom 9, so a whole region's bike network
 * shows at once.
 *
 * Added at the same insertion point as the other overlays (layers.ts), after Fog and Heatmap and
 * before the activity tracks, so they paint over the veil and the heat, the very places a rider
 * looking for unridden paths needs to see them, and under the rider's own tracks.
 */
export const BIKE_PATHS_SOURCE_ID = 'bike-paths';
export const BIKE_PATHS_CYCLEWAY_LAYER_ID = 'bike-paths-cycleway';
export const BIKE_PATHS_SHARED_LAYER_ID = 'bike-paths-shared';
/** Bottom to top: a shared path under a cycleway where the two meet. */
export const BIKE_PATHS_LAYER_IDS = [BIKE_PATHS_SHARED_LAYER_ID, BIKE_PATHS_CYCLEWAY_LAYER_ID] as const;

/** Where the tiles start (internal/httpapi's bikePathsMinZoom): a region on the screen. */
export const BIKE_PATHS_MIN_ZOOM = 9;
/** Past it the tiles serve every zoom above by overzooming, like the spots tiles. */
const BIKE_PATHS_MAX_ZOOM = 14;
const BIKE_PATHS_SOURCE_LAYER = 'bike_paths';
const BIKE_PATHS_TILE_URL = `${API_BASE_URL}${TILES_V1}/bike-paths/{z}/{x}/{y}.mvt`;

/** Which of the two are showing. */
export interface BikePathOverlays {
  bikePaths: boolean;
  sharedPaths: boolean;
}

/**
 * Adds the source and both layers if they aren't already there, hidden — idempotent, since
 * `styledata` re-runs it after every theme swap, which is also when the colors follow the new
 * flavor. setBikePathsVisible shows them.
 */
export function ensureBikePathLayers(map: MapLibreMap, beforeId: string | undefined, flavor: Flavor): void {
  if (!map.getSource(BIKE_PATHS_SOURCE_ID)) {
    map.addSource(BIKE_PATHS_SOURCE_ID, {
      type: 'vector',
      tiles: [versionedTileURL(BIKE_PATHS_TILE_URL)],
      minzoom: BIKE_PATHS_MIN_ZOOM,
      maxzoom: BIKE_PATHS_MAX_ZOOM,
    });
  }
  const colors = PATH_COLORS[flavor];
  if (!map.getLayer(BIKE_PATHS_SHARED_LAYER_ID)) {
    map.addLayer(
      {
        id: BIKE_PATHS_SHARED_LAYER_ID,
        type: 'line',
        source: BIKE_PATHS_SOURCE_ID,
        'source-layer': BIKE_PATHS_SOURCE_LAYER,
        minzoom: BIKE_PATHS_MIN_ZOOM,
        filter: ['==', ['get', 'kind'], 'shared'],
        layout: { visibility: 'none' },
        paint: {
          'line-color': colors.shared,
          // Dashed like a trail, since people walk it too; a cycleway is solid.
          'line-dasharray': [2, 1],
          'line-width': ['interpolate', ['exponential', 1.6], ['zoom'], BIKE_PATHS_MIN_ZOOM, 1.5, 13, 2, 18, 4.5],
        },
      },
      beforeId,
    );
  }
  if (!map.getLayer(BIKE_PATHS_CYCLEWAY_LAYER_ID)) {
    map.addLayer(
      {
        id: BIKE_PATHS_CYCLEWAY_LAYER_ID,
        type: 'line',
        source: BIKE_PATHS_SOURCE_ID,
        'source-layer': BIKE_PATHS_SOURCE_LAYER,
        minzoom: BIKE_PATHS_MIN_ZOOM,
        filter: ['==', ['get', 'kind'], 'cycleway'],
        layout: { visibility: 'none', 'line-cap': 'round', 'line-join': 'round' },
        paint: {
          'line-color': colors.cycleway,
          'line-width': ['interpolate', ['exponential', 1.6], ['zoom'], BIKE_PATHS_MIN_ZOOM, 1.8, 13, 2.4, 18, 5.5],
        },
      },
      beforeId,
    );
  }
}

/** Shows or hides each kind. Diffs before setting, like paths.ts's setPathsVisible, since
 *  reattachOverlays calls it on every `styledata`. A no-op before ensureBikePathLayers. */
export function setBikePathsVisible(map: MapLibreMap, shown: BikePathOverlays): void {
  for (const [id, on] of [
    [BIKE_PATHS_CYCLEWAY_LAYER_ID, shown.bikePaths],
    [BIKE_PATHS_SHARED_LAYER_ID, shown.sharedPaths],
  ] as const) {
    if (!map.getLayer(id)) continue;
    const next = on ? 'visible' : 'none';
    if (map.getLayoutProperty(id, 'visibility') === next) continue;
    map.setLayoutProperty(id, 'visibility', next);
  }
}
