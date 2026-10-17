import { useEffect, useMemo, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { clearTrackBands, setTrackBands } from './trackBands';
import { getActivityTrackMetrics, type ActivityTrackMetrics } from '../api';

/**
 * The focused activity's pace-colored segments (trackBands.ts). The metrics are fetched when the
 * focus moves, or [version] changes after a reprocess; until they land the previous activity's may
 * be in hand, so drawing checks `activityId` against the focus. Not drawn while the track is hidden
 * (the bands are a layer above it and would keep painting it) or Pending (they'd paint the old
 * shape over the new one). Returns the bands drawn, which a style reload redraws.
 */
export function useTrackMetrics(
  map: MapLibreMap | null,
  focusedId: string | null,
  version: number,
  mapHiddenIds: ReadonlySet<string>,
  pendingIds: readonly string[],
): ActivityTrackMetrics | null {
  const [metrics, setMetrics] = useState<ActivityTrackMetrics | null>(null);
  useEffect(() => {
    if (!focusedId) {
      setMetrics(null);
      return;
    }
    const controller = new AbortController();
    getActivityTrackMetrics(focusedId, controller.signal)
      .then(setMetrics)
      .catch(() => {
        if (!controller.signal.aborted) setMetrics(null);
      });
    return () => controller.abort();
  }, [focusedId, version]);

  const bands = useMemo(() => {
    if (focusedId === null || metrics?.activityId !== focusedId) return null;
    if (mapHiddenIds.has(focusedId) || pendingIds.includes(focusedId)) return null;
    return metrics;
  }, [metrics, focusedId, mapHiddenIds, pendingIds]);
  useEffect(() => {
    if (!map) return;
    if (bands) setTrackBands(map, bands.points);
    else clearTrackBands(map);
  }, [map, bands]);
  return bands;
}
