import type { Map as MapLibreMap } from 'maplibre-gl';
import { FOG_LAYER_ID } from './fog';
import { PATH_LAYER_IDS } from './style';

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
