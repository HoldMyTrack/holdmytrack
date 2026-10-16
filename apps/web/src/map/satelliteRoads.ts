import type { MapMode } from './mapMode';

/**
 * Whether the basemap's roads come off the map: over satellite imagery in Fog and Heatmap. There
 * the picture is the ground and what the user covered of it, and roads drawn over both, veil
 * or no veil, read as a second map on top. Normal keeps them, see-through, as the way through
 * the imagery (style.ts's `SATELLITE_ROAD_OPACITY`).
 */
export function roadsHiddenOverImagery(satellite: boolean, mode: MapMode): boolean {
  return satellite && mode !== 'normal';
}

/**
 * The layers `roadsHiddenOverImagery` takes off: every basemap layer named `roads_*` — the lines
 * with their casings, bridges, tunnels, rail and one-way arrows, and the street names and shields
 * that would otherwise float over nothing. The trails (`paths_*`) and place labels stay.
 */
export function satelliteRoadLayerIds(layers: readonly { id: string; source?: unknown }[], basemapSource: string): string[] {
  return layers.filter((layer) => layer.source === basemapSource && layer.id.startsWith('roads_')).map((layer) => layer.id);
}
