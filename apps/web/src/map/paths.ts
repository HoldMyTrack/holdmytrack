import type { Map as MapLibreMap } from 'maplibre-gl';
import { setBikePathsVisible } from './bikePaths';
import { FOG_LAYER_ID } from './fog';
import { PATH_LAYER_IDS, TRACK_LAYER_IDS, TRAIL_LAYER_IDS, type PathOverlays } from './style';

/**
 * The Layers menu's Trails, Tracks, Bike paths and Shared paths (docs/SPEC.md FR-4.13,
 * overlays.ts): shows or hides the basemap's trail and track layers (style.ts) and the bike-path
 * overlay's two (bikePaths.ts). Diffs before setting, like mapMode.ts's setVisible:
 * reattachOverlays calls this on every 'styledata', and an unconditional setLayoutProperty would
 * keep re-firing it (docs/DEVELOPMENT.md). A no-op for a layer the map doesn't have.
 */
export function setPathsVisible(map: MapLibreMap, paths: PathOverlays): void {
  for (const [ids, on] of [
    [TRAIL_LAYER_IDS, paths.trails],
    [TRACK_LAYER_IDS, paths.tracks],
  ] as const) {
    const next = on ? 'visible' : 'none';
    for (const id of ids) {
      if (!map.getLayer(id)) continue;
      if (map.getLayoutProperty(id, 'visibility') === next) continue;
      map.setLayoutProperty(id, 'visibility', next);
    }
  }
  setBikePathsVisible(map, paths);
}

/**
 * Moves the basemap's trail and track layers up from among the road layers to `beforeId`, the
 * overlays' insertion point (layers.ts), so they paint over Fog's veil and Heatmap's heat
 * rather than under them (IMPLEMENTATION.md §4.24). Call it after Fog and Heatmap are added and
 * before the bike-path layers and the activity tracks, which then stack over the trails.
 *
 * Only a layer still below the fog raster moves: reattachOverlays calls this on every
 * `styledata`, and moving a layer that's already in place would fire another (docs/DEVELOPMENT.md).
 * A style swap puts the layers back among the roads, and the next call lifts them again.
 */
export function raisePathLayers(map: MapLibreMap, beforeId: string | undefined): void {
  const order = map.getStyle().layers.map((layer) => layer.id);
  const fog = order.indexOf(FOG_LAYER_ID);
  if (fog < 0) return;
  for (const id of PATH_LAYER_IDS) {
    const at = order.indexOf(id);
    if (at >= 0 && at < fog) map.moveLayer(id, beforeId);
  }
}
