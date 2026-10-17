import { useEffect, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { capturesSince, mergeSpotCaptures } from './spotCaptures';
import { setSpotClickHandler, setSpotsCaptured, setSpotsVisible, type Spot, type SpotCategory } from './spots';
import { getSpotCaptures, type SpotCapture } from '../api';

export interface SpotsState {
  /** The spot whose popup is open. */
  openSpot: Spot | null;
  closeSpot: () => void;
  /** When this account captured [spotId] with the Android app (FR-15.6), or null. */
  capturedAt: (spotId: Spot['id']) => string | null;
}

/** Points of interest (FR-15) over the map: the shown categories, the captured badges, and the
 *  popup a click on a spot opens. Hiding a spot's category closes its popup. */
export function useSpots(map: MapLibreMap | null, shown: readonly SpotCategory[]): SpotsState {
  const [openSpot, setOpenSpot] = useState<Spot | null>(null);
  const [captures, setCaptures] = useState<readonly SpotCapture[]>([]);

  useEffect(() => {
    setSpotClickHandler(setOpenSpot);
  }, []);
  // Captures are made on the phone, so they're re-read when the page regains visibility or focus.
  // The first read takes the whole list; later ones only what's newer (spotCaptures.ts).
  useEffect(() => {
    let controller: AbortController | null = null;
    let known: readonly SpotCapture[] | null = null;
    const load = () => {
      controller?.abort();
      controller = new AbortController();
      const since = known ? capturesSince(known) : undefined;
      getSpotCaptures(controller.signal, since).then(
        (fresh) => {
          known = known ? mergeSpotCaptures(known, fresh) : fresh;
          setCaptures(known);
        },
        () => {
          // Offline or aborted: the badges stay as they were.
        },
      );
    };
    const onVisible = () => {
      if (document.visibilityState === 'visible') load();
    };
    load();
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('focus', load);
    return () => {
      controller?.abort();
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('focus', load);
    };
  }, []);
  useEffect(() => {
    if (map) setSpotsCaptured(map, captures.map((c) => c.spot_id));
  }, [map, captures]);
  useEffect(() => {
    if (map) setSpotsVisible(map, shown);
    setOpenSpot((open) => (open && !shown.includes(open.category) ? null : open));
  }, [map, shown]);

  return {
    openSpot,
    closeSpot: () => setOpenSpot(null),
    capturedAt: (spotId) => captures.find((c) => c.spot_id === spotId)?.captured_at ?? null,
  };
}
