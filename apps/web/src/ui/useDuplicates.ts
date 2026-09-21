import { useCallback, useEffect, useState } from 'react';
import { getDuplicates, type DuplicateActivity } from '../api';

export interface DuplicatesState {
  duplicates: DuplicateActivity[];
  loading: boolean;
  error: string | null;
  /** Re-reads the list — called after an upload/sync completes, so a duplicate caught by that
   *  ingest shows up without a manual reload. */
  refresh: () => void;
}

/**
 * Backs the Activities panel's "Duplicates" affordance (`docs/ROADMAP.md`'s "Surface
 * superseded activities" item, `docs/SPEC.md` FR-3.7's own "Not yet built" note) — a plain
 * one-shot read of `GET /v1/activities/duplicates`, not a poller: unlike upload history, a
 * duplicate is settled the instant ingest resolves it, so there's no in-progress state to
 * watch for. `refresh()` lets the caller re-read after something that could produce a new one
 * (an upload or sync finishing) instead of polling for it.
 */
export function useDuplicates(): DuplicatesState {
  const [duplicates, setDuplicates] = useState<DuplicateActivity[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    getDuplicates(controller.signal)
      .then((result) => {
        setDuplicates(result);
        setError(null);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [nonce]);

  const refresh = useCallback(() => setNonce((n) => n + 1), []);
  return { duplicates, loading, error, refresh };
}
