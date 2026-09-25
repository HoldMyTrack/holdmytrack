import { useEffect, useState } from 'react';
import { getActivityTrends, type ActivityTrends, type TrendBucket } from '../api';

export interface TrendsState {
  bucket: TrendBucket;
  setBucket: (bucket: TrendBucket) => void;
  trends: ActivityTrends | null;
  error: string | null;
}

/**
 * Owns the Week/Month toggle and fetches docs/SPEC.md FR-9's trends for it — same shape
 * as useYearGraph.ts: refetch on bucket change, abort the in-flight request if the bucket
 * changes again before it resolves.
 */
export function useTrends(): TrendsState {
  const [bucket, setBucket] = useState<TrendBucket>('week');
  const [trends, setTrends] = useState<ActivityTrends | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    setTrends(null);
    setError(null);
    getActivityTrends(bucket, controller.signal)
      .then(setTrends)
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, [bucket]);

  return { bucket, setBucket, trends, error };
}
