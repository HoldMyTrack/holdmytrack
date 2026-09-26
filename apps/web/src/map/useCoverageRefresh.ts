import { useCallback, useEffect, useRef, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { getCoverageStatus } from '../api';
import { bumpCoverageVersion } from './coverageVersion';
import { refreshFogLayers } from './fog';
import { refreshHeatmapLayers } from './heatmap';

/** How often the coverage status is re-read while a re-render is outstanding. */
const POLL_MS = 2000;
/** Give up waiting after this many reads (~3 minutes) and refetch anyway — a render that
 *  failed leaves no pending job, so this only bounds a job stuck in the queue. */
const MAX_POLLS = 90;

/**
 * Keeps the Fog/Heatmap layers current after an upload or delete (IMPLEMENTATION.md §4.2.3).
 * Both rasters are re-rendered by a queued `render_fog` job, and their tile URLs never change
 * on their own, so an open page would otherwise show pre-change coverage until a reload.
 *
 * The returned `watch()` starts polling `GET /v1/coverage/status` and, once the account has
 * no coverage-changing job left, bumps the coverage version and refetches every Fog/Heatmap
 * source. Each call restarts the watch rather than joining one already running: a second
 * upload or delete can enqueue its jobs just after an in-flight read already came back
 * "done", and restarting is what guarantees that read isn't the last word.
 *
 * While it waits, it also refetches whenever the status's `version` changes: a track edit or
 * Private location change renders once with its Pending activities left out of coverage and
 * again once they're back (IMPLEMENTATION.md §4.7.7), and the first of those is only shown if
 * picked up mid-job. The first read of a page only sets the baseline.
 *
 * `onDone`, when given, runs after that refetch — for a caller whose change also moves things
 * the coverage rasters don't cover (a Private location change reprocesses whole activities),
 * and which can't otherwise tell when the server has finished.
 */
export function useCoverageRefresh(map: MapLibreMap | null): (onDone?: () => void) => void {
  const [generation, setGeneration] = useState(0);
  const onDoneRef = useRef<(() => void) | undefined>(undefined);
  // The status version the layers were last refetched at (or first seen at) — shared across
  // watches, since a restarted watch is still showing the same tiles.
  const shownVersionRef = useRef<number | null>(null);

  useEffect(() => {
    if (generation === 0 || !map) return;
    const controller = new AbortController();
    let timer: number | undefined;
    let polls = 0;
    const refetch = (version: number) => {
      shownVersionRef.current = version;
      bumpCoverageVersion();
      refreshFogLayers(map);
      refreshHeatmapLayers(map);
    };
    const check = () => {
      getCoverageStatus(controller.signal)
        .then(({ rendering, version }) => {
          polls += 1;
          if (rendering && polls < MAX_POLLS) {
            if (shownVersionRef.current === null) shownVersionRef.current = version;
            else if (version !== shownVersionRef.current) refetch(version);
            timer = window.setTimeout(check, POLL_MS);
            return;
          }
          refetch(version);
          const onDone = onDoneRef.current;
          onDoneRef.current = undefined;
          onDone?.();
        })
        .catch(() => {
          // A failed read just ends this watch; the next upload or delete starts another.
        });
    };
    check();
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [generation, map]);

  // A restart without its own `onDone` keeps the one still waiting (a Private location change's
  // list refresh shouldn't be dropped because a row then went Pending); it runs once, then clears.
  return useCallback((onDone?: () => void) => {
    if (onDone) onDoneRef.current = onDone;
    setGeneration((g) => g + 1);
  }, []);
}
