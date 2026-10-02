import { isFlavor, type Flavor } from './style';

/**
 * Two-way sync between the camera plus theme and the URL hash, so a view is
 * shareable and survives reload.
 *
 * MapLibre has a built-in `hash: true` option that writes `#zoom/lat/lng`, and it
 * is deliberately not used: the theme has to live in the same hash, and mixing the
 * built-in writer with our own produces two components fighting over one string.
 *
 * Format: `#map=<zoom>/<lat>/<lon>[&theme=<flavor>]`. The map's flavor follows the page's
 * light/dark theme (MapView.tsx); `theme` pins one of the other three flavors (pinnedFlavor)
 * until the page's theme next changes, and is written back only while it does.
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
    // Number('') is 0, so a truncated link's empty part (`#map=5//`) would read as 0°, 0°.
    const parts = map.split('/');
    const [zoom, latitude, longitude] = parts.map(Number);
    if (
      parts.length === 3 &&
      parts.every((part) => part.trim() !== '') &&
      zoom !== undefined &&
      latitude !== undefined &&
      longitude !== undefined &&
      Number.isFinite(zoom) &&
      Number.isFinite(latitude) &&
      Number.isFinite(longitude) &&
      zoom >= 0 &&
      zoom <= 24 &&
      Math.abs(latitude) <= 90 &&
      Math.abs(longitude) <= 180
    ) {
      result.view = { zoom, latitude, longitude };
    }
  }

  return result;
}

export function formatHash(view: ViewState, flavor?: Flavor): string {
  const map = [
    view.zoom.toFixed(ZOOM_PRECISION),
    view.latitude.toFixed(COORD_PRECISION),
    view.longitude.toFixed(COORD_PRECISION),
  ].join('/');
  return flavor ? `#map=${map}&theme=${flavor}` : `#map=${map}`;
}

/**
 * Rewrite the hash without touching history: panning a map should not fill the
 * back button with hundreds of entries.
 */
export function replaceHash(view: ViewState, flavor?: Flavor): void {
  const next = formatHash(view, flavor);
  if (next !== window.location.hash) {
    window.history.replaceState(null, '', next);
  }
}

/** The flavor a URL pins the map to: `white`, `black` or `grayscale`. `light` and `dark` pin
 *  nothing — they are the two the page's theme already picks between, so the theme decides —
 *  which also drops the `&theme=light` every URL carried while the flavor was URL-only. */
export function pinnedFlavor(hash: HashState): Flavor | undefined {
  return hash.flavor === 'light' || hash.flavor === 'dark' ? undefined : hash.flavor;
}

/** The basemap flavor that matches the page's theme when the URL doesn't pin one. */
export function flavorForTheme(theme: 'light' | 'dark'): Flavor {
  return theme;
}
