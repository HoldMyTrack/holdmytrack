import { useCallback, type Dispatch } from 'react';
import type { MapSessionEvent } from './session';
import { invertedChecks } from './selection';
import type { Activity } from '../api';
import type { DistanceRange } from '../ui/activityFacets';

export interface SelectionActions {
  toggleChecked: (id: string) => void;
  /** The toolbar's Show/Hide over its target. */
  toggleGroupVisibility: () => void;
  /** A toggle: the type stays in the dropdown either way, so it can be turned back on without Reset filters. */
  toggleType: (activityType: string) => void;
  resetFilters: () => void;
  setDistanceFilter: (filter: DistanceRange | null) => void;
  /** Focuses one activity and flies to it, unless it's hidden (an empty fit is a no-op). With
   *  Focus on map, the only fly to an activity. */
  focus: (id: string) => void;
  /** A click on empty list space, the counterpart of clicking away from every track. The checked group stays. */
  clearFocus: () => void;
  /** The master checkbox while fully checked: empties the group; the focus stays. */
  clearChecked: () => void;
  /** The master checkbox while unchecked or partial: every row listed, never one the filters hide. No fly. */
  checkAll: () => void;
  /** Inverts the check over the listed rows; a checked row the filters hide is dropped. No fly. */
  invertChecked: () => void;
  /** Focus on map: flies to the toolbar's target, hidden tracks excluded. The one way to look at a checked group. */
  showTarget: () => void;
}

export function useSelectionActions(
  dispatch: Dispatch<MapSessionEvent>,
  activities: readonly Activity[],
  listed: readonly Activity[],
  checked: ReadonlySet<string>,
  target: ReadonlySet<string>,
  mapHiddenIds: ReadonlySet<string>,
  fitTo: (activities: Activity[]) => void,
): SelectionActions {
  const focus = useCallback(
    (id: string) => {
      dispatch({ type: 'focused', id });
      fitTo(activities.filter((a) => a.id === id && !mapHiddenIds.has(a.id)));
    },
    [dispatch, activities, mapHiddenIds, fitTo],
  );
  const toggleChecked = useCallback((id: string) => dispatch({ type: 'checkToggled', id }), [dispatch]);
  const toggleGroupVisibility = useCallback(() => dispatch({ type: 'visibilityToggled' }), [dispatch]);
  const toggleType = useCallback((activityType: string) => dispatch({ type: 'typeToggled', activityType }), [dispatch]);
  const resetFilters = useCallback(() => dispatch({ type: 'filtersReset' }), [dispatch]);
  const setDistanceFilter = useCallback((filter: DistanceRange | null) => dispatch({ type: 'distanceSet', filter }), [dispatch]);
  const clearFocus = useCallback(() => dispatch({ type: 'focused', id: null }), [dispatch]);
  const clearChecked = useCallback(() => dispatch({ type: 'checkedSet', ids: [] }), [dispatch]);
  const checkAll = useCallback(() => dispatch({ type: 'checkedSet', ids: listed.map((a) => a.id) }), [dispatch, listed]);
  const invertChecked = useCallback(() => dispatch({ type: 'checkedSet', ids: invertedChecks(listed, checked) }), [dispatch, listed, checked]);
  const showTarget = useCallback(
    () => fitTo(activities.filter((a) => target.has(a.id) && !mapHiddenIds.has(a.id))),
    [activities, target, mapHiddenIds, fitTo],
  );
  return { toggleChecked, toggleGroupVisibility, toggleType, resetFilters, setDistanceFilter, focus, clearFocus, clearChecked, checkAll, invertChecked, showTarget };
}
