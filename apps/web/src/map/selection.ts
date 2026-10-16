/**
 * The Activities panel's selection rules, as plain values so they're unit-tested apart from
 * `MapView.tsx`. Android keeps the same rules in `panel/PanelState.kt`.
 *
 * Focus and the checked group are independent: a row's text or its track focuses it (one at a
 * time); a checkbox builds the group the toolbar acts on.
 */
import type { Activity } from '../api';
import { passesFilters, type DistanceRange } from '../ui/activityFacets.ts';

/** `set` with `item` added, or removed if it was there. */
export function toggled<T>(set: ReadonlySet<T>, item: T): Set<T> {
  const next = new Set(set);
  if (next.has(item)) next.delete(item);
  else next.add(item);
  return next;
}

/** What the toolbar acts on: the checked group if anything is checked, else the focused row.
 *  Checked wins over a union so a Delete never reaches past what was ticked. */
export function toolbarTarget<S extends ReadonlySet<string>>(checked: S, focused: string | null): S | Set<string> {
  if (checked.size > 0) return checked;
  return new Set(focused !== null ? [focused] : []);
}

/** Show/Hide over the toolbar's target: if any of it is hidden, show all; otherwise hide all.
 *  One click never leaves the group mixed. */
export function toggledVisibility(hidden: ReadonlySet<string>, target: ReadonlySet<string>): Set<string> {
  const anyHidden = [...target].some((id) => hidden.has(id));
  const next = new Set(hidden);
  for (const id of target) {
    if (anyHidden) next.delete(id);
    else next.add(id);
  }
  return next;
}

/** `set` without `ids`; the same set when it holds none of them, so state doesn't change. */
export function withoutIds<T extends ReadonlySet<string>>(set: T, ids: ReadonlySet<string>): T | Set<string> {
  if (![...ids].some((id) => set.has(id))) return set;
  const next = new Set(set);
  for (const id of ids) next.delete(id);
  return next;
}

/** Inverts the check over the listed rows; a checked row the filters hide is dropped. */
export function invertedChecks(listed: readonly Pick<Activity, 'id'>[], checked: ReadonlySet<string>): Set<string> {
  return new Set(listed.filter((a) => !checked.has(a.id)).map((a) => a.id));
}

/** Tracks the map doesn't draw or fly to: filtered out, hidden, or Pending (FR-5.15). Pending
 *  stays out of `hidden` so the user's own choice survives the reprocess. */
export function mapHiddenIds(
  activities: readonly Activity[],
  excludedTypes: ReadonlySet<string>,
  distanceFilter: DistanceRange | null,
  hidden: ReadonlySet<string>,
  pendingIds: readonly string[],
): Set<string> {
  const filteredOut = activities.filter((a) => !passesFilters(a, excludedTypes, distanceFilter)).map((a) => a.id);
  return new Set([...filteredOut, ...hidden, ...pendingIds]);
}
