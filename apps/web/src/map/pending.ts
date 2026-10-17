/**
 * What a newly fetched list says about reprocesses (a track edit, a Private location change):
 * which started, which finished. Android: `map/PendingDiff.kt`.
 *
 * Trap: a job often finishes before the list reload after Apply returns, so its row is never
 * seen Pending. An activity applied here (`awaiting`) therefore also counts as finished the first
 * time a list fetched after Apply shows it not pending.
 */
export interface PendingChange {
  /** The ids pending now. */
  now: Set<string>;
  /** A row entered Pending. */
  started: boolean;
  /** A row left Pending, or an awaited edit landed. */
  finished: boolean;
  /** Ids that were pending and no longer are. */
  left: Set<string>;
  /** Awaited ids that landed with this list. */
  landed: Set<string>;
  /** Awaited ids still pending. */
  awaiting: Set<string>;
}

export function pendingChange(previous: ReadonlySet<string>, pendingIds: Iterable<string>, awaiting: ReadonlySet<string>): PendingChange {
  const now = new Set(pendingIds);
  const left = new Set([...previous].filter((id) => !now.has(id)));
  const started = [...now].some((id) => !previous.has(id));
  const landed = new Set([...awaiting].filter((id) => !now.has(id)));
  const stillAwaiting = new Set([...awaiting].filter((id) => now.has(id)));
  return { now, started, finished: left.size > 0 || landed.size > 0, left, landed, awaiting: stillAwaiting };
}
