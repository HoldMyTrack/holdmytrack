import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { HistogramBucket } from '../api';
import { formatDistance } from './format';
import { useUnitSystem, type UnitSystem } from './units';

/**
 * The bottom timeline's interactive range picker.
 *
 * **One bar per day the user actually recorded, packed.** Days with no activity get no slot
 * at all — two bars sit side by side whether their real dates are a day or a year apart. So
 * this is not a proportional calendar axis any more, and deliberately so: the chart answers
 * "what did I do, and in what order" rather than "how was it spread across the year", and on
 * a real history the proportional version spent most of its width drawing nothing.
 *
 * Everything here is therefore measured in **bars, not days**. A drag converts pixels to a
 * whole number of slots and works in slot indices; the only place real dates come back is
 * when a selection is committed, since `selectedRange` is a date filter that the activity
 * list and summary endpoints have to understand.
 *
 * Two independent things, easy to conflate: which bars are on screen (`days` — the pan
 * position, owned by useActivityDays) and which range is selected (`selectedRange` — the
 * list/summary filter, owned by MapView, drawn here as the highlighted band). Panning never
 * writes to the selection: paging back to a user's very first activity leaves the selection
 * exactly as it was, and if it scrolls off screen the band simply isn't drawn until it comes
 * back. A selection reaching past one edge of the view draws clipped to that edge.
 *
 * Dragging a handle renders from local state rather than from the `selectedRange` prop, so a
 * resize doesn't refetch the activity list on every pointermove — only on release. Panning
 * needs no such treatment now: it moves a window over data that's already loaded, and only
 * crossing into unloaded history costs a request (useActivityDays prefetches before that).
 *
 * **A plain click on a bar outside the current band selects just that one day.** Bars have
 * no click handler of their own — the chart's own pointerdown already has to decide pan vs.
 * slide by hit-testing the band, so a click is detected the same way, in `endDrag`: a
 * pointerdown/up pair that started as a pan (i.e., missed the band) but never actually panned
 * anywhere. See `CLICK_MOVE_THRESHOLD_PX` for why both a pixel-distance check and
 * `drag.panned === 0` are required, not just one.
 */
export interface DateRange {
  from: string;
  to: string;
}

export interface RangePickerProps {
  /** The bars to draw: consecutive days-with-activity, ascending, already windowed. */
  days: HistogramBucket[];
  /** Move the view by whole bars — negative is toward the past. Also driven by the
   *  Earlier/Later buttons, which live in ActivityHistogram.tsx's header row now, not here —
   *  this component only needs `onPan` for its own drag-the-strip gesture. */
  onPan: (deltaBars: number) => void;
  selectedRange: DateRange;
  onChangeSelection: (next: DateRange) => void;
  /** How many bars the chart's current rendered width actually fits — reported on every
   *  measurement (mount, and every resize), so useActivityDays.ts can show that many instead
   *  of a fixed count. Reported live as a fixed count leaving the strip visibly short of a
   *  wide monitor's full width, packed against the right edge with empty space on the left. */
  onCapacityChange: (barsPerView: number) => void;
}

/** Must match `.range-picker__slot`'s flex-basis and `.range-picker__bars`' `gap` in
 *  index.css — used only to turn a measured pixel width into a bar count for
 *  `onCapacityChange`, never for layout itself (the CSS is what actually draws it). */
const SLOT_WIDTH_PX = 10;
const SLOT_GAP_PX = 1;

/** Never reports a capacity below this, however narrow (or momentarily zero, e.g. mid-layout)
 *  the measurement — a real but tiny number would still work, this just avoids a jarring
 *  flash down to one or two bars between measurements. */
const MIN_REPORTED_CAPACITY = 5;

/** Percent of the chart's width two month labels must be apart to both be drawn. Bars are
 *  packed by activity-day, so a sparse stretch can put twelve months into twelve adjacent
 *  slots; without this they would overprint each other into a smear. */
const MIN_TICK_GAP_PERCENT = 9;

/** Shortest bar drawn for a day that has activity, as a percent of the chart height — a day
 *  with a 300 m walk on it still has to be visible and clickable. */
const MIN_BAR_PERCENT = 4;

/** Must match .range-picker__selection's `bottom` and .range-picker__months' `height` in
 *  index.css — the month rail's own pixel height, excluded from the band's slide hit-test
 *  below so it stays a pan target even when a selection spans the whole strip. */
const MONTH_RAIL_HEIGHT_PX = 20;

/** A pointerdown that ends within this many pixels of where it started, over the bars (not
 *  the month rail) and outside the current band, counts as a click on that bar rather than a
 *  pan that just didn't get anywhere — selecting that one day instead. Paired with requiring
 *  `drag.panned === 0` below (see endDrag) so a real, if small, pan that already moved the
 *  view by a bar or more never also fires a day selection underneath it. */
const CLICK_MOVE_THRESHOLD_PX = 4;

/**
 * Log-scaled rather than linear against the busiest day on screen: a single 10-activity
 * outlier day would otherwise flatten every ordinary 1-activity day on the same strip down
 * to MIN_BAR_PERCENT, since a linear scale makes every bar's height *proportional* to the
 * peak rather than merely *ordered* by it. `+1` inside both logs keeps a 0 m day (an
 * activity with no recorded distance) from producing log(0), and keeps the ratio meaningful
 * when the peak itself is small.
 */
function barHeightPercent(day: HistogramBucket, peak: number): number {
  if (peak <= 0) return MIN_BAR_PERCENT;
  const scaled = Math.log(day.distanceMeters + 1) / Math.log(peak + 1);
  return Math.max(MIN_BAR_PERCENT, scaled * 100);
}

/**
 * Which slots the selection covers, clipped to what's on screen, or null when it covers none
 * of them — either it's entirely off one side, or it falls in a gap between two activity-days
 * that are adjacent bars here. Nothing to draw in that case; the footer's own range label is
 * still showing what's selected.
 */
function bandSlots(days: HistogramBucket[], range: DateRange): { start: number; end: number } | null {
  if (days.length === 0) return null;
  const start = days.findIndex((day) => day.date >= range.from);
  if (start === -1) return null;
  let end = -1;
  for (let i = days.length - 1; i >= start; i -= 1) {
    if (days[i]!.date <= range.to) {
      end = i;
      break;
    }
  }
  return end < start ? null : { start, end };
}

/**
 * A month label under the first bar of each month, at that bar's real position — not spread
 * evenly across the axis, which with packed bars would claim an even spacing through time
 * that isn't there. Labels closer together than MIN_TICK_GAP_PERCENT are dropped rather than
 * drawn on top of one another, and the year is spelled out whenever it changes.
 *
 * `blankFraction`/`packedFraction` fold in the same right-alignment RangePicker's own
 * measured `packedFraction` applies to the selection band — a tick's position is still
 * `(index + 0.5) / days.length` *of the packed group*, just offset by however much blank
 * space sits to its left when there are too few days to fill the chart.
 */
function monthTicks(
  days: HistogramBucket[],
  blankFraction: number,
  packedFraction: number,
): { key: string; label: string; left: number }[] {
  const ticks: { key: string; label: string; left: number }[] = [];
  let lastLeft = -Infinity;
  let lastYear = '';
  days.forEach((day, index) => {
    if (index > 0 && days[index - 1]!.date.slice(0, 7) === day.date.slice(0, 7)) return;
    const left = (blankFraction + ((index + 0.5) / days.length) * packedFraction) * 100;
    if (left - lastLeft < MIN_TICK_GAP_PERCENT) return;
    const year = day.date.slice(0, 4);
    const month = new Date(`${day.date}T00:00:00Z`)
      .toLocaleDateString(undefined, { month: 'short', timeZone: 'UTC' })
      .toUpperCase();
    ticks.push({ key: day.date, label: year === lastYear ? month : `${month} ${year}`, left });
    lastLeft = left;
    lastYear = year;
  });
  return ticks;
}

function barTitle(day: HistogramBucket, system: UnitSystem): string {
  const activities = `${day.count} ${day.count === 1 ? 'activity' : 'activities'}`;
  return `${day.date}: ${activities}, ${formatDistance(day.distanceMeters, system)}`;
}

type DragMode = 'handle-start' | 'handle-end' | 'slide' | 'pan';

interface DragState {
  mode: DragMode;
  pointerId: number;
  startX: number;
  /** Slot indices the band occupied when the drag began. */
  slots: { start: number; end: number };
  /** The selection's real dates when the drag began — see commitSelection for why an edge
   *  the user never touched keeps its original date rather than the visible bar's. */
  range: DateRange;
  /** Bars already handed to onPan this drag, so each move sends only the difference. */
  panned: number;
}

export function RangePicker({ days, onPan, selectedRange, onChangeSelection, onCapacityChange }: RangePickerProps) {
  const chartRef = useRef<HTMLDivElement>(null);
  const barsRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<DragState | null>(null);
  const [draftSlots, setDraftSlots] = useState<{ start: number; end: number } | null>(null);
  const system = useUnitSystem();

  // What fraction of the chart's own width the packed bars actually render at — 1 once
  // there are enough days to fill it (today's behavior, unchanged), less than 1 for a short
  // history now that .range-picker__slot no longer grows to fill leftover space. Measured
  // from the real DOM (barsRef's scrollWidth survives .range-picker__bars' own
  // flex-shrink compression the same way it survives the CSS-only right-alignment trick, so
  // one number covers both "few days" and "too many days to fit" without separate logic) —
  // defaults to 1 (today's full-width assumption) for the one frame before the first
  // measurement lands, so there's nothing to flicker.
  const [packedFraction, setPackedFraction] = useState(1);
  const blankFraction = 1 - packedFraction;

  const measurePackedFraction = useCallback(() => {
    const chartEl = chartRef.current;
    const barsEl = barsRef.current;
    if (!chartEl || !barsEl) return;
    const chartWidth = chartEl.getBoundingClientRect().width;
    // Capacity comes from the chart's own full width divided by one slot's fixed footprint —
    // deliberately not from how many bars are actually rendered right now (below), since the
    // whole point is answering "how many *could* fit" even when fewer than that are loaded or
    // on screen.
    onCapacityChange(Math.max(MIN_REPORTED_CAPACITY, Math.floor((chartWidth + SLOT_GAP_PX) / (SLOT_WIDTH_PX + SLOT_GAP_PX))));
    // .range-picker__bars' own box still spans the chart's full width (it's `inset: 4px 0
    // 20px`, i.e. left:0/right:0) — only its flex *content* packs to the right now, so its
    // own scrollWidth/getBoundingClientRect can't tell "few days" from "enough to fill."
    // Measuring the first and last slot's actual rendered positions instead reads the real
    // packed span directly, independent of the (still full-width) box around them.
    const firstSlot = barsEl.firstElementChild as HTMLElement | null;
    const lastSlot = barsEl.lastElementChild as HTMLElement | null;
    if (!firstSlot || !lastSlot || chartWidth <= 0) {
      setPackedFraction(1);
      return;
    }
    const packedWidth = lastSlot.getBoundingClientRect().right - firstSlot.getBoundingClientRect().left;
    setPackedFraction(Math.min(1, Math.max(0, packedWidth / chartWidth)));
  }, [onCapacityChange]);

  // Re-measure whenever the bars themselves could have changed width: a new `days` window
  // (a different count, e.g. paging to the edge of a short history) via this layout effect,
  // and a resize of the chart itself (window resize, or the Activities panel's own drag
  // handle changing how much width the map — and this footer — actually has) via the
  // observer below. Neither alone covers both cases: `days` can stay the same length across
  // a resize, and the chart can stay the same size across a pan that changes how many days
  // are in view.
  useLayoutEffect(() => {
    measurePackedFraction();
  }, [days, measurePackedFraction]);

  useLayoutEffect(() => {
    const chartEl = chartRef.current;
    if (!chartEl) return;
    const observer = new ResizeObserver(measurePackedFraction);
    observer.observe(chartEl);
    return () => observer.disconnect();
  }, [measurePackedFraction]);

  // Scaled against the busiest day on screen, not an absolute distance: the chart is about
  // the shape of a history, and a fixed scale would flatten a walker's year to nothing.
  const peak = useMemo(() => days.reduce((max, day) => Math.max(max, day.distanceMeters), 0), [days]);
  const ticks = useMemo(() => monthTicks(days, blankFraction, packedFraction), [days, blankFraction, packedFraction]);
  const committedBand = useMemo(() => bandSlots(days, selectedRange), [days, selectedRange]);
  const band = draftSlots ?? committedBand;

  /**
   * Resolve dragged slot indices back to dates. An edge the drag didn't move keeps the date
   * it already had: the band is clipped to the view, so an all-time selection viewed from the
   * middle of a history has both edges sitting on visible bars, and reading the date off the
   * bar would silently narrow the untouched edge to whatever happens to be on screen.
   */
  const commitSelection = useCallback(
    (drag: DragState, slots: { start: number; end: number }) => {
      const from = slots.start === drag.slots.start ? drag.range.from : days[slots.start]!.date;
      const to = slots.end === drag.slots.end ? drag.range.to : days[slots.end]!.date;
      onChangeSelection({ from, to });
    },
    [days, onChangeSelection],
  );

  const beginDrag = useCallback(
    (mode: DragMode, event: React.PointerEvent) => {
      event.stopPropagation();
      // Without this, a pan drag that so much as crosses a month label's text (the bars
      // themselves aren't text, but the drag path easily wanders near .range-picker__months)
      // lets Firefox start its default "select this content" gesture. A second drag started
      // anywhere afterwards — a handle included — then reads as "drag the selection" instead
      // of a plain pointer gesture: Firefox shows a native ghost-image drag of the whole
      // component and, crucially, stops delivering pointermove for it, so the handle looks
      // like it silently does nothing. Reported live as exactly that: pan, then a handle drag
      // that "doesn't work" and instead "moves like it is an image." Chromium doesn't trigger
      // this readily, which is why it didn't show up testing there.
      event.preventDefault();
      const el = chartRef.current;
      if (!el || days.length === 0) return;
      // A handle or the band's middle can only be grabbed when a band is drawn, so its slots
      // are known; a pan doesn't use them and gets the whole strip as a harmless stand-in.
      const slots = committedBand ?? { start: 0, end: days.length - 1 };
      el.setPointerCapture(event.pointerId);
      dragRef.current = { mode, pointerId: event.pointerId, startX: event.clientX, slots, range: selectedRange, panned: 0 };
      if (mode !== 'pan') setDraftSlots(slots);
    },
    [committedBand, days, selectedRange],
  );

  const onPointerMove = useCallback(
    (event: React.PointerEvent) => {
      const drag = dragRef.current;
      const el = chartRef.current;
      if (!drag || !el || event.pointerId !== drag.pointerId) return;
      const width = el.getBoundingClientRect().width;
      // The packed group's own pixel width, not the chart's full width — a short history's
      // bars only occupy packedFraction of the chart (the rest is blank space, left of
      // them), so dividing by the full width would under-count how far one slot actually
      // spans on screen and make a drag feel slower than the bars it's tracking.
      const slotWidth = (width * packedFraction) / days.length;
      if (slotWidth <= 0 || days.length === 0) return;
      // Pixels to whole slots. Every slot is the same width by construction (they are flex
      // siblings), which is what makes this exact rather than the old pixels-per-calendar-day
      // approximation — there is no longer any relationship between width and elapsed time.
      const deltaSlots = Math.round((event.clientX - drag.startX) / slotWidth);
      const last = days.length - 1;

      if (drag.mode === 'pan') {
        // Dragging the strip rightwards pulls earlier bars into view, hence the negation.
        onPan(-(deltaSlots - drag.panned));
        drag.panned = deltaSlots;
        return;
      }
      if (drag.mode === 'handle-start') {
        setDraftSlots({ start: Math.min(Math.max(drag.slots.start + deltaSlots, 0), drag.slots.end), end: drag.slots.end });
      } else if (drag.mode === 'handle-end') {
        setDraftSlots({ start: drag.slots.start, end: Math.min(Math.max(drag.slots.end + deltaSlots, drag.slots.start), last) });
      } else {
        const span = drag.slots.end - drag.slots.start;
        const start = Math.min(Math.max(drag.slots.start + deltaSlots, 0), last - span);
        setDraftSlots({ start, end: start + span });
      }
    },
    [days, onPan, packedFraction],
  );

  const endDrag = useCallback(
    (event: React.PointerEvent) => {
      const drag = dragRef.current;
      if (!drag || event.pointerId !== drag.pointerId) return;
      if (drag.mode !== 'pan' && draftSlots) {
        commitSelection(drag, draftSlots);
      } else if (drag.mode === 'pan' && drag.panned === 0 && days.length > 0) {
        // A pointerdown outside the band that never actually panned anywhere — treat it as a
        // click on whichever bar is underneath and select just that one day, rather than
        // leaving it as a pan gesture that did nothing.
        const el = chartRef.current;
        const moved = Math.abs(event.clientX - drag.startX);
        if (el && moved <= CLICK_MOVE_THRESHOLD_PX) {
          const rect = el.getBoundingClientRect();
          const aboveMonthRail = event.clientY < rect.bottom - MONTH_RAIL_HEIGHT_PX;
          // The packed bars start blankFraction of the way across the chart, not at its own
          // left edge — a click left of that (the blank space a short history leaves) has no
          // bar under it at all, so it's excluded below rather than clamped to day 0.
          const packedLeft = rect.left + blankFraction * rect.width;
          const packedWidth = packedFraction * rect.width;
          if (aboveMonthRail && packedWidth > 0 && event.clientX >= packedLeft) {
            const index = Math.min(
              Math.max(Math.floor(((event.clientX - packedLeft) / packedWidth) * days.length), 0),
              days.length - 1,
            );
            const date = days[index]!.date;
            onChangeSelection({ from: date, to: date });
          }
        }
      }
      dragRef.current = null;
      setDraftSlots(null);
    },
    [commitSelection, draftSlots, days, onChangeSelection, blankFraction, packedFraction],
  );

  /**
   * Decides pan vs. slide by comparing the pointerdown's position to the band's own pixel
   * rectangle, in JS — rather than making the band itself `pointer-events: auto` and letting
   * CSS hit-testing decide. That earlier approach made the whole band swallow hover for every
   * bar underneath it (title tooltips included) for as long as it was selected, which is a
   * bigger loss than it looks: a selection commonly covers most of the strip, so most bars
   * would never show their own tooltip again. Keeping the band `pointer-events: none` and
   * doing the hit-test here gets "drag the band to slide" without that cost — hover reaches
   * every bar regardless of whether it's currently selected. The Y check excludes the month
   * rail specifically, since that's the one strip a selection band must never claim as a
   * slide target — it's what stays grabbable for panning when a selection covers everything
   * on screen.
   */
  const onChartPointerDown = useCallback(
    (event: React.PointerEvent) => {
      const el = chartRef.current;
      if (el && band) {
        const rect = el.getBoundingClientRect();
        const packedLeft = rect.left + blankFraction * rect.width;
        const packedWidth = packedFraction * rect.width;
        const bandLeft = packedLeft + (band.start / days.length) * packedWidth;
        const bandRight = packedLeft + ((band.end + 1) / days.length) * packedWidth;
        const aboveMonthRail = event.clientY < rect.bottom - MONTH_RAIL_HEIGHT_PX;
        if (aboveMonthRail && event.clientX >= bandLeft && event.clientX <= bandRight) {
          beginDrag('slide', event);
          return;
        }
      }
      beginDrag('pan', event);
    },
    [band, days.length, beginDrag, blankFraction, packedFraction],
  );

  return (
    <div
      className="range-picker__chart"
      ref={chartRef}
      data-testid="range-picker"
      role="group"
      aria-label="Activity days — drag to pan, drag the highlighted band to change the selected range"
      onPointerDown={onChartPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
    >
      {band && (
        // pointer-events: none (index.css) — onChartPointerDown above does the slide
        // hit-test in JS instead of letting this div capture it, specifically so it never
        // steals hover from a bar underneath (a selection commonly covers most of the
        // strip, so most bars would otherwise never show their own tooltip).
        <div
          className="range-picker__selection"
          data-testid="range-picker-selection"
          style={{
            left: `${(blankFraction + (band.start / days.length) * packedFraction) * 100}%`,
            width: `${((band.end - band.start + 1) / days.length) * packedFraction * 100}%`,
          }}
        >
          <span
            className="range-picker__handle range-picker__handle--start"
            onPointerDown={(event) => beginDrag('handle-start', event)}
          />
          <span
            className="range-picker__handle range-picker__handle--end"
            onPointerDown={(event) => beginDrag('handle-end', event)}
          />
        </div>
      )}

      <div className="range-picker__bars" data-testid="histogram-bars" ref={barsRef}>
        {days.map((day, index) => (
          // The slot is what carries the layout — one equal flex share per activity-day, so
          // the band's percentages above line up with the bars exactly. The bar inside it
          // is capped in width so a history of six days doesn't render six fat slabs.
          <span key={day.date} className="range-picker__slot">
            <span
              // Selected bars are drawn in the accent colour as well as sitting under the
              // band's tint, per main-screen-v6.png — the tint alone is faint enough that
              // the band's edges do most of the work of saying what's in the selection.
              className={
                band && index >= band.start && index <= band.end
                  ? 'range-picker__bar range-picker__bar--selected'
                  : 'range-picker__bar'
              }
              style={{ height: `${barHeightPercent(day, peak)}%` }}
              title={barTitle(day, system)}
            />
          </span>
        ))}
      </div>

      {/* The month rail doubles as the pan grip: it is the one part of the chart a
          selection band never covers, so there is always somewhere to grab. */}
      <div className="range-picker__months" data-testid="range-picker-rail">
        {ticks.map((tick) => (
          <span key={tick.key} className="range-picker__month" style={{ left: `${tick.left}%` }}>
            {tick.label}
          </span>
        ))}
      </div>
    </div>
  );
}
