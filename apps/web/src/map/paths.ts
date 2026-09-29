import type { Map as MapLibreMap } from 'maplibre-gl';
import { PATH_LAYER_IDS } from './style';

/**
 * The map's Trails & bike paths toggle (docs/SPEC.md FR-4.5): shows or hides the basemap's
 * path layers (style.ts `PATH_LAYER_IDS`). A per-device choice, off until turned on, kept in
 * localStorage the way the theme is — a view preference, not something on the account.
 */

const STORAGE_KEY = 'hmt.showPaths';

export function loadShowPaths(): boolean {
  try {
    return window.localStorage.getItem(STORAGE_KEY) === '1';
  } catch {
    return false;
  }
}

export function saveShowPaths(on: boolean): void {
  try {
    if (on) window.localStorage.setItem(STORAGE_KEY, '1');
    else window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Storage blocked (private mode, disabled site data): the toggle still works for this page.
  }
}

/** Diffs before setting, like mapMode.ts's setVisible: reattachOverlays calls this on every
 *  'styledata', and an unconditional setLayoutProperty would keep re-firing it
 *  (docs/DEVELOPMENT.md). A no-op for a layer the current style doesn't have. */
export function setPathsVisible(map: MapLibreMap, on: boolean): void {
  const next = on ? 'visible' : 'none';
  for (const id of PATH_LAYER_IDS) {
    if (!map.getLayer(id)) continue;
    if (map.getLayoutProperty(id, 'visibility') === next) continue;
    map.setLayoutProperty(id, 'visibility', next);
  }
}
