import type { SpotCategory } from './spots';
import { SPOT_CATEGORIES } from './spots';

/**
 * The map's view choices beside the mode toggle: satellite imagery (the Satellite button,
 * FR-4.14), and what the Layers menu (OverlaysMenu.tsx) has picked — the four kinds of path
 * (FR-4.13) and each Spots category (FR-15.2) — with the Layers button's checkbox, which shows or
 * hides all of them at once. A per-browser view preference, kept in localStorage like the theme,
 * not on the account; every path and place off until picked.
 */
export interface Overlays {
  /** Satellite imagery under the basemap's roads and labels. Only meaningful when the
   *  deployment configures imagery (config.ts's satelliteSource). */
  satellite: boolean;
  /** The Layers checkbox: the picks below are drawn only while it's on, and kept while it's off. */
  enabled: boolean;
  trails: boolean;
  /** Dirt, farm and forest roads — OSM's `highway=track`. */
  tracks: boolean;
  /** Cycleways, from the bike-path tiles (bikePaths.ts). */
  bikePaths: boolean;
  /** Paths, footways and bridleways designated for bikes, from the same tiles. */
  sharedPaths: boolean;
  /** The Spots categories shown, in SPOT_CATEGORIES order. */
  spots: SpotCategory[];
}

const STORAGE_KEY = 'hmt.overlays';
// The two toggles the menu replaced: one for both kinds of path, one for every category.
const LEGACY_PATHS_KEY = 'hmt.showPaths';
const LEGACY_POI_KEY = 'hmt.showPoi';

export const NO_OVERLAYS: Overlays = { satellite: false, enabled: true, trails: false, tracks: false, bikePaths: false, sharedPaths: false, spots: [] };

/** How many paths and places are picked, whether or not the Layers checkbox is on. */
export function pickedCount(overlays: Overlays): number {
  return Number(overlays.trails) + Number(overlays.tracks) + Number(overlays.bikePaths) + Number(overlays.sharedPaths) + overlays.spots.length;
}

export function loadOverlays(): Overlays {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const saved = JSON.parse(raw) as Partial<Overlays>;
      return {
        satellite: saved.satellite === true,
        // On unless turned off: a choice saved before the checkbox existed keeps showing its picks.
        enabled: saved.enabled !== false,
        trails: saved.trails === true,
        // Tracks were part of Trails before they had their own entry: a choice saved then
        // keeps showing them.
        tracks: typeof saved.tracks === 'boolean' ? saved.tracks : saved.trails === true,
        bikePaths: saved.bikePaths === true,
        sharedPaths: saved.sharedPaths === true,
        spots: SPOT_CATEGORIES.filter((c) => Array.isArray(saved.spots) && saved.spots.includes(c)),
      };
    }
    const paths = window.localStorage.getItem(LEGACY_PATHS_KEY) === '1';
    const poi = window.localStorage.getItem(LEGACY_POI_KEY) === '1';
    return { satellite: false, enabled: true, trails: paths, tracks: paths, bikePaths: paths, sharedPaths: false, spots: poi ? [...SPOT_CATEGORIES] : [] };
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
