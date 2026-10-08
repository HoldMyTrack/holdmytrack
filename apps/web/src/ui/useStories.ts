import { useCallback, useEffect, useState } from 'react';
import { listStories, type Story } from '../api';

export interface StoriesState {
  /** Newest first (`created_at`), as `GET /v1/stories` returns them. */
  stories: Story[];
  /** A list fetched since the tab last opened is in hand. */
  ready: boolean;
  error: string | null;
  /** Replaces one Story with a copy a write already returned (a rename). */
  replace: (story: Story) => void;
  /** Drops a deleted Story from the list in hand, rather than refetching: a refetch leaves the
   *  old list, deleted Story included, ready until it lands. */
  remove: (id: string) => void;
  /** Reads the list again, keeping the one in hand until it lands — a copy someone sent has
   *  arrived (useStoryInbox). */
  reload: () => void;
}

/** Reader for `GET /v1/stories`, the Stories tab's list (`SPEC.md` FR-14.6). Fetched each time
 *  the tab opens (`enabled`), so a Story made or changed elsewhere — Create story included — is
 *  there without a reload. */
export function useStories(enabled: boolean): StoriesState {
  const [stories, setStories] = useState<Story[]>([]);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Bumped by reload: a read that keeps `ready`, unlike the tab opening.
  const [reloads, setReloads] = useState(0);

  useEffect(() => setReady(false), [enabled]);

  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    listStories(controller.signal)
      .then((result) => {
        setStories(result);
        setError(null);
        setReady(true);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, [enabled, reloads]);

  const replace = useCallback(
    (story: Story) => setStories((current) => current.map((s) => (s.id === story.id ? story : s))),
    [],
  );
  const remove = useCallback((id: string) => setStories((current) => current.filter((s) => s.id !== id)), []);
  const reload = useCallback(() => setReloads((n) => n + 1), []);
  return { stories, ready, error, replace, remove, reload };
}