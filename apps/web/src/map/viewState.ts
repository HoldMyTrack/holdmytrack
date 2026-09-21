import { FLAVORS, isFlavor, type Flavor } from './style';

/**
 * Two-way sync between the camera plus theme and the URL hash, so a view is
 * shareable and survives reload.
 *
 * MapLibre has a built-in `hash: true` option that writes `#zoom/lat/lng`, and it
 * is deliberately not used: the theme has to live in the same hash, and mixing the
 * built-in writer with our own produces two components fighting over one string.
 *
 * Format: `#map=<zoom>/<lat>/<lon>&theme=<flavor>`
 */

export interface ViewState {
  longitude: number;
  latitude: number;
  zoom: number;
}

export interface HashState {
  view?: ViewState;
  flavor?: Flavor;
}

/** Five decimals of degree is ~1 m — past the point the camera can distinguish. */
const COORD_PRECISION = 5;
const ZOOM_PRECISION = 2;

export function parseHash(hash: string): HashState {
  const params = new URLSearchParams(hash.replace(/^#/, ''));
  const result: HashState = {};

  const theme = params.get('theme');
  if (theme && isFlavor(theme)) result.flavor = theme;

  const map = params.get('map');
  if (map) {
    const [zoom, latitude, longitude] = map.split('/').map(Number);
    if (
      zoom !== undefined &&
      latitude !== undefined &&
      longitude !== undefined &&
      Number.isFinite(zoom) &&
      Number.isFinite(latitude) &&
      Number.isFinite(longitude) &&
      Math.abs(latitude) <= 90 &&
      Math.abs(longitude) <= 180
    ) {
      result.view = { zoom, latitude, longitude };
    }
  }

  return result;
}

export function formatHash(view: ViewState, flavor: Flavor): string {
  const map = [
    view.zoom.toFixed(ZOOM_PRECISION),
    view.latitude.toFixed(COORD_PRECISION),
    view.longitude.toFixed(COORD_PRECISION),
  ].join('/');
  return `#map=${map}&theme=${flavor}`;
}

/**
 * Rewrite the hash without touching history: panning a map should not fill the
 * back button with hundreds of entries.
 */
export function replaceHash(view: ViewState, flavor: Flavor): void {
  const next = formatHash(view, flavor);
  if (next !== window.location.hash) {
    window.history.replaceState(null, '', next);
  }
}

export const DEFAULT_FLAVOR: Flavor = FLAVORS[0];

/**
 * Drops any saved camera position from the URL — called at every app-initiated identity
 * change (sign-out, demo start, sign-in success), never on an ordinary reload, where a saved
 * hash is a legitimate "return to where I was." Without this, a fresh session's MapView can
 * inherit a previous session's leftover camera position and skip its own fly-to-most-recent
 * entirely (FR-4.5's "saved URL position wins" working exactly as designed, against stale
 * data — reported live for the Demo Customer account, root-caused to this).
 */
export function clearSavedView(): void {
  if (window.location.hash) {
    window.history.replaceState(null, '', window.location.pathname + window.location.search);
  }
}
