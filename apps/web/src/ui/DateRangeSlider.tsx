import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent, MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent } from 'react';
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-react';
import type { HistogramBucket } from '../api';
import { formatDayLabel } from './format';
import type { DateRange } from './dateMath';
import { t, tn } from '../i18n';

export interface DateRangeSliderProps {
  /** The window: WINDOW_DAYS consecutive days-with-activity, ascending — useActivityDays'
   *  `visibleDays`. */
  days: HistogramBucket[];
  /** Moves the window by whole activity-days — negative is toward the past. */
  onPan: (deltaDays: number) => void;
  canPanEarlier: boolean;
  canPanLater: boolean;
  /** The range-shift buttons: `range` moved its own length in activity-days, the window
   *  following it — or null when it can't move (useActivityDays' `shift`). */
  onShift: (range: DateRange, dir: -1 | 1) => DateRange | null;
  canShift: (range: DateRange, dir: -1 | 1) => boolean;
  value: DateRange;
  onChange: (next: DateRange) => void;
}

type Knob = 'start' | 'end';

type Tick = 'day' | 'gap' | 'month';

const DAY_MS = 86_400_000;

/** The kind of tick on each slot boundary, 0..days.length — the two outer edges are plain. */
function ticksOf(days: HistogramBucket[]): Tick[] {
  return Array.from({ length: days.length + 1 }, (_, b) => {
    const before = days[b - 1]?.date;
    const after = days[b]?.date;
    if (!before || !after) return 'day';
    if (before.slice(0, 7) !== after.slice(0, 7)) return 'month';
    return Date.parse(after) - Date.parse(before) > DAY_MS ? 'gap' : 'day';
  });
}

/** How far one Earlier/Later tap moves the window, in activity-days. */
const STEP_DAYS = 5;

/** Press-and-hold on Earlier/Later: the first step is immediate, then it repeats. */
const REPEAT_DELAY_MS = 400;
const REPEAT_INTERVAL_MS = 180;

/**
 * The date-range control (`SPEC.md` FR-6), on the Activities tab alone: a plain two-knob slider
 * in one row, « ‹ track › », with the selected dates under it — the look of the Activities
 * panel's DistanceFilter.tsx (its `.activity-filters__track`/`__fill` rules) with finger-height
 * knobs. The same control on a desktop and a phone; only where it sits differs (index.css).
 *
 * **Activity-days, not calendar days**: one slot per day the user recorded something, none for
 * the days between, so a quiet month costs no track. The window, its paging and the history
 * fetches behind them are useActivityDays'.
 *
 * **Knobs sit on slot boundaries, not on slots.** The start knob marks where the first selected
 * day begins and the end knob where the last one ends, so a one-day selection has its knobs one
 * slot apart and the two can never be dragged closer than that — there's no zero-width
 * selection to reach, and the knobs never sit on top of each other.
 *
 * **Paging.** Earlier/Later move the window STEP_DAYS per tap (repeating while held). A knob
 * sitting on the edge the window moves toward is pulled along with it, which is how a selection
 * grows past the window: park the start knob on the left edge and tap Earlier. The knob on the
 * side being moved toward therefore never scrolls out of view; only the far one can, and it
 * simply isn't drawn until the window comes back to it, its date still in the label under the
 * track. The pull lands once the moved window has rendered (the days before it may still be
 * loading), so a tap's commit waits for that too.
 *
 * Hand-rolled pointer handling on the whole track rather than two stacked native range inputs
 * (which always hand a drag to whichever input is on top): a press moves whichever knob is
 * nearer — jumping it to the pressed boundary if the press is on bare track — and a drag
 * carries it. Each knob is also a focusable `role="slider"`.
 *
 * Knob drags and held buttons render from a local draft and commit only on release, so the
 * activity list isn't refetched for every day passed.
 *
 * **Range shift.** The inner pair, ‹ ›, move the whole selection by its own length in
 * activity-days, packed against the old range (rangeShift.ts), and the window follows — the
 * outer pair, « » (Earlier/Later), move the scale. A tap commits;
 * a hold repeats like Earlier/Later and commits on release.
 *
 * **Day marks.** A tick on every slot boundary — where a knob can land — taller where the month
 * changes, and doubled (an axis break) where the two neighbouring activity-days aren't
 * consecutive calendar days, so packed slots don't pass for an unbroken run. Ticks inside the
 * selection take the accent.
 */
export function DateRangeSlider({
  days,
  onPan,
  canPanEarlier,
  canPanLater,
  onShift,
  canShift,
  value,
  onChange,
}: DateRangeSliderProps) {
  const trackRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ pointerId: number; knob: Knob } | null>(null);
  const repeatRef = useRef<{ timer: number } | null>(null);
  /** A pan asked for and not yet rendered, the knobs it pulls along, and whether the gesture
   *  has ended and should commit once it lands. */
  const panRef = useRef<{ start: boolean; end: boolean; commit: boolean } | null>(null);

  const [draft, setDraft] = useState<DateRange | null>(null);
  const current = draft ?? value;

  const n = days.length;
  const firstDay = days[0]?.date;
  const lastDay = days[n - 1]?.date;

  // ---- Dates ↔ slot boundaries ----------------------------------------------------------
  // Boundary b is where slot b begins (0..n). A date before the window is -1 and one after it
  // is n + 1 — off-screen — unless there's no history on that side, where it sits on the edge:
  // today with nothing recorded yet today still has its end knob on the right edge.

  const startOf = (from: string) => {
    if (!firstDay || !lastDay) return 0;
    if (from < firstDay) return canPanEarlier ? -1 : 0;
    const i = days.findIndex((d) => d.date >= from);
    return i === -1 ? (canPanLater ? n + 1 : n) : i;
  };
  const endOf = (to: string) => {
    if (!firstDay || !lastDay) return 0;
    if (to > lastDay) return canPanLater ? n + 1 : n;
    if (to < firstDay) return canPanEarlier ? -1 : 0;
    return days.filter((d) => d.date <= to).length;
  };

  const start = startOf(current.from);
  const end = endOf(current.to);
  const clamp = (b: number) => Math.min(Math.max(b, 0), n);
  const percent = (b: number) => (n === 0 ? 0 : (clamp(b) / n) * 100);

  /** Moves one knob to boundary `b`, a slot from the other (pushing it if needed). A knob
   *  that ends up where it already was keeps its own date, which may lie off-screen or in a
   *  gap between activity-days — only a knob that actually moved takes a slot's date. */
  const moveKnob = (knob: Knob, b: number, sel: DateRange): DateRange => {
    if (n === 0) return sel;
    const s0 = startOf(sel.from);
    const e0 = endOf(sel.to);
    let s = s0;
    let e = e0;
    if (knob === 'start') {
      s = Math.min(Math.max(b, 0), n - 1);
      e = Math.max(e0, s + 1);
    } else {
      e = Math.min(Math.max(b, 1), n);
      s = Math.min(s0, e - 1);
    }
    return {
      from: s === s0 ? sel.from : days[s]!.date,
      to: e === e0 ? sel.to : days[e - 1]!.date,
    };
  };

  // Latest values for the press-and-hold timer and the pull effect, which outlive the render
  // that started them.
  const live = useRef({ current, canPanEarlier, canPanLater, onPan, onShift, startOf, endOf, n });
  live.current = { current, canPanEarlier, canPanLater, onPan, onShift, startOf, endOf, n };

  const commit = (next: DateRange | null) => {
    setDraft(null);
    if (!next || (next.from === value.from && next.to === value.to)) return;
    onChange(next);
  };

  // ---- Earlier / Later ------------------------------------------------------------------

  // A pan has rendered: pull the knobs it was carrying onto the new edges, then commit if
  // the gesture that asked for it is already over.
  useEffect(() => {
    const pan = panRef.current;
    if (!pan || !firstDay || !lastDay) return;
    let next = live.current.current;
    if (pan.start) next = { from: firstDay, to: next.to < firstDay ? firstDay : next.to };
    if (pan.end) next = { from: next.from > lastDay ? lastDay : next.from, to: lastDay };
    live.current.current = next;
    panRef.current = null;
    if (pan.commit) commit(next);
    else setDraft(next);
  }, [firstDay, lastDay]);

  /** Moves the window STEP_DAYS; a knob on the edge being moved toward is pulled along once
   *  the moved window renders. False once the window is already at that end of the history. */
  const step = (dir: -1 | 1): boolean => {
    const now = live.current;
    if (!(dir < 0 ? now.canPanEarlier : now.canPanLater)) return false;
    panRef.current = {
      // `||` with a pan still in flight: its window hasn't rendered, so this one's edges are stale.
      start: panRef.current?.start || (dir < 0 && now.startOf(now.current.from) === 0),
      end: panRef.current?.end || (dir > 0 && now.endOf(now.current.to) === now.n),
      commit: false,
    };
    now.onPan(dir * STEP_DAYS);
    return true;
  };

  /** Moves the selection its own length; false when it can't move that way (yet). */
  const shiftStep = (dir: -1 | 1): boolean => {
    const now = live.current;
    const next = now.onShift(now.current, dir);
    if (!next) return false;
    now.current = next;
    setDraft(next);
    return true;
  };

  /** Ends a gesture: commits now, or once a pan still in flight has landed. */
  const finish = () => {
    if (panRef.current) panRef.current.commit = true;
    else commit(live.current.current);
  };

  const stopRepeat = () => {
    const repeat = repeatRef.current;
    if (!repeat) return;
    window.clearTimeout(repeat.timer);
    repeatRef.current = null;
    finish();
  };

  /** Press-and-hold on any of the four buttons: `action` once now, then repeating. */
  const startRepeat = (action: () => boolean, event: ReactPointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    action();
    const tick = () => {
      // Reaching the end disables the button, and a disabled button gets no pointerup — so
      // the hold ends (and commits) here instead.
      if (!action()) return stopRepeat();
      if (repeatRef.current) repeatRef.current.timer = window.setTimeout(tick, REPEAT_INTERVAL_MS);
    };
    repeatRef.current = { timer: window.setTimeout(tick, REPEAT_DELAY_MS) };
  };

  useEffect(() => () => window.clearTimeout(repeatRef.current?.timer), []);

  const pageButton = (dir: -1 | 1) => (
    <button
      type="button"
      className="date-range-slider__page"
      data-testid={dir < 0 ? 'date-range-slider-earlier' : 'date-range-slider-later'}
      aria-label={dir < 0 ? tn('slider.days_earlier', STEP_DAYS) : tn('slider.days_later', STEP_DAYS)}
      title={dir < 0 ? t('slider.earlier') : t('slider.later')}
      disabled={dir < 0 ? !canPanEarlier : !canPanLater}
      onPointerDown={(event) => startRepeat(() => step(dir), event)}
      onPointerUp={stopRepeat}
      onPointerCancel={stopRepeat}
      // Pointer presses are handled above; this is the keyboard's Enter/Space (detail 0).
      onClick={(event: ReactMouseEvent) => {
        if (event.detail !== 0) return;
        step(dir);
        finish();
      }}
    >
      {dir < 0 ? <ChevronsLeft size={16} /> : <ChevronsRight size={16} />}
    </button>
  );

  const shiftButton = (dir: -1 | 1) => (
    <button
      type="button"
      className="date-range-slider__page date-range-slider__page--shift"
      data-testid={dir < 0 ? 'date-range-slider-shift-earlier' : 'date-range-slider-shift-later'}
      aria-label={dir < 0 ? t('slider.shift_earlier') : t('slider.shift_later')}
      title={dir < 0 ? t('slider.shift_earlier') : t('slider.shift_later')}
      disabled={!canShift(current, dir)}
      onPointerDown={(event) => startRepeat(() => shiftStep(dir), event)}
      onPointerUp={stopRepeat}
      onPointerCancel={stopRepeat}
      onClick={(event: ReactMouseEvent) => {
        if (event.detail !== 0) return;
        shiftStep(dir);
        finish();
      }}
    >
      {dir < 0 ? <ChevronLeft size={16} /> : <ChevronRight size={16} />}
    </button>
  );

  // ---- The track ------------------------------------------------------------------------

  /** The slot boundary nearest a clientX. */
  const boundaryAt = (clientX: number) => {
    const rect = trackRef.current!.getBoundingClientRect();
    return clamp(Math.round(((clientX - rect.left) / rect.width) * n));
  };

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!trackRef.current || n === 0) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    const b = boundaryAt(event.clientX);
    // An off-screen knob counts as sitting on the edge it went past.
    const knob: Knob = Math.abs(b - clamp(start)) <= Math.abs(b - clamp(end)) ? 'start' : 'end';
    dragRef.current = { pointerId: event.pointerId, knob };
    setDraft(moveKnob(knob, b, current));
  };

  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId || !draft) return;
    setDraft(moveKnob(drag.knob, boundaryAt(event.clientX), draft));
  };

  const onPointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (dragRef.current?.pointerId !== event.pointerId) return;
    dragRef.current = null;
    commit(draft);
  };

  const onKnobKeyDown = (knob: Knob, event: KeyboardEvent) => {
    const at = knob === 'start' ? start : end;
    const delta = { ArrowLeft: -1, ArrowDown: -1, ArrowRight: 1, ArrowUp: 1, PageDown: -STEP_DAYS, PageUp: STEP_DAYS }[event.key];
    const target = event.key === 'Home' ? 0 : event.key === 'End' ? n : delta !== undefined ? at + delta : null;
    if (target === null) return;
    event.preventDefault();
    setDraft(moveKnob(knob, target, current));
  };

  const knob = (which: Knob) => {
    const at = which === 'start' ? start : end;
    return (
      n > 0 &&
      at >= 0 &&
      at <= n && (
        <span
          className="date-range-slider__knob"
          role="slider"
          tabIndex={0}
          aria-label={which === 'start' ? t('slider.start') : t('slider.end')}
          aria-valuemin={0}
          aria-valuemax={n}
          aria-valuenow={at}
          aria-valuetext={formatDayLabel(which === 'start' ? current.from : current.to)}
          data-testid={`date-range-slider-${which}`}
          style={{ left: `${percent(at)}%` }}
          onKeyDown={(event) => onKnobKeyDown(which, event)}
          onKeyUp={() => commit(draft)}
          onBlur={() => commit(draft)}
        />
      )
    );
  };

  return (
    <div className="date-range-slider" data-testid="date-range-slider">
      <div className="date-range-slider__row">
        {pageButton(-1)}
        {shiftButton(-1)}
        <div
          ref={trackRef}
          className="date-range-slider__track"
          data-testid="date-range-slider-track"
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={onPointerUp}
        >
          <div className="activity-filters__track" />
          {ticksOf(days).map((tick, b) => (
            <span
              key={b}
              className={`date-range-slider__tick date-range-slider__tick--${tick}${
                b >= clamp(start) && b <= clamp(end) ? ' date-range-slider__tick--selected' : ''
              }`}
              style={{ left: `${percent(b)}%` }}
            />
          ))}
          {n > 0 && clamp(end) > clamp(start) && (
            <div
              className="activity-filters__fill"
              style={{ left: `${percent(start)}%`, right: `${100 - percent(end)}%` }}
            />
          )}
          {knob('start')}
          {knob('end')}
        </div>
        {shiftButton(1)}
        {pageButton(1)}
      </div>
      <div className="date-range-slider__labels" data-testid="date-range-slider-labels">
        <span>{formatDayLabel(current.from)}</span>
        <span>{formatDayLabel(current.to)}</span>
      </div>
    </div>
  );
}
