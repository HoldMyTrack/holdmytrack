import { useCallback, useEffect, useState } from 'react';
import { listActivities, type Activity, type ActivityQuery } from '../api';

export interface ActivityList {
  activities: Activity[];
  loading: boolean;
  error: string | null;
  /** Re-reads the list; the upload widget calls this once a job actually finishes. */
  reload: () => void;
}

/**
 * Reader for §4.7's `GET /v1/activities`. One request, the whole filtered set — the panel
 * renders every row, the map draws every track, and the type/distance facets are computed
 * over the same full list, so there was never a page-2 the app could get away without.
 *
 * The filter is compared by its serialised form, not by identity, so a caller passing a
 * fresh object literal every render doesn't restart the fetch on every render.
 */
export function useActivityList(query: ActivityQuery = {}): ActivityList {
  const queryKey = JSON.stringify(query);
  const [activities, setActivities] = useState<Activity[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    setLoading(true);
    const controller = new AbortController();
    listActivities(JSON.parse(queryKey) as ActivityQuery, controller.signal)
      .then((list) => {
        setActivities(list);
        setError(null);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (controller.signal.aborted) return;
        setLoading(false);
      });
    return () => controller.abort();
  }, [queryKey, nonce]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  return { activities, loading, error, reload };
}
