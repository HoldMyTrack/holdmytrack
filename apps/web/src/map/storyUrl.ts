import type { PanelTab } from '../ui/ActivitiesPanel';

/** The open Story in the URL (FR-14.6), kept there so a refresh or a shared link returns to it. */

/** `?story=<id>`, or null. */
export function storyParam(search: string): string | null {
  return new URLSearchParams(search).get('story') || null;
}

/** `?tab=stories` opens the Stories tab with no Story picked; the copy-arrived email links here (FR-14.7). */
export function storiesTabParam(search: string): boolean {
  return new URLSearchParams(search).get('tab') === 'stories';
}

/** The panel's tab on mount: Activities for an arriving activity, Stories for `?story=` or
 *  `?tab=stories`, Privacy for `/?private-locations` (FR-8.1). */
export function initialPanelTab(search: string, arrivingOnActivity: boolean, privateLocationsOpen: boolean): PanelTab {
  if (arrivingOnActivity) return 'activities';
  if (storyParam(search) !== null || storiesTabParam(search)) return 'stories';
  return privateLocationsOpen ? 'private' : 'activities';
}

/** The URL with `?story=` set, or removed for null; the hash and other parameters kept. */
export function urlWithStory(location: { pathname: string; search: string; hash: string }, id: string | null): string {
  const params = new URLSearchParams(location.search);
  if (id === null) params.delete('story');
  else params.set('story', id);
  const qs = params.toString();
  return location.pathname + (qs ? `?${qs}` : '') + location.hash;
}

/** What opening or closing a Story does to history: push, replace, or nothing (Back/Forward already moved it). */
export type StoryNav = 'push' | 'replace' | 'none';
