import { useEffect, useState } from 'react';
import {
  getActivityGraphStats,
  getActivityYearGraph,
  type ActivityGraphStats,
  type ActivityYearGraph,
} from '../api';

export interface YearGraphState {
  graph: ActivityYearGraph | null;
  stats: ActivityGraphStats | null;
  error: string | null;
}

/**
 * One §4.8 year block's data: its day cells (for the grid) and its four stat numbers (for the
 * subtotal line), fetched together since a `YearGrid` needs both and neither alone is useful
 * to render. Two requests, not one — `getActivityYearGraph` is just §4.7's existing histogram
 * endpoint, and `getActivityGraphStats` is the new one; there's no single endpoint that
 * returns both, and inventing one would mean the histogram endpoint growing a special case
 * only this caller uses.
 */
export function useYearGraph(year: number): YearGraphState {
  const [graph, setGraph] = useState<ActivityYearGraph | null>(null);
  const [stats, setStats] = useState<ActivityGraphStats | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    setGraph(null);
    setStats(null);
    setError(null);
    Promise.all([getActivityYearGraph(year, controller.signal), getActivityGraphStats(year, controller.signal)])
      .then(([g, s]) => {
        setGraph(g);
        setStats(s);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, [year]);

  return { graph, stats, error };
}
