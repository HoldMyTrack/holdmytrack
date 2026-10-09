import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { answerStorySend, listStorySends, type StoryInbox } from '../api';

/** How often the inbox is re-read while an accepted copy is still arriving — the copy is one
 *  ingest per activity, so a few seconds is the right order of magnitude. */
const COPYING_POLL_MS = 3000;

export interface StoryInboxState extends StoryInbox {
  /** Accepts (true) or declines one waiting copy; rejects with the server's message. */
  answer: (id: string, accept: boolean) => Promise<void>;
}

/**
 * Reader for `GET /v1/story-sends`, the Stories tab's inbox (`SPEC.md` FR-14.7): the copies of
 * Stories others sent, and the accepted ones still being copied. Read once on mount, for the
 * tab's count of waiting copies, then each time the tab opens (`enabled`) and every few seconds
 * while a copy is arriving; `onArrived` runs whenever one has, so the caller can read its
 * Stories and activities again.
 */
export function useStoryInbox(enabled: boolean, onArrived: () => void): StoryInboxState {
  const [inbox, setInbox] = useState<StoryInbox>({ sends: [], copying: [] });
  const [nonce, setNonce] = useState(0);
  const copyingRef = useRef(0);
  const readRef = useRef(false);
  const onArrivedRef = useRef(onArrived);
  onArrivedRef.current = onArrived;

  useEffect(() => {
    if (!enabled && readRef.current) return;
    const controller = new AbortController();
    listStorySends(controller.signal)
      .then((result) => {
        readRef.current = true;
        if (result.copying.length < copyingRef.current) onArrivedRef.current();
        copyingRef.current = result.copying.length;
        setInbox(result);
      })
      // The inbox is an extra on the tab: a failed read leaves the last one shown.
      .catch(() => {});
    return () => controller.abort();
  }, [enabled, nonce]);

  // Polled only while something is arriving.
  useEffect(() => {
    if (!enabled || inbox.copying.length === 0) return;
    const timer = window.setTimeout(() => setNonce((n) => n + 1), COPYING_POLL_MS);
    return () => window.clearTimeout(timer);
  }, [enabled, inbox]);

  const answer = useCallback(async (id: string, accept: boolean) => {
    await answerStorySend(id, accept);
    setNonce((n) => n + 1);
  }, []);
  return useMemo(() => ({ ...inbox, answer }), [inbox, answer]);
}
