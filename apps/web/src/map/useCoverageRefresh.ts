import { useCallback, useEffect, useState } from 'react';
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
 */
export function useCoverageRefresh(map: MapLibreMap | null): () => void {
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    if (generation === 0 || !map) return;
    const controller = new AbortController();
    let timer: number | undefined;
    let polls = 0;
    const check = () => {
      getCoverageStatus(controller.signal)
        .then(({ rendering }) => {
          polls += 1;
          if (rendering && polls < MAX_POLLS) {
            timer = window.setTimeout(check, POLL_MS);
            return;
          }
          bumpCoverageVersion();
          refreshFogLayers(map);
          refreshHeatmapLayers(map);
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

  return useCallback(() => setGeneration((g) => g + 1), []);
}
