import type { Map as MapLibreMap } from 'maplibre-gl';
import { BIKE_PATH_LAYER_IDS, TRACK_LAYER_IDS, TRAIL_LAYER_IDS, type PathOverlays } from './style';

/**
 * The Overlays menu's Trails, Tracks and Bike paths (docs/SPEC.md FR-4.13, overlays.ts): shows or
 * hides the basemap's three kinds of path layer (style.ts). Diffs before setting, like mapMode.ts's
 * setVisible: reattachOverlays calls this on every 'styledata', and an unconditional
 * setLayoutProperty would keep re-firing it (docs/DEVELOPMENT.md). A no-op for a layer the
 * current style doesn't have.
 */
export function setPathsVisible(map: MapLibreMap, paths: PathOverlays): void {
  for (const [ids, on] of [
    [TRAIL_LAYER_IDS, paths.trails],
    [TRACK_LAYER_IDS, paths.tracks],
    [BIKE_PATH_LAYER_IDS, paths.bikePaths],
  ] as const) {
    const next = on ? 'visible' : 'none';
    for (const id of ids) {
      if (!map.getLayer(id)) continue;
      if (map.getLayoutProperty(id, 'visibility') === next) continue;
      map.setLayoutProperty(id, 'visibility', next);
    }
  }
}
