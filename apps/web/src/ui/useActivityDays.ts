import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { getActivityDayPage, type HistogramBucket } from '../api';

/**
 * The range picker's data and pan position, paged by *days that have activity*.
 *
 * The strip draws one bar per day the user actually recorded and nothing at all for the days
 * in between (see RangePicker.tsx), so "one screen of bars" is a count of real days, not a
 * width of calendar time. That is why this asks the backend for `days=N&before=X` rather than
 * for a calendar window: a page is exactly N bars however sparse the history is. Asking for a
 * window instead would mean guessing a `from`/`to`, counting what came back, and widening the
 * guess until it was enough — several round trips per pan step for a user who rides once a
 * month, and a different wrong guess for every user. (That was the alternative; §4.7 records
 * why this one won.)
 *
 * Paging only ever goes backwards, which is why there is no `after=` request: the first page
 * is the most recent N days, so everything between wherever the view sits and today is
 * already in hand. `days` below is therefore one contiguous run of activity-days ending at
 * the user's most recent one, grown by prepending.
 *
 * The pan position is an *anchor date* — the date of the leftmost visible bar — rather than
 * an index, because prepending a page shifts every index by the size of that page. An anchor
 * names the same bar before and after, so the view does not jump when history loads in
 * behind it.
 *
 * Nothing here touches the selected range. Panning changes which bars are on screen and that
 * is all; the selection is MapView's state and paging back to a user's very first activity
 * leaves it exactly as it was.
 */

/** How many bars the strip shows at once — one "page", and one Earlier/Later click's step.
 *  A tightly-packed strip needs far more days in the same footer width than 45 ever
 *  could without each bar floating in a slot much wider than itself (confirmed live: at 45,
 *  a full screen's 22px-capped bars sat in ~35px slots, a ~13px gap around each one). Raised
 *  so the on-screen density is actually tight; see `.range-picker__bar`'s own `max-width` for
 *  the other half of this. */
const BARS_PER_VIEW = 120;

/** How many days-with-activity one request asks for. Two screens' worth, so the slack kept
 *  by PREFETCH_MARGIN below survives a click without a round trip of its own. */
const PAGE_SIZE = BARS_PER_VIEW * 2;

/** Start loading more history once the view is within this many bars of the loaded edge —
 *  one full click's worth, so Earlier normally lands on bars that are already here. */
const PREFETCH_MARGIN = BARS_PER_VIEW;

export interface ActivityDaysState {
  /** The bars currently on screen: a packed slice of consecutive days-with-activity. */
  visibleDays: HistogramBucket[];
  /** This user's first activity's UTC day, or null until the first page lands — and
   *  permanently null for a user who has no activities at all. */
  earliest: string | null;
  /** True once a first page has come back, however empty. Distinct from `earliest`, which a
   *  user with no history never gets, so a caller waiting to resolve its own default
   *  selection (MapView's is the 5 most recent activity-days) has something that actually
   *  resolves. */
  ready: boolean;
  error: string | null;
  /** How far one Earlier/Later click moves, in bars. */
  pageStep: number;
  canPanEarlier: boolean;
  canPanLater: boolean;
  /** Moves the view by whole bars — negative is toward the past. */
  panBy: (deltaBars: number) => void;
  /** Re-reads from the newest end; the upload widget calls this once a job finishes. */
  reload: () => void;
  /** Bumps by exactly one on every `reload()` (not on `panBy`, which never refetches — see
   *  panBy's own comment) — MapView's own default-date-range effect depends on this rather
   *  than on `visibleDays` directly, so a fresh upload's data can re-trigger it without a
   *  routine pan also doing so (panning changes `visibleDays` just as much as a reload does,
   *  but must never re-pick the selection out from under a user who's just browsing). */
  generation: number;
}

export function useActivityDays(): ActivityDaysState {
  const [days, setDays] = useState<HistogramBucket[]>([]);
  const [earliest, setEarliest] = useState<string | null>(null);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Date of the leftmost visible bar. null means "pinned to the newest end", which is both
  // the opening position and where panning fully right returns to — so a day that arrives
  // after an upload shows up rather than sitting just off the right edge.
  const [anchor, setAnchor] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);
  // One extend request at a time. A second would be anchored at the same day as the first
  // and prepend the same page twice.
  const extending = useRef(false);
  // Bars a pan asked for that weren't loaded yet, carried until they are. Only reachable by
  // clicking Earlier faster than the network answers — PREFETCH_MARGIN covers the rest.
  const carry = useRef(0);

  useEffect(() => {
    const controller = new AbortController();
    getActivityDayPage({ limit: PAGE_SIZE }, controller.signal)
      .then((page) => {
        setDays(page.days);
        setEarliest(page.earliest);
        setAnchor(null);
        setError(null);
        setReady(true);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, [nonce]);

  const maxStart = Math.max(0, days.length - BARS_PER_VIEW);
  // The anchor resolved against the current array. `>=` rather than an exact match so an
  // anchor day that somehow isn't in `days` still lands next to where it belongs.
  const start = useMemo(() => {
    if (anchor === null) return maxStart;
    const index = days.findIndex((day) => day.date >= anchor);
    return Math.min(index === -1 ? maxStart : index, maxStart);
  }, [days, anchor, maxStart]);

  const visibleDays = useMemo(() => days.slice(start, start + BARS_PER_VIEW), [days, start]);

  // Whether there is any history at all behind what's loaded. Derived rather than tracked as
  // a flag: a short page is what running out looks like, and then days[0] *is* `earliest`.
  const hasEarlier = days.length > 0 && earliest !== null && days[0]!.date > earliest;

  const extendEarlier = useCallback(() => {
    const oldest = days[0]?.date;
    if (!oldest || extending.current) return;
    extending.current = true;
    getActivityDayPage({ limit: PAGE_SIZE, before: oldest })
      .then((page) => {
        setDays((prev) => (prev[0]?.date === oldest ? [...page.days, ...prev] : prev));
        setEarliest(page.earliest);
        setError(null);
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => {
        extending.current = false;
      });
  }, [days]);

  useEffect(() => {
    if (hasEarlier && start <= PREFETCH_MARGIN) extendEarlier();
  }, [hasEarlier, start, extendEarlier]);

  const panBy = useCallback(
    (deltaBars: number) => {
      const target = start + deltaBars;
      const next = Math.min(Math.max(target, 0), maxStart);
      carry.current = target < 0 && hasEarlier ? target : 0;
      setAnchor(next >= maxStart ? null : (days[next]?.date ?? null));
    },
    [days, start, maxStart, hasEarlier],
  );

  // Settle a carried pan once the page it was waiting on arrives. Deliberately keyed on
  // `days` alone: a carry exists precisely because the bars it wanted weren't loaded, so a
  // newly prepended page is the only thing that can change the answer — and `panBy` closes
  // over the same fresh `days` this effect is reacting to.
  useEffect(() => {
    const owed = carry.current;
    if (owed >= 0) return;
    carry.current = 0;
    panBy(owed);
  }, [days]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);

  return {
    visibleDays,
    earliest,
    ready,
    error,
    pageStep: BARS_PER_VIEW,
    canPanEarlier: start > 0 || hasEarlier,
    canPanLater: start < maxStart,
    panBy,
    reload,
    generation: nonce,
  };
}
