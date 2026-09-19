import type { HistogramBucket } from '../api';
import { RangePicker, type DateRange } from './RangePicker';

/**
 * The docked bottom timeline. The interactive strip is
 * RangePicker.tsx — one bar per day that has activity, drag to resize or slide the
 * selection, drag the strip to page through history.
 *
 * Two regions side by side: a legend column, sized to its own content rather than to the
 * Activities panel's width — the sidebar is independently resizable (260–560px), and there's
 * no reason this footer should shrink or grow along with that drag — and a chart area filling
 * whatever's left, `flex: 1`, so the date picker gets as much room as it can.
 * The legend stacks two independent pieces of information as one column, not side by side
 * (splitting the width in two left each date range only half the room to render in, which
 * truncated mid-number well before the text ran out of space to matter): SHOWN DAYS answers
 * "what's currently on screen" (a different question the pan position owns, not the
 * selection); SELECTED RANGE answers "what's selected" below it (the range and how much of it
 * actually has activity — km/activity-count totals live in ActivitiesPanel's own subtext
 * already, so repeating them here would just be the same numbers twice). The Earlier/Later
 * buttons flank the chart itself rather than sitting in the legend — icon-only, actively styled
 * when there's somewhere
 * further to page (canPanEarlier/canPanLater below) and flatly inert otherwise.
 */
export interface ActivityHistogramProps {
  /** The days-with-activity currently on screen — see useActivityDays. */
  days: HistogramBucket[];
  onPan: (deltaBars: number) => void;
  pageStep: number;
  canPanEarlier: boolean;
  canPanLater: boolean;
  selectedRange: DateRange;
  onChangeSelection: (next: DateRange) => void;
  /** Calendar days spanned by `selectedRange`, inclusive — MapView's, since it's the one
   *  that already has dateMath's dayDiff and the selection's own dates. */
  selectedRangeDays: number;
  /** How many of those calendar days actually have at least one activity — distinct
   *  UTC dates among the activities `selectedRange` matched, computed in MapView from the
   *  same list the map and panel already fetched, not from `days` (which is the pan
   *  window and can be scrolled somewhere else entirely). A count of *days*, not of
   *  activities — reported live as a bug when a single selected day with 2 activities on
   *  it read "1 day · 1 with activity", which looks exactly like an activity count when
   *  both numbers collapse to 1. Not a computation bug; the rendered label just didn't say
   *  "day" a second time, so keep that word in whatever phrasing uses this number. */
  selectedActiveDays: number;
  /** Forwarded straight to RangePicker.tsx — see its own doc comment. */
  onCapacityChange: (barsPerView: number) => void;
}

/** "9 MAR 2026", matching main-screen-v6.png's date labels. */
function formatRangeLabel(date: string): string {
  const d = new Date(`${date}T00:00:00Z`);
  const day = d.getUTCDate();
  const month = d.toLocaleDateString(undefined, { month: 'short', timeZone: 'UTC' }).toUpperCase();
  return `${day} ${month} ${d.getUTCFullYear()}`;
}

export function ActivityHistogram({
  days,
  onPan,
  pageStep,
  canPanEarlier,
  canPanLater,
  selectedRange,
  onChangeSelection,
  selectedRangeDays,
  selectedActiveDays,
  onCapacityChange,
}: ActivityHistogramProps) {
  const first = days[0];
  const last = days[days.length - 1];

  return (
    <footer className="activity-histogram" data-testid="activity-histogram">
      <div className="activity-histogram__legend">
        {/* What's currently panned into view, independent of the selection below: this can
            show a completely different stretch of history while a selection elsewhere stays
            exactly where it was. */}
        <div className="activity-histogram__legend-block">
          <div className="activity-histogram__legend-label">Shown days</div>
          <div className="activity-histogram__range-label" data-testid="visible-window-label">
            {first && last ? `${formatRangeLabel(first.date)} – ${formatRangeLabel(last.date)}` : '—'}
          </div>
          <div className="activity-histogram__stats">
            {first && last ? `${days.length} active ${days.length === 1 ? 'day' : 'days'}` : 'No activity days to show'}
          </div>
        </div>

        <div className="activity-histogram__legend-block">
          <div className="activity-histogram__legend-label">Selected range</div>
          <div className="activity-histogram__range-label" data-testid="selected-range-label">
            {formatRangeLabel(selectedRange.from)} – {formatRangeLabel(selectedRange.to)}
          </div>
          <div className="activity-histogram__stats" data-testid="selected-range-stats">
            {selectedRangeDays}-day range · {selectedActiveDays} active {selectedActiveDays === 1 ? 'day' : 'days'}
          </div>
        </div>
      </div>

      <div className="activity-histogram__chart-area">
        <button
          type="button"
          className="range-picker__page"
          data-testid="range-picker-earlier"
          disabled={!canPanEarlier}
          aria-label="Earlier"
          title="Earlier"
          onClick={() => onPan(-pageStep)}
        >
          <ChevronIcon direction="left" />
        </button>

        <RangePicker
          days={days}
          onPan={onPan}
          selectedRange={selectedRange}
          onChangeSelection={onChangeSelection}
          onCapacityChange={onCapacityChange}
        />

        <button
          type="button"
          className="range-picker__page"
          data-testid="range-picker-later"
          disabled={!canPanLater}
          aria-label="Later"
          title="Later"
          onClick={() => onPan(pageStep)}
        >
          <ChevronIcon direction="right" />
        </button>
      </div>
    </footer>
  );
}

/** A plain chevron for the icon-only Earlier/Later buttons — text labels moved to
 *  `aria-label`/`title` since the buttons now flank the chart at a fixed 34px width. */
function ChevronIcon({ direction }: { direction: 'left' | 'right' }) {
  const d = direction === 'left' ? 'M9 4l-6 6 6 6' : 'M7 4l6 6-6 6';
  return (
    <svg viewBox="0 0 16 16" width="14" height="14">
      <path d={d} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
