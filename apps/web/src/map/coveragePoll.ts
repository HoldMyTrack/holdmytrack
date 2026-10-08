/** How often the coverage status is re-read while a re-render is outstanding, at first. */
export const FAST_POLL_MS = 2000;
/** How many reads stay at FAST_POLL_MS (~3 minutes) before the watch slows down. */
export const FAST_POLLS = 90;
/** The interval after that. The watch never gives up: a big import can keep the worker busy
 *  for far longer than three minutes, and the final refetch (and the map notice clearing)
 *  only happens on the read that finds nothing left. */
export const SLOW_POLL_MS = 10_000;

/** The delay before the next read, after `polls` reads so far (a failed read counts as one). */
export function coveragePollDelay(polls: number): number {
  return polls < FAST_POLLS ? FAST_POLL_MS : SLOW_POLL_MS;
}
