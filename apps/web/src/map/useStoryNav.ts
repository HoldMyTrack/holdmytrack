import { useCallback, useEffect, useMemo, useRef, type Dispatch } from 'react';
import type { MapSessionEvent } from './session';
import { storyParam, urlWithStory, type StoryNav } from './storyUrl';
import type { Story } from '../api';
import type { PanelTab, StoriesPanel } from '../ui/ActivitiesPanel';
import type { StoriesState } from '../ui/useStories';
import type { StoryState } from '../ui/useStory';
import { useStoryInbox } from '../ui/useStoryInbox';

function moveStoryUrl(id: string | null, nav: StoryNav) {
  if (nav === 'push') window.history.pushState(null, '', urlWithStory(window.location, id));
  else if (nav === 'replace') window.history.replaceState(null, '', urlWithStory(window.location, id));
}

export interface StoryNavInputs {
  storyId: string | null;
  dispatch: Dispatch<MapSessionEvent>;
  panelTab: PanelTab;
  setPanelTab: (tab: PanelTab) => void;
  stories: StoriesState;
  story: StoryState;
  reloadActivities: () => void;
  watchCoverage: (then?: () => void) => void;
  refreshTracks: () => void;
  /** A Story copy arrived (FR-14.7): new activities, refreshed as an import is. */
  afterImport: () => void;
  /** An activity left the open Story: its hover preview goes too. */
  onActivityRemoved: (id: string) => void;
}

export interface StoryNavigation {
  enterStory: (id: string, nav?: StoryNav) => void;
  changePanelTab: (tab: PanelTab) => void;
  /** A Story just made with Add to story's "New story…" opens on the Stories tab. */
  openCreatedStory: (story: Story) => void;
  /** Activities added to an existing Story with Add to story. */
  handleStoryAdded: (story: Story) => void;
  storiesPanel: StoriesPanel;
}

/**
 * The open Story (FR-14.6) and the Stories tab: one Story is always open there, the newest until
 * another is picked, and it's kept in the URL so Back and Forward move between Stories and to and
 * from the tab. Opening or closing one resets the list state as a new range does (session.ts).
 */
export function useStoryNav(inputs: StoryNavInputs): StoryNavigation {
  const { storyId, dispatch, panelTab, setPanelTab, stories, story, reloadActivities, watchCoverage, refreshTracks, afterImport, onActivityRemoved } = inputs;

  const enterStory = useCallback(
    (id: string, nav: StoryNav = 'push') => {
      dispatch({ type: 'storyEntered', id });
      setPanelTab('stories');
      moveStoryUrl(id, nav);
    },
    [dispatch, setPanelTab],
  );
  const exitStory = useCallback(
    (nav: StoryNav = 'push') => {
      dispatch({ type: 'storyExited' });
      moveStoryUrl(null, nav);
    },
    [dispatch],
  );
  // Leaving the Stories tab closes its Story; opening it opens the newest (the effect below).
  const changePanelTab = useCallback(
    (next: PanelTab) => {
      if (next !== 'stories' && storyId !== null) exitStory();
      setPanelTab(next);
    },
    [storyId, exitStory, setPanelTab],
  );

  const storiesReload = stories.reload;
  const onCopyArrived = useCallback(() => {
    storiesReload();
    afterImport();
  }, [storiesReload, afterImport]);
  const inbox = useStoryInbox(panelTab === 'stories', onCopyArrived);

  useEffect(() => {
    if (panelTab !== 'stories' || storyId !== null || !stories.ready) return;
    const newest = stories.stories[0];
    if (newest) enterStory(newest.id);
  }, [panelTab, storyId, stories.ready, stories.stories, enterStory]);

  const navRef = useRef({ storyId, enterStory, exitStory, setPanelTab });
  navRef.current = { storyId, enterStory, exitStory, setPanelTab };
  useEffect(() => {
    const onPopState = () => {
      const { storyId: current, enterStory: enter, exitStory: exit, setPanelTab: setTab } = navRef.current;
      const next = storyParam(window.location.search);
      if (next === current) return;
      if (next !== null) {
        enter(next, 'none');
        return;
      }
      exit('none');
      setTab('activities');
    };
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, []);

  const openCreatedStory = useCallback((created: Story) => enterStory(created.id), [enterStory]);
  // The list reloads for the rows' badges. The server moved the tile version, which only
  // `watchCoverage` brings to this page; without it the Story could later draw from stale tiles.
  const handleStoryAdded = useCallback(
    (added: Story) => {
      stories.replace(added);
      reloadActivities();
      watchCoverage();
    },
    [stories.replace, reloadActivities, watchCoverage],
  );
  const handleStoryEdited = useCallback(
    (edited: Story) => {
      stories.replace(edited);
      story.set(edited); // only if it's still the open one (useStory)
    },
    [stories.replace, story.set],
  );
  // A deleted Story's activities stay. Deleting the open one opens the next newest in its place in
  // history; deleting the last leaves none open.
  const handleStoryDeleted = useCallback(
    (id: string) => {
      stories.remove(id);
      if (id !== storyId) return;
      const next = stories.stories.find((s) => s.id !== id);
      if (next) enterStory(next.id, 'replace');
      else exitStory('replace');
    },
    [stories.remove, stories.stories, storyId, enterStory, exitStory],
  );
  // An activity taken out of the open Story: new totals, the row gone, and the tracks redrawn at
  // the tile version the removal moved to.
  const handleStoryActivityRemoved = useCallback(
    (edited: Story, activityId: string) => {
      handleStoryEdited(edited);
      dispatch({ type: 'activitiesRemoved', ids: [activityId], unfocusOnly: true });
      onActivityRemoved(activityId);
      reloadActivities();
      watchCoverage(refreshTracks);
    },
    [handleStoryEdited, dispatch, onActivityRemoved, reloadActivities, watchCoverage, refreshTracks],
  );

  const storiesPanel: StoriesPanel = useMemo(
    () => ({
      stories: stories.stories,
      ready: stories.ready,
      error: stories.error,
      inbox,
      openId: storyId,
      openStory: story.story,
      openError: story.error,
      onOpen: (id: string) => enterStory(id),
      onEdited: handleStoryEdited,
      onDeleted: handleStoryDeleted,
      onActivityRemoved: handleStoryActivityRemoved,
    }),
    [stories.stories, stories.ready, stories.error, inbox, storyId, story.story, story.error, enterStory, handleStoryEdited, handleStoryDeleted, handleStoryActivityRemoved],
  );

  return { enterStory, changePanelTab, openCreatedStory, handleStoryAdded, storiesPanel };
}
