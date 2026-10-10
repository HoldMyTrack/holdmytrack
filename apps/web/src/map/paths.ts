import type { Map as MapLibreMap } from 'maplibre-gl';
import { setBikePathsVisible } from './bikePaths';
import { TRACK_LAYER_IDS, TRAIL_LAYER_IDS, type PathOverlays } from './style';

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