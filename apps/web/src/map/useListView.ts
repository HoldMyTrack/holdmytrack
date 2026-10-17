import { useEffect, useMemo, type Dispatch } from 'react';
import type { MapSessionEvent, Selection } from './session';
import { mapHiddenIds as hiddenOnMap } from './selection';
import type { Activity } from '../api';
import { distanceBounds, passesFilters, typeFacets, type DistanceRange, type TypeFacet } from '../ui/activityFacets';

export interface ListView {
  /** Rows with a reprocess pending (a track edit or a Private location change). A Pending track isn't drawn (FR-5.15). */
  pendingIds: string[];
  distanceBounds: DistanceRange | null;
  facets: TypeFacet[];
  /** The rows the panel lists: the loaded activities narrowed by the TYPE/DISTANCE filters. */
  listed: Activity[];
  /** Tracks not drawn: filtered out, hidden or Pending. The tile already omits a Pending one; this
   *  hides it before the refetch lands. */
  mapHiddenIds: Set<string>;
}

/** What the loaded activities and the selection's filters make of the list and the map. */
export function useListView(activities: Activity[], selection: Selection, dispatch: Dispatch<MapSessionEvent>): ListView {
  const { excludedTypes, distanceFilter, hidden } = selection;
  const pendingIds = useMemo(() => activities.filter((a) => a.pending).map((a) => a.id), [activities]);
  const bounds = useMemo(() => distanceBounds(activities), [activities]);
  // The distance slider shows only while the activities span a range. A band left set after that
  // would keep filtering with nothing on screen to clear it.
  const sliderShown = bounds !== null && bounds.min < bounds.max;
  useEffect(() => {
    if (!sliderShown) dispatch({ type: 'distanceSet', filter: null });
  }, [sliderShown, dispatch]);
  const facets = useMemo(() => typeFacets(activities, distanceFilter), [activities, distanceFilter]);
  const listed = useMemo(() => activities.filter((a) => passesFilters(a, excludedTypes, distanceFilter)), [activities, excludedTypes, distanceFilter]);
  const mapHiddenIds = useMemo(
    () => hiddenOnMap(activities, excludedTypes, distanceFilter, hidden, pendingIds),
    [activities, excludedTypes, distanceFilter, hidden, pendingIds],
  );
  return { pendingIds, distanceBounds: bounds, facets, listed, mapHiddenIds };
}
