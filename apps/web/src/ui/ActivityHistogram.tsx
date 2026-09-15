import type { HistogramBucket } from '../api';
import { RangePicker, type DateRange } from './RangePicker';

/**
 * The docked bottom timeline. The interactive strip is
 * RangePicker.tsx — one bar per day that has activity, drag to resize or slide the
 * selection, drag the strip to page through history.
 *
 * The header is two independent pieces of information side by side, not one stack: the left
 * side answers "what's selected" (the range and how much of it actually has activity —
 * km/activity-count totals live in ActivitiesPanel's own subtext already, so repeating them
 * here would just be the same numbers twice); the right side answers "what's currently on
 * screen", which is a different question the pan position owns and the selection doesn't.
 * The Earlier/Later buttons live in that same top row, next to the window they page — not
 * flanking the chart below, which is what let the chart itself stretch the full width of the
 * footer rather than sharing it with two button columns.
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
}: ActivityHistogramProps) {
  const first = days[0];
  const last = days[days.length - 1];

  return (
    <footer className="activity-histogram" data-testid="activity-histogram">
      <div className="activity-histogram__header">
        <div className="activity-histogram__summary">
          <div className="activity-histogram__range-label" data-testid="selected-range-label">
            {formatRangeLabel(selectedRange.from)} – {formatRangeLabel(selectedRange.to)}
          </div>
          <div className="activity-histogram__stats" data-testid="selected-range-stats">
            {selectedRangeDays}-day range · {selectedActiveDays} active {selectedActiveDays === 1 ? 'day' : 'days'}
          </div>
        </div>

        <div className="activity-histogram__window">
          {/* What's currently panned into view — a count of active days now rather than a
              span of calendar time, since packed bars no longer imply one. Independent of
              the selection to its left: this can show a completely different stretch of
              history while a selection elsewhere stays exactly where it was. */}
          <span className="activity-histogram__window-label" data-testid="visible-window-label" aria-hidden="true">
            {first && last
              ? `${days.length} active ${days.length === 1 ? 'day' : 'days'} · ${formatRangeLabel(first.date)} – ${formatRangeLabel(last.date)}`
              : 'No activity days to show'}
          </span>
          <button
            type="button"
            className="range-picker__page"
            data-testid="range-picker-earlier"
            disabled={!canPanEarlier}
            onClick={() => onPan(-pageStep)}
          >
            ‹ Earlier
          </button>
          <button
            type="button"
            className="range-picker__page"
            data-testid="range-picker-later"
            disabled={!canPanLater}
            onClick={() => onPan(pageStep)}
          >
            Later ›
          </button>
        </div>
      </div>

      <RangePicker
        days={days}
        onPan={onPan}
        selectedRange={selectedRange}
        onChangeSelection={onChangeSelection}
      />
    </footer>
  );
}
