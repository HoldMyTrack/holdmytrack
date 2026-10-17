import { useCallback, useEffect, useRef } from 'react';
import { pendingChange } from './pending';

/** How often the list is re-read while a reprocess is pending: one activity's ingest takes seconds. */
const POLL_MS = 2000;

export interface PendingPollHandlers {
  reload: () => void;
  /** A row entered Pending: it was drawn from tiles fetched before, and the server now leaves it out. */
  onStarted: () => void;
  /** A row left Pending, or an awaited edit landed: it has new points. */
  onFinished: () => void;
}

/**
 * Rows with a reprocess running (a track edit, a Private location change): while any is, the list
 * is read again every few seconds, keyed on each fetched list, so polling stops by itself. An edit
 * applied here ([awaitEdit]) counts as finished once a later list shows it not pending
 * (pending.ts). Each fetched list is a new `pendingIds` array, and Apply's fetch aborts earlier
 * ones, so every list seen after Apply was fetched after it.
 */
export function usePendingPoll(pendingIds: string[], handlers: PendingPollHandlers): (id: string) => void {
  const handlersRef = useRef(handlers);
  handlersRef.current = handlers;

  useEffect(() => {
    if (pendingIds.length === 0) return;
    const timer = window.setTimeout(() => handlersRef.current.reload(), POLL_MS);
    return () => window.clearTimeout(timer);
  }, [pendingIds]);

  const awaitingRef = useRef<Set<string>>(new Set());
  const previousRef = useRef<ReadonlySet<string>>(new Set());
  const lastRef = useRef<string[] | null>(null);
  useEffect(() => {
    // Only a newly fetched list says anything new.
    if (pendingIds === lastRef.current) return;
    lastRef.current = pendingIds;
    const change = pendingChange(previousRef.current, pendingIds, awaitingRef.current);
    previousRef.current = change.now;
    awaitingRef.current = change.awaiting;
    if (change.started) handlersRef.current.onStarted();
    if (change.finished) handlersRef.current.onFinished();
  }, [pendingIds]);

  return useCallback((id: string) => {
    awaitingRef.current.add(id);
  }, []);
}
