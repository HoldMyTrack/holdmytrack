import { useCallback, useEffect } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { refreshTrackLayer } from './tracks';
import type { ActivityQuery } from '../api';

export interface DataRefreshInputs {
  activityQuery: ActivityQuery;
  reloadActivities: () => void;
  reloadDays: () => void;
  reloadStory: () => void;
  /** Waits for the account's coverage jobs to drain, then refetches Fog and Heatmap (useCoverageRefresh.ts). */
  watchCoverage: (then?: () => void) => void;
  /** The bands refetch against new points. */
  bumpMetrics: () => void;
}

export interface DataRefresh {
  /** The tracks layer at the current query, fetched again. */
  refreshTracks: () => void;
  /** A finished import or a delete: tracks, list, days, the open Story and coverage all refresh. */
  afterImport: () => void;
  /**
   * A saved or deleted Private location reprocesses every activity it could clip. The reload shows
   * them Pending and the Pending poll refreshes the map, but a batch that finishes before the
   * reload returns is never seen Pending, so the coverage watch refreshes everything once the
   * server drains.
   */
  afterPrivateLocations: () => void;
  /**
   * A row entered Pending (usePendingPoll.ts): the tracks refetch and the coverage watch starts,
   * since the server clears Pending just before its last render.
   */
  pendingStarted: () => void;
  /** A row left Pending with new points: tracks, days, the open Story, the bands and coverage
   *  refresh; the camera stays (FR-5.15). */
  pendingFinished: () => void;
}

/** What the map reads again when activities change under it. The header's Upload menu
 *  (static/upload.js) says when an upload or import has landed (`hmt:imports-changed`). */
export function useDataRefresh(map: MapLibreMap | null, inputs: DataRefreshInputs): DataRefresh {
  const { activityQuery, reloadActivities, reloadDays, reloadStory, watchCoverage, bumpMetrics } = inputs;
  const refreshTracks = useCallback(() => {
    if (map) refreshTrackLayer(map, activityQuery);
  }, [map, activityQuery]);

  const afterImport = useCallback(() => {
    refreshTracks();
    reloadActivities();
    reloadDays();
    reloadStory();
    watchCoverage();
  }, [refreshTracks, reloadActivities, reloadDays, reloadStory, watchCoverage]);

  useEffect(() => {
    window.addEventListener('hmt:imports-changed', afterImport);
    return () => window.removeEventListener('hmt:imports-changed', afterImport);
  }, [afterImport]);

  const afterPrivateLocations = useCallback(() => {
    reloadActivities();
    watchCoverage(() => {
      refreshTracks();
      reloadActivities();
      reloadDays();
      reloadStory();
      bumpMetrics();
    });
  }, [refreshTracks, reloadActivities, reloadDays, reloadStory, watchCoverage, bumpMetrics]);

  const pendingStarted = useCallback(() => {
    refreshTracks();
    watchCoverage();
  }, [refreshTracks, watchCoverage]);

  const pendingFinished = useCallback(() => {
    refreshTracks();
    // Includes the Country/Region tiers: an edit can un-visit a region.
    watchCoverage();
    reloadDays();
    reloadStory();
    bumpMetrics();
  }, [refreshTracks, watchCoverage, reloadDays, reloadStory, bumpMetrics]);

  return { refreshTracks, afterImport, afterPrivateLocations, pendingStarted, pendingFinished };
}
