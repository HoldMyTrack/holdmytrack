import { useCallback, useEffect, useState } from 'react';
import { getActivityTotals, type ActivityQuery, type ActivityTotals } from '../api';

export interface ActivityTotalsState {
  /** null until the first response lands, and on error — callers render a placeholder. */
  totals: ActivityTotals | null;
  error: string | null;
  /** Re-reads the summary; the upload widget calls this once a job actually finishes. */
  reload: () => void;
}

/**
 * Reader for §4.7's `GET /v1/activities/summary`. One request, one row back — the header
 * badge, the panel subtext and the histogram's stats line all read this same response
 * rather than each deriving totals from whatever rows happen to be loaded.
 */
export function useActivityTotals(query: ActivityQuery = {}): ActivityTotalsState {
  const queryKey = JSON.stringify(query);
  const [totals, setTotals] = useState<ActivityTotals | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    getActivityTotals(JSON.parse(queryKey) as ActivityQuery, controller.signal)
      .then((value) => {
        setTotals(value);
        setError(null);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setTotals(null);
        setError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, [queryKey, nonce]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  return { totals, error, reload };
}
