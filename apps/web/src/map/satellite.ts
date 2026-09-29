import type { Map as MapLibreMap } from 'maplibre-gl';
import { SATELLITE_LAYER_ID, satelliteHiddenLayerIds } from './style';

/**
 * The Overlays menu's Base map switch (docs/SPEC.md FR-4.14, overlays.ts): shows the satellite
 * imagery and hides the basemap's background and area fills over it, or the reverse — roads,
 * boundaries and labels stay either way. Diffs before setting, like paths.ts: reattachOverlays
 * calls this on every 'styledata', and an unconditional setLayoutProperty would keep re-firing
 * it (docs/DEVELOPMENT.md). A no-op when the style has no imagery (no deployment config).
 */
export function setSatelliteVisible(map: MapLibreMap, on: boolean): void {
  if (!map.getLayer(SATELLITE_LAYER_ID)) return;
  const set = (id: string, visible: boolean) => {
    const next = visible ? 'visible' : 'none';
    if (map.getLayoutProperty(id, 'visibility') !== next) map.setLayoutProperty(id, 'visibility', next);
  };
  set(SATELLITE_LAYER_ID, on);
  for (const id of satelliteHiddenLayerIds(map.getStyle().layers)) set(id, !on);
}
