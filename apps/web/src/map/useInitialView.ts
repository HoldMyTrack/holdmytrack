import { useEffect, useRef, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { WORLD_VIEW } from './config';
import type { Flavor } from './style';
import { flavorForTheme, parseHash, pinnedFlavor, replaceHash, type HashState, type ViewState } from './viewState';
import { currentTheme, useTheme } from '../ui/useTheme';

export interface InitialView {
  /** The URL's hash, the opening camera and flavor, read once per mount. */
  initial: { hash: HashState; view: ViewState; flavor: Flavor };
  /** The basemap flavor now. */
  flavor: Flavor;
  /** A flavor the URL pinned, until the theme next changes. */
  pinned: Flavor | undefined;
}

/**
 * The opening camera and the basemap flavor. Every sign-in, sign-up, demo start and sign-out
 * ends in a redirect to a bare `/`, so a previous session's camera can't carry over
 * (IMPLEMENTATION.md §4.19). The flavor follows the page theme and swaps live; a URL can pin
 * another until the theme next changes, since whoever switched the theme expects the map to follow.
 */
export function useInitialView(): InitialView {
  const [initial] = useState<InitialView['initial']>(() => {
    const hash = parseHash(window.location.hash);
    return {
      hash,
      view: hash.view ?? { longitude: WORLD_VIEW.longitude, latitude: WORLD_VIEW.latitude, zoom: WORLD_VIEW.zoom },
      flavor: pinnedFlavor(hash) ?? flavorForTheme(currentTheme()),
    };
  });
  const theme = useTheme();
  const [pinned, setPinned] = useState(() => pinnedFlavor(initial.hash));
  const initialTheme = useRef(theme);
  useEffect(() => {
    if (theme !== initialTheme.current) setPinned(undefined);
  }, [theme]);
  return { initial, flavor: pinned ?? flavorForTheme(theme), pinned };
}

/** Keeps the hash current, once per gesture (`moveend`), not per frame. */
export function useHashSync(map: MapLibreMap | null, pinned: Flavor | undefined): void {
  // Read by the moveend listener, which outlives any one render.
  const pinnedRef = useRef(pinned);
  pinnedRef.current = pinned;
  useEffect(() => {
    if (!map) return;
    const sync = () => {
      const centre = map.getCenter();
      replaceHash({ longitude: centre.lng, latitude: centre.lat, zoom: map.getZoom() }, pinnedRef.current);
    };
    sync();
    map.on('moveend', sync);
    return () => {
      map.off('moveend', sync);
    };
  }, [map, pinned]);
}
