import { useEffect, useRef } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { flyToBBox, flyToView, unionBBox } from './bbox';
import { firstListFit, needsFallbackView } from './cameraRules';
import { WORLD_VIEW } from './config';
import { countryView } from './countryView';
import type { Activity } from '../api';

export interface ListFlightInputs {
  activities: readonly Activity[];
  /** The query the list was fetched for; null before the first. */
  loadedKey: string | null;
  storyId: string | null;
  /** The session asked for a flight onto the next list ([onFlown] consumes it). */
  flyRequest: boolean;
  onFlown: () => void;
  mapHiddenIds: ReadonlySet<string>;
  /** The URL carries a camera, which the first list leaves alone. */
  urlView: boolean;
  /** Arriving on one activity (`/?activity=`) flies to it instead of the first list. */
  arrivingOnActivity: boolean;
  daysReady: boolean;
  earliest: string | null;
  country: string;
  fitTo: (activities: Activity[]) => void;
}

/**
 * Camera moves on a newly loaded list. The first list's fit is cameraRules.ts's firstListFit;
 * after that, only a list the session asked a flight for (a picked range, a Story opened from the
 * map) moves the camera, onto what's drawn. Keyed on the query the list was fetched for, because
 * the list is a new array on every reload: the Pending poll, a finished upload or a Private
 * location change must not move the camera. An account with no history at all gets its Country's
 * view, else WORLD_VIEW; never geolocation.
 */
export function useListFlight(map: MapLibreMap | null, inputs: ListFlightInputs): void {
  const { activities, loadedKey, storyId, flyRequest, onFlown, mapHiddenIds, urlView, arrivingOnActivity, daysReady, earliest, country, fitTo } = inputs;
  // Read by the flight without being its dependency: it's keyed on the list, but leaves out tracks that aren't drawn.
  const mapHiddenIdsRef = useRef(mapHiddenIds);
  useEffect(() => {
    mapHiddenIdsRef.current = mapHiddenIds;
  }, [mapHiddenIds]);

  const hasFlownRef = useRef(arrivingOnActivity);
  const flownKeyRef = useRef<string | null>(null);
  useEffect(() => {
    if (!map || loadedKey === null) return;
    if (loadedKey === flownKeyRef.current) return;
    flownKeyRef.current = loadedKey;
    if (!hasFlownRef.current) {
      const fit = firstListFit(activities, storyId, urlView);
      if (fit === null) return;
      // The first list, even an empty one: an account with no history gets the fallback view
      // below, and its first upload doesn't fly either.
      hasFlownRef.current = true;
      if (fit.length > 0) fitTo(fit);
      return;
    }
    if (!flyRequest) return;
    onFlown();
    const visible = activities.filter((a) => !mapHiddenIdsRef.current.has(a.id));
    const bounds = unionBBox(visible.flatMap((a) => (a.bbox ? [a.bbox] : [])));
    if (bounds) flyToBBox(map, bounds);
  }, [map, activities, loadedKey, fitTo, storyId, flyRequest]);

  const hasFlownToFallbackRef = useRef(false);
  useEffect(() => {
    if (!map || hasFlownToFallbackRef.current) return;
    if (!needsFallbackView(urlView, activities.length, daysReady, earliest)) return;
    hasFlownToFallbackRef.current = true;
    flyToView(map, countryView(country) ?? WORLD_VIEW);
  }, [map, activities, daysReady, earliest, country]);
}
