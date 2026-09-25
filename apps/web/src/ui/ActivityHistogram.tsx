import type { HistogramBucket } from '../api';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { DateRangeSlider } from './DateRangeSlider';
import { formatDayLabel } from './format';
import { RangePicker, type DateRange } from './RangePicker';
import { MOBILE_QUERY, useMediaQuery } from './useMediaQuery';
import { t, tn } from '../i18n';

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
 *
 * **On a phone, none of that.** The bars, handles and month rail are too small to use with a
 * fingertip on a phone-width strip, and the legend costs height a short screen can't spare, so
 * below index.css's phone breakpoint the whole footer is just DateRangeSlider.tsx — two knobs
 * over a 15-day window of calendar days, stepped 5 days at a time by its own Earlier/Later
 * buttons, with the selected dates under them. Switched in JS
 * (`useMediaQuery`), not hidden with CSS, so the desktop chart isn't mounted and measuring
 * itself behind a phone layout.
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
  /** The phone slider's two ends: this user's first activity day and today (YYYY-MM-DD). */
  historyStart: string;
  today: string;
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
  historyStart,
  today,
}: ActivityHistogramProps) {
  const isPhone = useMediaQuery(MOBILE_QUERY);
  const first = days[0];
  const last = days[days.length - 1];

  if (isPhone) {
    // A selection can predate what's been loaded as history's start only transiently (the
    // default range resolves from the same page), but never let a knob sit off the scale.
    const start = selectedRange.from < historyStart ? selectedRange.from : historyStart;
    const end = selectedRange.to > today ? selectedRange.to : today;
    return (
      <footer className="activity-histogram activity-histogram--compact" data-testid="activity-histogram">
        <DateRangeSlider first={start} last={end} value={selectedRange} onChange={onChangeSelection} />
      </footer>
    );
  }

  return (
    <footer className="activity-histogram" data-testid="activity-histogram">
      <div className="activity-histogram__legend">
        {/* What's currently panned into view, independent of the selection below: this can
            show a completely different stretch of history while a selection elsewhere stays
            exactly where it was. */}
        <div className="activity-histogram__legend-block">
          <div className="activity-histogram__legend-label">{t('histogram.shown_days')}</div>
          <div className="activity-histogram__range-label" data-testid="visible-window-label">
            {first && last ? `${formatDayLabel(first.date)} – ${formatDayLabel(last.date)}` : '—'}
          </div>
          <div className="activity-histogram__stats">
            {first && last ? tn('histogram.active_days', days.length) : t('histogram.no_days')}
          </div>
        </div>

        <div className="activity-histogram__legend-block">
          <div className="activity-histogram__legend-label">{t('histogram.selected_range')}</div>
          <div className="activity-histogram__range-label" data-testid="selected-range-label">
            {formatDayLabel(selectedRange.from)} – {formatDayLabel(selectedRange.to)}
          </div>
          <div className="activity-histogram__stats" data-testid="selected-range-stats">
            {tn('histogram.range_days', selectedRangeDays)} · {tn('histogram.active_days', selectedActiveDays)}
          </div>
        </div>
      </div>

      <div className="activity-histogram__chart-area">
        <button
          type="button"
          className="range-picker__page"
          data-testid="range-picker-earlier"
          disabled={!canPanEarlier}
          aria-label={t('histogram.earlier')}
          title={t('histogram.earlier')}
          onClick={() => onPan(-pageStep)}
        >
          <ChevronLeft size={16} />
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
          aria-label={t('histogram.later')}
          title={t('histogram.later')}
          onClick={() => onPan(pageStep)}
        >
          <ChevronRight size={16} />
        </button>
      </div>
    </footer>
  );
}
