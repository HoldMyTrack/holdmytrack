import { useEffect, useState } from 'react';
import { getBestEfforts, type ActivityBestEfforts, type BestEffortMetric } from '../api';

export interface BestEffortsState {
  metric: BestEffortMetric;
  setMetric: (metric: BestEffortMetric) => void;
  bestEfforts: ActivityBestEfforts | null;
  error: string | null;
}

/**
 * Owns the Pace/Heart rate toggle and fetches VISION.md §5.3's best-effort curve for
 * it — same shape as useTrends.ts: refetch on metric change, abort the in-flight request if
 * the metric changes again before it resolves.
 */
export function useBestEfforts(): BestEffortsState {
  const [metric, setMetric] = useState<BestEffortMetric>('pace');
  const [bestEfforts, setBestEfforts] = useState<ActivityBestEfforts | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    setBestEfforts(null);
    setError(null);
    getBestEfforts(metric, controller.signal)
      .then(setBestEfforts)
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, [metric]);

  return { metric, setMetric, bestEfforts, error };
}
