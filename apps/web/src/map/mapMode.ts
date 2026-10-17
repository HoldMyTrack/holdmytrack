import type { Map as MapLibreMap } from 'maplibre-gl';
import { setCoverageView } from './coverageLayers';
import { BAND_LAYER_ID } from './trackBands';
import { TRACKS_CASING_LAYER_ID, TRACKS_LAYER_ID } from './tracks';

/**
 * The three mutually exclusive views (docs/SPEC.md FR-4.4) — not
 * independent checkboxes, a single toggle. What Fog and Heatmap show, and how they quiet the
 * basemap, is the served style's (coverageStyle.ts); this adds the app's own layers, which
 * both modes hide: the coverage already shows where, and the drawn lines on top of either one
 * read as visual noise rather than adding information, reported live as ruining the effect.
 */
export type MapMode = 'normal' | 'fog' | 'heatmap';

/**
 * Applies `mode` to the overlays and the tracks. A no-op for whichever layer isn't on the map yet
 * (relevant right after a styledata swap, before reattachOverlays has re-added everything)
 * rather than throwing. `satellite` picks the overlays' view over imagery.
 *
 * `editingTrack` is the Edit track session (§4.7.7): every other activity disappears while
 * one is being edited, and the edited one is drawn by its own overlay (trackEdit.ts), so the
 * shared tracks layer and its bands are hidden outright rather than filtered.
 */
export function setMapMode(map: MapLibreMap, mode: MapMode, satellite: boolean, editingTrack = false): void {
  setCoverageView(map, mode, satellite);
  const tracks = mode === 'normal' && !editingTrack;
  setVisible(map, TRACKS_LAYER_ID, tracks);
  setVisible(map, TRACKS_CASING_LAYER_ID, tracks);
  // FR-4.8: a focused activity's pace-colored segments are a second layer over the shared
  // tracks layer (trackBands.ts), so leaving Normal has to hide it too.
  setVisible(map, BAND_LAYER_ID, tracks);
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
