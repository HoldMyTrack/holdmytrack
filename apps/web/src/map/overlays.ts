import type { SpotCategory } from './spots';
import { SPOT_CATEGORIES } from './spots';

/**
 * What the Overlays menu (OverlaysMenu.tsx) has switched on: the three kinds of path
 * (FR-4.13) and each Spots category (FR-15.2). A per-browser view preference, kept in
 * localStorage like the theme, not on the account; everything off until turned on.
 */
export interface Overlays {
  trails: boolean;
  /** Dirt, farm and forest roads — OSM's `highway=track`. */
  tracks: boolean;
  bikePaths: boolean;
  /** The Spots categories shown, in SPOT_CATEGORIES order. */
  spots: SpotCategory[];
}

const STORAGE_KEY = 'hmt.overlays';
// The two toggles the menu replaced: one for both kinds of path, one for every category.
const LEGACY_PATHS_KEY = 'hmt.showPaths';
const LEGACY_POI_KEY = 'hmt.showPoi';

export const NO_OVERLAYS: Overlays = { trails: false, tracks: false, bikePaths: false, spots: [] };

export function loadOverlays(): Overlays {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const saved = JSON.parse(raw) as Partial<Overlays>;
      return {
        trails: saved.trails === true,
        // Tracks were part of Trails before they had their own entry: a choice saved then
        // keeps showing them.
        tracks: typeof saved.tracks === 'boolean' ? saved.tracks : saved.trails === true,
        bikePaths: saved.bikePaths === true,
        spots: SPOT_CATEGORIES.filter((c) => Array.isArray(saved.spots) && saved.spots.includes(c)),
      };
    }
    const paths = window.localStorage.getItem(LEGACY_PATHS_KEY) === '1';
    const poi = window.localStorage.getItem(LEGACY_POI_KEY) === '1';
    return { trails: paths, tracks: paths, bikePaths: paths, spots: poi ? [...SPOT_CATEGORIES] : [] };
  } catch {
    return NO_OVERLAYS;
  }
}

export function saveOverlays(overlays: Overlays): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(overlays));
    window.localStorage.removeItem(LEGACY_PATHS_KEY);
    window.localStorage.removeItem(LEGACY_POI_KEY);
  } catch {
    // Storage blocked (private mode, disabled site data): the menu still works for this page.
  }
}
