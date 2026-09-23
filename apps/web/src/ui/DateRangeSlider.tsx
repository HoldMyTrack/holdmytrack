import { useEffect, useRef, useState } from 'react';
import type { KeyboardEvent, MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent } from 'react';
import { ChevronIcon } from './ChevronIcon';
import { addDays, dayDiff } from './dateMath';
import { formatDayLabel } from './format';
import type { DateRange } from './RangePicker';

export interface DateRangeSliderProps {
  /** The whole scale, as YYYY-MM-DD: the first activity day and today. */
  first: string;
  last: string;
  value: DateRange;
  onChange: (next: DateRange) => void;
}

type Knob = 'start' | 'end';
/** Day *boundaries* counted from the start of `first`: `start` is the first selected day's
 *  own start, `end` the last selected day's own end — so `end - start` is the number of
 *  selected days, never less than 1. */
interface Span {
  start: number;
  end: number;
}

/** How many days the track shows edge to edge — ~18px a day on a phone, wide enough for two
 *  knobs one day apart to sit clearly side by side. */
const WINDOW_DAYS = 15;
/** How far one Earlier/Later tap moves the window. */
const STEP_DAYS = 5;

/** Press-and-hold on Earlier/Later: the first step is immediate, then it repeats. */
const REPEAT_DELAY_MS = 400;
const REPEAT_INTERVAL_MS = 180;

/**
 * The phone footer's date-range control, in place of RangePicker.tsx's bar chart: a plain
 * two-knob slider over calendar days, with the selected dates under it — the look of the
 * Activities panel's DistanceFilter.tsx (its `.activity-filters__track`/`__fill` rules) with
 * finger-height knobs. The bar chart's bars, handles and month rail are far too small for a
 * fingertip on a phone-width strip, so a phone gets this instead.
 *
 * **Knobs sit on day boundaries, not on days.** The start knob marks where the first selected
 * day begins and the end knob where the last one ends, so a one-day selection has its knobs
 * one day apart and the two can never be dragged closer than that — there's no zero-width
 * selection to reach, and the knobs never sit on top of each other.
 *
 * **A 15-day window, not the whole history.** Stretching months across ~270px leaves a few
 * pixels per day, too few to pick a single day. The track shows `WINDOW_DAYS` edge to edge;
 * Earlier/Later beside it move that window `STEP_DAYS` per tap (repeating while held). A knob
 * sitting on the edge the window moves toward is pulled along with it, which is how a
 * selection grows past the window: park the start knob on the left edge and tap Earlier. The
 * knob on the side being moved toward therefore never scrolls out of view; only the far one
 * can, and its date stays in the label under the track. Calendar days rather than
 * days-with-activity — with no bars there's nothing to pack, and a straight time scale is what
 * a plain slider reads as.
 *
 * Hand-rolled pointer handling on the whole track rather than two stacked native range inputs
 * (which always hand a drag to whichever input is on top): a press moves whichever knob is
 * nearer — jumping it to the pressed boundary if the press is on bare track — and a drag
 * carries it. Each knob is also a focusable `role="slider"`.
 *
 * Knob drags and held buttons render from a local draft and commit only on release, so the
 * activity list isn't refetched for every day passed — the same reason RangePicker commits
 * only on release.
 */
export function DateRangeSlider({ first, last, value, onChange }: DateRangeSliderProps) {
  const trackRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ pointerId: number; knob: Knob } | null>(null);
  const repeatRef = useRef<{ timer: number } | null>(null);

  /** The end boundary of `last` — the scale runs 0..limit. */
  const limit = Math.max(1, dayDiff(first, last) + 1);
  const span = Math.min(WINDOW_DAYS, limit);
  const committedStart = Math.min(Math.max(dayDiff(first, value.from), 0), limit - 1);
  const committed: Span = {
    start: committedStart,
    end: Math.min(Math.max(dayDiff(first, value.to) + 1, committedStart + 1), limit),
  };

  const [draft, setDraft] = useState<Span | null>(null);
  const current = draft ?? committed;

  // The window's right edge, as a date (the day just after it) so it survives `first`
  // moving underneath it as history loads. Starts with the selection's end on the right edge.
  const [windowEndDate, setWindowEndDate] = useState(() => addDays(first, committed.end));
  const windowEnd = Math.min(Math.max(dayDiff(first, windowEndDate), span), limit);
  const windowStart = windowEnd - span;

  // A selection set from elsewhere (a map click selecting one day, the default resolving)
  // that lands wholly outside the window brings the window to it. Keyed on the committed
  // selection only, so the window moving on its own never snaps back.
  useEffect(() => {
    if (committed.end <= windowStart || committed.start >= windowEnd) setWindowEndDate(addDays(first, committed.end));
  }, [committed.start, committed.end]);

  // Latest values for the press-and-hold timer, which outlives the render that started it.
  const live = useRef({ current, windowEnd, windowStart });
  live.current = { current, windowEnd, windowStart };

  const commit = (next: Span | null) => {
    setDraft(null);
    if (!next || (next.start === committed.start && next.end === committed.end)) return;
    onChange({ from: addDays(first, next.start), to: addDays(first, next.end - 1) });
  };

  // ---- Earlier / Later ------------------------------------------------------------------

  /** Moves the window up to STEP_DAYS; a knob on the edge being moved toward is pulled
   *  along. False once the window is already at that end of the history. */
  const step = (dir: -1 | 1): boolean => {
    const { current: sel, windowEnd: end, windowStart: start } = live.current;
    const nextEnd = Math.min(Math.max(end + dir * STEP_DAYS, span), limit);
    if (nextEnd === end) return false;
    const nextStart = nextEnd - span;
    const next = { ...sel };
    if (dir < 0 && sel.start === start) next.start = nextStart;
    if (dir > 0 && sel.end === end) next.end = nextEnd;
    live.current = { current: next, windowEnd: nextEnd, windowStart: nextStart };
    setWindowEndDate(addDays(first, nextEnd));
    if (next.start !== sel.start || next.end !== sel.end) setDraft(next);
    return true;
  };

  const stopRepeat = () => {
    const repeat = repeatRef.current;
    if (!repeat) return;
    window.clearTimeout(repeat.timer);
    repeatRef.current = null;
    commit(live.current.current);
  };

  const startRepeat = (dir: -1 | 1, event: ReactPointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    step(dir);
    const tick = () => {
      // Reaching the end disables the button, and a disabled button gets no pointerup — so
      // the hold ends (and commits) here instead.
      if (!step(dir)) return stopRepeat();
      if (repeatRef.current) repeatRef.current.timer = window.setTimeout(tick, REPEAT_INTERVAL_MS);
    };
    repeatRef.current = { timer: window.setTimeout(tick, REPEAT_DELAY_MS) };
  };

  useEffect(() => () => window.clearTimeout(repeatRef.current?.timer), []);

  const pageButton = (dir: -1 | 1) => (
    <button
      type="button"
      className="range-picker__page date-range-slider__page"
      data-testid={dir < 0 ? 'date-range-slider-earlier' : 'date-range-slider-later'}
      aria-label={dir < 0 ? `${STEP_DAYS} days earlier` : `${STEP_DAYS} days later`}
      title={dir < 0 ? 'Earlier' : 'Later'}
      disabled={dir < 0 ? windowStart === 0 : windowEnd === limit}
      onPointerDown={(event) => startRepeat(dir, event)}
      onPointerUp={stopRepeat}
      onPointerCancel={stopRepeat}
      // Pointer presses are handled above; this is the keyboard's Enter/Space (detail 0).
      onClick={(event: ReactMouseEvent) => {
        if (event.detail !== 0) return;
        step(dir);
        commit(live.current.current);
      }}
    >
      <ChevronIcon direction={dir < 0 ? 'left' : 'right'} />
    </button>
  );

  // ---- The track ------------------------------------------------------------------------

  const inWindow = (b: number) => b >= windowStart && b <= windowEnd;
  const toWindow = (b: number) => Math.min(Math.max(b, windowStart), windowEnd);
  const percent = (b: number) => ((toWindow(b) - windowStart) / span) * 100;

  /** Moves one knob to boundary `b`, keeping it inside the window and a day from the other. */
  const moveKnob = (knob: Knob, b: number, from: Span): Span =>
    knob === 'start'
      ? { start: Math.min(Math.max(b, windowStart), from.end - 1), end: from.end }
      : { start: from.start, end: Math.max(Math.min(b, windowEnd), from.start + 1) };

  /** The day boundary nearest a clientX. */
  const boundaryAt = (clientX: number) => {
    const rect = trackRef.current!.getBoundingClientRect();
    return toWindow(Math.round(windowStart + ((clientX - rect.left) / rect.width) * span));
  };

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!trackRef.current) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    const b = boundaryAt(event.clientX);
    // An off-screen knob counts as sitting on the edge it went past.
    const knob: Knob = Math.abs(b - toWindow(current.start)) <= Math.abs(b - toWindow(current.end)) ? 'start' : 'end';
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
    const at = current[knob];
    const delta = { ArrowLeft: -1, ArrowDown: -1, ArrowRight: 1, ArrowUp: 1, PageDown: -STEP_DAYS, PageUp: STEP_DAYS }[event.key];
    const target = event.key === 'Home' ? windowStart : event.key === 'End' ? windowEnd : delta !== undefined ? at + delta : null;
    if (target === null) return;
    event.preventDefault();
    setDraft(moveKnob(knob, target, current));
  };

  const dayOf = (knob: Knob, sel: Span) => addDays(first, knob === 'start' ? sel.start : sel.end - 1);

  const knob = (which: Knob) =>
    inWindow(current[which]) && (
      <span
        className="date-range-slider__knob"
        role="slider"
        tabIndex={0}
        aria-label={which === 'start' ? 'Start date' : 'End date'}
        aria-valuemin={windowStart}
        aria-valuemax={windowEnd}
        aria-valuenow={current[which]}
        aria-valuetext={formatDayLabel(dayOf(which, current))}
        data-testid={`date-range-slider-${which}`}
        style={{ left: `${percent(current[which])}%` }}
        onKeyDown={(event) => onKnobKeyDown(which, event)}
        onKeyUp={() => commit(draft)}
        onBlur={() => commit(draft)}
      />
    );

  return (
    <div className="date-range-slider" data-testid="date-range-slider">
      <div className="date-range-slider__row">
        {pageButton(-1)}
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
          {current.end > windowStart && current.start < windowEnd && (
            <div
              className="activity-filters__fill"
              style={{ left: `${percent(current.start)}%`, right: `${100 - percent(current.end)}%` }}
            />
          )}
          {knob('start')}
          {knob('end')}
        </div>
        {pageButton(1)}
      </div>
      <div className="date-range-slider__labels" data-testid="date-range-slider-labels">
        <span>{formatDayLabel(dayOf('start', current))}</span>
        <span>{formatDayLabel(dayOf('end', current))}</span>
      </div>
    </div>
  );
}
