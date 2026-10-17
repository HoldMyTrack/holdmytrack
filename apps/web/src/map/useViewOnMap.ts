import { useCallback, useEffect, useRef, type Dispatch } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import type { MapMode } from './mapMode';
import type { MapSessionEvent } from './session';
import type { Activity } from '../api';
import type { DateRange } from '../ui/dateMath';

/**
 * "View on map" (`/?activity=&day=`, the /sync page's link), once, on arrival: before any range of
 * the user's own, so the default never replaces it. Normal mode first, since only it focuses. If
 * the activity's day is outside the range, the range becomes that day and the focus waits for its
 * list, and for the map. The day is the activity's local day, as the server reads a bare from/to
 * (IMPLEMENTATION.md §4.30); a UTC day would miss an evening activity west of Greenwich.
 */
export function useViewOnMap(
  map: MapLibreMap | null,
  target: { id: string; day: string } | null,
  state: { mode: MapMode; range: DateRange | null; activities: readonly Activity[] },
  dispatch: Dispatch<MapSessionEvent>,
  focus: (id: string) => void,
  /** Moves the slider's window to the day, or its knobs would sit off its edges. */
  revealDay: (day: string) => void,
): void {
  const { mode, range, activities } = state;
  const pendingFocusRef = useRef<string | null>(null);
  const view = useCallback(
    (activityId: string, day: string) => {
      if (mode !== 'normal') dispatch({ type: 'modeChanged', mode: 'normal' });
      revealDay(day);
      if (range !== null && day >= range.from && day <= range.to) {
        focus(activityId);
        return;
      }
      pendingFocusRef.current = activityId;
      dispatch({ type: 'dayViewed', day });
    },
    [mode, dispatch, range, focus, revealDay],
  );
  const arrivedRef = useRef(false);
  useEffect(() => {
    if (target === null || arrivedRef.current) return;
    arrivedRef.current = true;
    view(target.id, target.day);
  }, [target, view]);
  useEffect(() => {
    const pending = pendingFocusRef.current;
    if (pending === null || !map) return;
    if (activities.some((a) => a.id === pending)) {
      pendingFocusRef.current = null;
      focus(pending);
    }
  }, [map, activities, focus]);
}
