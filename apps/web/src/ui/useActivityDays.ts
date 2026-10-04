import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { getActivityDayPage, type HistogramBucket } from '../api';
import type { DateRange } from './dateMath';
import { shiftRange } from './rangeShift';

/**
 * The date slider's data and pan position, paged by *days that have activity*.
 *
 * The slider (DateRangeSlider.tsx) has one slot per day the user actually recorded and none at
 * all for the days in between, so "one window of days" is a count of real days, not a width of
 * calendar time. That is why this asks the backend for `days=N&before=X` rather than for a
 * calendar window: a page is exactly N days however sparse the history is. Asking for a
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
 * The pan position is an *anchor date* — the date of the leftmost visible day — rather than
 * an index, because prepending a page shifts every index by the size of that page. An anchor
 * names the same day before and after, so the view does not jump when history loads in
 * behind it.
 *
 * Nothing here touches the selected range. Panning changes which days are in the window and that
 * is all; the selection is MapView's state and paging back to a user's very first activity
 * leaves it exactly as it was.
 */

/** How many activity-days the slider's track shows edge to edge — ~18px a day on a phone, wide
 *  enough for two knobs one day apart to sit clearly side by side. */
export const WINDOW_DAYS = 15;

/** How many days-with-activity one request asks for — plenty of slack past one window, so
 *  paging back through history rarely has to wait on a round trip of its own. */
const PAGE_SIZE = 240;

export interface ActivityDaysState {
  /** The days currently in the slider's window: WINDOW_DAYS consecutive days-with-activity. */
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
  canPanEarlier: boolean;
  canPanLater: boolean;
  /** Moves the window by whole activity-days — negative is toward the past. */
  panBy: (deltaDays: number) => void;
  /** Moves the window to show `day` (YYYY-MM-DD), in its middle where there's room, loading
   *  older history first if it's further back than what's loaded — for a range picked from
   *  outside the slider (MapView's viewActivityOnDay), whose knobs would otherwise sit off the
   *  window's edges. */
  reveal: (day: string) => void;
  /** The range-shift buttons (rangeShift.ts): `range` moved its own length in activity-days,
   *  with the window moved just enough to show it — or null when there's nothing further that
   *  way, or when the days it needs are still loading (the next page is asked for; ask again). */
  shift: (range: DateRange, dir: -1 | 1) => DateRange | null;
  /** Whether `shift` has anywhere to go — true while the days it needs are only unloaded. */
  canShift: (range: DateRange, dir: -1 | 1) => boolean;
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
  // Date of the leftmost visible day. null means "pinned to the newest end", which is both
  // the opening position and where panning fully right returns to — so a day that arrives
  // after an upload shows up rather than sitting just off the right edge.
  const [anchor, setAnchor] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);
  // One extend request at a time. A second would be anchored at the same day as the first
  // and prepend the same page twice.
  const extending = useRef(false);
  // Days a pan asked for that weren't loaded yet, carried until they are. Only reachable by
  // clicking Earlier faster than the network answers — the prefetch effect below covers the
  // rest.
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

  const maxStart = Math.max(0, days.length - WINDOW_DAYS);
  // The anchor resolved against the current array. `>=` rather than an exact match so an
  // anchor day that somehow isn't in `days` still lands next to where it belongs.
  const start = useMemo(() => {
    if (anchor === null) return maxStart;
    const index = days.findIndex((day) => day.date >= anchor);
    return Math.min(index === -1 ? maxStart : index, maxStart);
  }, [days, anchor, maxStart]);

  const visibleDays = useMemo(() => days.slice(start, start + WINDOW_DAYS), [days, start]);

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

  // Start loading more history once the window is within one window's width of the loaded
  // edge, so Earlier normally lands on days that are already here.
  useEffect(() => {
    if (hasEarlier && start <= WINDOW_DAYS) extendEarlier();
  }, [hasEarlier, start, extendEarlier]);

  const panBy = useCallback(
    (deltaDays: number) => {
      const target = start + deltaDays;
      const next = Math.min(Math.max(target, 0), maxStart);
      carry.current = target < 0 && hasEarlier ? target : 0;
      setAnchor(next >= maxStart ? null : (days[next]?.date ?? null));
    },
    [days, start, maxStart, hasEarlier],
  );

  // Settle a carried pan once the page it was waiting on arrives. Deliberately keyed on
  // `days` alone: a carry exists precisely because the days it wanted weren't loaded, so a
  // newly prepended page is the only thing that can change the answer — and `panBy` closes
  // over the same fresh `days` this effect is reacting to.
  useEffect(() => {
    const owed = carry.current;
    if (owed >= 0) return;
    carry.current = 0;
    panBy(owed);
  }, [days]);

  // A day to show, until the window has moved to it — waiting for the first page, and for as
  // many older pages as it takes to reach it.
  const [revealing, setRevealing] = useState<string | null>(null);
  useEffect(() => {
    if (revealing === null || !ready) return;
    if (days.length === 0) {
      setRevealing(null);
      return;
    }
    if (days[0]!.date > revealing && hasEarlier) {
      extendEarlier();
      return;
    }
    let index = days.findIndex((day) => day.date >= revealing);
    if (index === -1) index = days.length - 1;
    const next = Math.min(Math.max(index - Math.floor(WINDOW_DAYS / 2), 0), maxStart);
    setAnchor(next >= maxStart ? null : days[next]!.date);
    setRevealing(null);
  }, [revealing, ready, days, hasEarlier, extendEarlier, maxStart]);
  const reveal = useCallback((day: string) => setRevealing(day), []);

  const dates = useMemo(() => days.map((day) => day.date), [days]);

  const shift = useCallback(
    (range: DateRange, dir: -1 | 1): DateRange | null => {
      const next = shiftRange(dates, hasEarlier, range, dir);
      if (next === 'load') {
        extendEarlier();
        return null;
      }
      if (next === null) return null;
      // Move the window only as far as it takes to show the new range; one longer than the
      // window shows its leading edge, the end it moved toward.
      const first = dates.indexOf(next.from);
      const last = dates.indexOf(next.to);
      let left = start;
      if (last - first + 1 > WINDOW_DAYS) left = dir < 0 ? first : last - WINDOW_DAYS + 1;
      else if (first < start) left = first;
      else if (last >= start + WINDOW_DAYS) left = last - WINDOW_DAYS + 1;
      left = Math.min(Math.max(left, 0), maxStart);
      if (left !== start) setAnchor(left >= maxStart ? null : dates[left]!);
      return next;
    },
    [dates, hasEarlier, extendEarlier, start, maxStart],
  );

  const canShift = useCallback(
    (range: DateRange, dir: -1 | 1) => shiftRange(dates, hasEarlier, range, dir) !== null,
    [dates, hasEarlier],
  );

  const reload = useCallback(() => setNonce((n) => n + 1), []);

  return {
    visibleDays,
    earliest,
    ready,
    error,
    canPanEarlier: start > 0 || hasEarlier,
    canPanLater: start < maxStart,
    panBy,
    reveal,
    shift,
    canShift,
    reload,
    generation: nonce,
  };
}
