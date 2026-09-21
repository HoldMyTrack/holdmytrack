import { useCallback, useEffect, useRef, useState } from 'react';
import { getUploadHistory, type UploadHistoryPage } from '../api';

const PAGE_SIZE = 5;
const POLL_INTERVAL_MS = 1500;

export interface UploadHistoryState {
  page: UploadHistoryPage | null;
  error: string | null;
  offset: number;
  setOffset: (offset: number) => void;
  /** Re-reads the current page immediately — called right after an upload's own request
   *  resolves, so a freshly enqueued job shows up without waiting for the next poll tick. */
  refresh: () => void;
}

/**
 * Backs ImportPanel.tsx's Files/Sync tabs (§4.0.1) — a paginated read of `GET /v1/uploads`
 * that also polls while anything is still processing, so a row visibly flips from
 * "Processing" to "Ready"/"Failed" without the panel needing to be closed and reopened.
 * Polling is keyed on the response's own `processing` count (every pending job for this
 * user, not just this page) rather than scanning this page's rows for one still processing:
 * a page with nothing in-flight on it shouldn't keep polling just because some *other* page
 * does, but it also shouldn't stay silent while page 2 is mid-upload and page 1 is what's on
 * screen — polling this same page is the simplest thing that keeps both cases correct,
 * since whichever page is open is the one that needs to notice a change.
 *
 * One call per tab, both always mounted regardless of which tab is currently showing — a
 * Files-tab job finishing while the Sync tab happens to be open still has to reach `onPoll`
 * (which refreshes the map), so polling can't be conditional on tab visibility the way the
 * *rendered* rows already are. `sources` (a stable array reference — pass a module-level
 * constant, not an inline literal, or its identity changing every render would restart the
 * poll loop) scopes which rows this instance's own page counts; omit it for the unfiltered
 * combined view.
 *
 * `onPoll` fires after every *poll-driven* read (not the initial one, and not the one
 * `refresh()` triggers) — the one reliable signal that a job may have actually finished
 * server-side, as opposed to just been enqueued. MapView's own "a new track landed" refresh
 * (Activities list, totals, histogram, the tracks tile layer) used to hang off `refresh()`
 * instead, which fires the instant the upload HTTP request returns — before the worker has
 * parsed the file and inserted its `activities` row at all. Reported live as exactly that: a
 * freshly uploaded track missing from the Activities list until a manual page reload, by
 * which time ingest had long since finished. This is what actually closes that gap: every
 * ~1.5s while something is still processing, the caller gets a chance to notice it's done.
 */
export function useUploadHistory(sources?: readonly string[], onPoll?: () => void): UploadHistoryState {
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<UploadHistoryPage | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);
  const pollingRef = useRef(false);
  // Read inside the effect without being one of its dependencies — a fresh onPoll identity
  // every MapView render must not restart the whole poll loop (new AbortController, losing
  // track of an in-flight request) the way including it directly in the deps array would.
  const onPollRef = useRef(onPoll);
  onPollRef.current = onPoll;

  useEffect(() => {
    const controller = new AbortController();
    let cancelled = false;

    const load = (polled: boolean) => {
      getUploadHistory({ limit: PAGE_SIZE, offset, ...(sources ? { sources } : {}) }, controller.signal)
        .then((result) => {
          if (cancelled) return;
          setPage(result);
          setError(null);
          pollingRef.current = result.processing > 0;
          if (polled) onPollRef.current?.();
          if (pollingRef.current) window.setTimeout(poll, POLL_INTERVAL_MS);
        })
        .catch((err: unknown) => {
          if (cancelled || controller.signal.aborted) return;
          setError(err instanceof Error ? err.message : String(err));
        });
    };
    const poll = () => {
      if (cancelled || !pollingRef.current) return;
      load(true);
    };

    load(false);
    return () => {
      cancelled = true;
      controller.abort();
    };
  }, [offset, nonce, sources]);

  const refresh = useCallback(() => setNonce((n) => n + 1), []);
  return { page, error, offset, setOffset, refresh };
}
