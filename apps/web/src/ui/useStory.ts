import { useCallback, useEffect, useState } from 'react';
import { getStory, StoryNotFoundError, type Story } from '../api';

export interface StoryState {
  /** null outside a Story, until it has loaded, and after an error. */
  story: Story | null;
  error: string | null;
  /** The Story doesn't exist, or isn't this account's (FR-14.5). */
  notFound: boolean;
  /** Re-reads it — after anything that changes its activities or their numbers. */
  reload: () => void;
  /** Replaces it with a copy a write already returned (a rename, a removal). */
  set: (story: Story) => void;
}

/** Reader for `GET /v1/stories/{id}` — the Story view's header (FR-14.1): its name, description
 *  and whole-Story statistics, whatever the date range. */
export function useStory(id: string | null): StoryState {
  const [story, setStory] = useState<Story | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    setError(null);
    setNotFound(false);
    if (id === null) {
      setStory(null);
      return;
    }
    // A different Story's header must not linger while this one loads; a reload of the same
    // one keeps what's shown until the new copy lands.
    setStory((current) => (current?.id === id ? current : null));
    const controller = new AbortController();
    getStory(id, controller.signal)
      .then(setStory)
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setStory(null);
        setNotFound(err instanceof StoryNotFoundError);
        setError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, [id, nonce]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  return { story, error, notFound, reload, set: setStory };
}
