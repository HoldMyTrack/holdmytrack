import type { Map as MapLibreMap } from 'maplibre-gl';
import { COUNTRY_FOG_LAYER_ID, FOG_LAYER_ID, REGION_FOG_LAYER_ID } from './fog';
import { COUNTRY_HEATMAP_LAYER_ID, HEATMAP_LAYER_ID, REGION_HEATMAP_LAYER_ID } from './heatmap';
import { BAND_LAYER_ID } from './trackBands';
import { TRACKS_LAYER_ID } from './tracks';

/**
 * The three mutually-exclusive views IMPLEMENTATION.md §4.2.2 names — not
 * independent checkboxes, a single toggle. Lives here rather than in fog.ts or heatmap.ts
 * because it has to coordinate all three overlay layers (fog, heatmap, tracks) at once:
 * both Fog and Heatmap hide the track layer (§4.2.2 — the raster already encodes where, and
 * the drawn lines on top of either one read as visual noise rather than adding information,
 * reported live as ruining the effect), which neither fog.ts nor heatmap.ts alone could
 * express without reaching into a layer they don't own.
 */
export type MapMode = 'normal' | 'fog' | 'heatmap';

/**
 * Applies visibility for all three overlay layers per mode. A no-op for whichever layer
 * isn't on the map yet (relevant right after a styledata swap, before reattachOverlays has
 * re-added everything) rather than throwing.
 *
 * `editingTrack` is the Edit track session (§4.7.7): every other activity disappears while
 * one is being edited, and the edited one is drawn by its own overlay (trackEdit.ts), so the
 * shared tracks layer and its bands are hidden outright rather than filtered.
 */
export function setMapMode(map: MapLibreMap, mode: MapMode, editingTrack = false): void {
  setVisible(map, FOG_LAYER_ID, mode === 'fog');
  setVisible(map, COUNTRY_FOG_LAYER_ID, mode === 'fog');
  setVisible(map, REGION_FOG_LAYER_ID, mode === 'fog');
  setVisible(map, HEATMAP_LAYER_ID, mode === 'heatmap');
  setVisible(map, COUNTRY_HEATMAP_LAYER_ID, mode === 'heatmap');
  setVisible(map, REGION_HEATMAP_LAYER_ID, mode === 'heatmap');
  // Tracks stay visible only in Normal — both Fog and Heatmap hide them (§4.2.2).
  setVisible(map, TRACKS_LAYER_ID, mode === 'normal' && !editingTrack);
  // FR-4.8: a focused activity's colored zone segments are a second layer over the shared
  // tracks layer (trackBands.ts) — not covered by the tracks toggle above — so switching to
  // Fog/Heatmap has to hide it too, or it keeps rendering over the raster.
  setVisible(map, BAND_LAYER_ID, mode === 'normal' && !editingTrack);
}

function setVisible(map: MapLibreMap, layerId: string, visible: boolean): void {
  if (!map.getLayer(layerId)) return;
  const next = visible ? 'visible' : 'none';
  // map.setLayoutProperty (unlike the style-internal one) calls _update(true)
  // unconditionally, marking sources dirty and forcing a source re-check on the next
  // render frame regardless of whether anything actually changed. reattachOverlays calls
  // this on every 'styledata' event, so a bare pass-through here self-sustains forever:
  // each re-render re-fires 'styledata', which calls this again, which dirties again.
  // Skipping the call when the value is already correct breaks that loop.
  if (map.getLayoutProperty(layerId, 'visibility') === next) return;
  map.setLayoutProperty(layerId, 'visibility', next);
}
