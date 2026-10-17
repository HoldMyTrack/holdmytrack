/**
 * The map's cross-feature state and the rules between its parts, as a reducer: which activities
 * the list and the tracks show (a date range or a Story), the selection over them, the mode, and
 * whether the next loaded list moves the camera. Features read it and change it only through
 * events; side effects (flights, the URL) stay with them. Android: `map/MapSession.kt`; both run
 * `testdata/map-session`.
 */
import type { DistanceRange } from '../ui/activityFacets';
import type { DateRange } from '../ui/dateMath';
import type { MapMode } from './mapMode';
import { toggled, toggledVisibility, toolbarTarget, withoutIds } from './selection.ts';

export interface Selection {
  /** The checkbox group the toolbar acts on. */
  checked: Set<string>;
  /** The one activity a row's text or its track focused: bolded and flown to. */
  focused: string | null;
  /** Tracks hidden from the map. */
  hidden: Set<string>;
  excludedTypes: Set<string>;
  distanceFilter: DistanceRange | null;
}

export interface MapSession {
  /** The date range; null until its default is known. */
  range: DateRange | null;
  /** The user picked [range]; until then the default may replace it. */
  userPicked: boolean;
  /** The open Story: the list and tracks are its activities, [range] kept for when it closes. */
  storyId: string | null;
  /** The next list loaded for a new query moves the camera onto what it draws. */
  flyRequest: boolean;
  selection: Selection;
  mode: MapMode;
  /** The checked group and focus as Normal mode left them, restored on the way back. */
  modeSnapshot: { checked: Set<string>; focused: string | null } | null;
}

export type MapSessionEvent =
  /** The slider picked a range: a new set of rows, so the selection resets (docs/SPEC.md FR-6.6). */
  | { type: 'rangePicked'; range: DateRange }
  /** The default range re-derived; ignored once the user has picked one, and held while the Edit
   *  window is open, since sliding past the edited activity would close it. */
  | { type: 'defaultRange'; range: DateRange; editOpen?: boolean }
  /** "View on map" for an activity outside the range: its own day, picked, without the range's
   *  flight, since the focus flies to the activity. */
  | { type: 'dayViewed'; day: string }
  | { type: 'storyEntered'; id: string }
  | { type: 'storyExited' }
  /** The camera has moved for the requested list. */
  | { type: 'flyConsumed' }
  | { type: 'modeChanged'; mode: MapMode }
  | { type: 'checkToggled'; id: string }
  | { type: 'checkedSet'; ids: Iterable<string> }
  | { type: 'focused'; id: string | null }
  /** The toolbar's Show/Hide over its target. */
  | { type: 'visibilityToggled' }
  | { type: 'typeToggled'; activityType: string }
  | { type: 'distanceSet'; filter: DistanceRange | null }
  | { type: 'filtersReset' }
  /** Activities left the account (deleted, or taken out of the open Story): their ids leave the selection. */
  | { type: 'activitiesRemoved'; ids: Iterable<string>; unfocusOnly?: boolean };

export function emptySelection(): Selection {
  return { checked: new Set(), focused: null, hidden: new Set(), excludedTypes: new Set(), distanceFilter: null };
}

export function initialSession(storyId: string | null): MapSession {
  return { range: null, userPicked: false, storyId, flyRequest: false, selection: emptySelection(), mode: 'normal', modeSnapshot: null };
}

export function mapSessionReducer(state: MapSession, event: MapSessionEvent): MapSession {
  const select = (selection: Partial<Selection>): MapSession => ({ ...state, selection: { ...state.selection, ...selection } });
  switch (event.type) {
    case 'rangePicked':
      return { ...state, range: event.range, userPicked: true, flyRequest: true, selection: emptySelection() };
    case 'defaultRange':
      return state.userPicked || event.editOpen ? state : { ...state, range: event.range };
    case 'dayViewed':
      return { ...mapSessionReducer(state, { type: 'rangePicked', range: { from: event.day, to: event.day } }), flyRequest: false };
    case 'storyEntered':
      return { ...state, storyId: event.id, flyRequest: true, selection: emptySelection() };
    case 'storyExited':
      // The list returns to the date range; the camera stays.
      return { ...state, storyId: null, flyRequest: false, selection: emptySelection() };
    case 'flyConsumed':
      return state.flyRequest ? { ...state, flyRequest: false } : state;
    case 'modeChanged': {
      if (event.mode === state.mode) return state;
      // Fog and Heatmap can't select or focus: leaving Normal keeps the group and focus aside,
      // and returning restores them. Fog to Heatmap touches neither.
      if (state.mode === 'normal') {
        const { checked, focused } = state.selection;
        return { ...state, mode: event.mode, modeSnapshot: { checked, focused }, selection: { ...state.selection, checked: new Set(), focused: null } };
      }
      if (event.mode === 'normal' && state.modeSnapshot) {
        const { checked, focused } = state.modeSnapshot;
        return { ...state, mode: event.mode, modeSnapshot: null, selection: { ...state.selection, checked, focused } };
      }
      return { ...state, mode: event.mode, modeSnapshot: event.mode === 'normal' ? null : state.modeSnapshot };
    }
    case 'checkToggled':
      return select({ checked: toggled(state.selection.checked, event.id) });
    case 'checkedSet':
      return select({ checked: new Set(event.ids) });
    case 'focused':
      return state.selection.focused === event.id ? state : select({ focused: event.id });
    case 'visibilityToggled':
      return select({ hidden: toggledVisibility(state.selection.hidden, toolbarTarget(state.selection.checked, state.selection.focused)) });
    case 'typeToggled':
      return select({ excludedTypes: toggled(state.selection.excludedTypes, event.activityType) });
    case 'distanceSet':
      return state.selection.distanceFilter === event.filter ? state : select({ distanceFilter: event.filter });
    case 'filtersReset':
      return select({ excludedTypes: new Set(), distanceFilter: null });
    case 'activitiesRemoved': {
      const ids = new Set(event.ids);
      const { checked, focused, hidden } = state.selection;
      const nextFocused = focused !== null && ids.has(focused) ? null : focused;
      if (event.unfocusOnly) return nextFocused === focused ? state : select({ focused: nextFocused });
      const nextChecked = withoutIds(checked, ids);
      const nextHidden = withoutIds(hidden, ids);
      if (nextFocused === focused && nextChecked === checked && nextHidden === hidden) return state;
      return select({ focused: nextFocused, checked: nextChecked as Set<string>, hidden: nextHidden as Set<string> });
    }
  }
}
