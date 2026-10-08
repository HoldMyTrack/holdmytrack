import { useCallback, useEffect, useRef, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { getCoverageStatus } from '../api';
import { coveragePollDelay } from './coveragePoll';
import { getTileVersion, setTileVersion } from './coverageVersion';
import { refreshFogLayers } from './fog';
import { refreshHeatmapLayers } from './heatmap';

/**
 * Keeps the Fog/Heatmap layers current after an upload or delete (IMPLEMENTATION.md §4.2.3).
 * Both rasters are re-rendered by a queued `render_fog` job, and their tile URLs never change
 * on their own, so an open page would otherwise show pre-change coverage until a reload.
 *
 * The returned `watch()` starts polling `GET /v1/coverage/status` and, once the account has
 * no coverage-changing job left, takes its tile version (coverageVersion.ts) and refetches
 * every Fog/Heatmap source. Each call restarts the watch rather than joining one already running: a second
 * upload or delete can enqueue its jobs just after an in-flight read already came back
 * "done", and restarting is what guarantees that read isn't the last word.
 *
 * While it waits, it also refetches whenever the status's `version` changes: a track edit or
 * Private location change renders once with its Pending activities left out of coverage and
 * again once they're back (IMPLEMENTATION.md §4.7.7), and the first of those is only shown if
 * picked up mid-job. The first read of a page only sets the baseline.
 *
 * It also watches once on its own as soon as the map exists, so a page opened while the
 * account's coverage is still being worked on gets its final refetch too; that first watch
 * refetches nothing if it finds the page's own tile version already current. Reads go every
 * 2 s and then every 10 s (coveragePoll.ts) until nothing is left, a failed read included.
 * `rendering` is what the latest read said, for the map's "still being updated" notice.
 *
 * `onDone`, when given, runs after that refetch — for a caller whose change also moves things
 * the coverage rasters don't cover (a Private location change reprocesses whole activities),
 * and which can't otherwise tell when the server has finished.
 */
export interface CoverageRefresh {
  /** Starts (or restarts) the watch — see useCoverageRefresh. */
  watch: (onDone?: () => void) => void;
  /** Whether the latest status read found coverage still being worked on. */
  rendering: boolean;
}

export function useCoverageRefresh(map: MapLibreMap | null): CoverageRefresh {
  const [generation, setGeneration] = useState(0);
  const [rendering, setRendering] = useState(false);
  const onDoneRef = useRef<(() => void) | undefined>(undefined);
  // The status version the layers were last refetched at (or first seen at) — shared across
  // watches, since a restarted watch is still showing the same tiles.
  const shownVersionRef = useRef<number | null>(null);

  useEffect(() => {
    if (!map) return;
    // The watch the map's own load starts, before any change has asked for one.
    const initial = generation === 0;
    const controller = new AbortController();
    let timer: number | undefined;
    let polls = 0;
    const refetch = (version: number, tileVersion: string) => {
      shownVersionRef.current = version;
      setTileVersion(tileVersion);
      refreshFogLayers(map);
      refreshHeatmapLayers(map);
    };
    const check = () => {
      getCoverageStatus(controller.signal)
        .then(({ rendering: busy, version, tile_version: tileVersion }) => {
          polls += 1;
          setRendering(busy);
          if (busy) {
            if (shownVersionRef.current === null) shownVersionRef.current = version;
            else if (version !== shownVersionRef.current) refetch(version, tileVersion);
            timer = window.setTimeout(check, coveragePollDelay(polls));
            return;
          }
          if (initial && polls === 1 && tileVersion === getTileVersion()) {
            // A page loaded with nothing in flight already shows current tiles.
            shownVersionRef.current = version;
            return;
          }
          refetch(version, tileVersion);
          const onDone = onDoneRef.current;
          onDoneRef.current = undefined;
          onDone?.();
        })
        .catch(() => {
          if (controller.signal.aborted) return;
          polls += 1;
          timer = window.setTimeout(check, coveragePollDelay(polls));
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
  const watch = useCallback((onDone?: () => void) => {
    if (onDone) onDoneRef.current = onDone;
    setGeneration((g) => g + 1);
  }, []);
  return { watch, rendering };
}
