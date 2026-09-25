import { useState } from 'react';
import type { TrendBucket, TrendPeriod } from '../api';
import { formatElevation, formatShortDate, formatTotalDistance, formatTotalHours } from './format';
import { useUnitSystem } from './units';
import { useTrends } from './useTrends';

/** Log-scaled against the window's own busiest period, same reasoning and same formula as
 *  RangePicker.tsx's barHeightPercent — one outlier week/month shouldn't flatten every
 *  ordinary one on the same axis down to the visibility floor. */
function barHeightPercent(period: TrendPeriod, peak: number): number {
  if (peak <= 0) return 0;
  return (Math.log(period.distanceMeters + 1) / Math.log(peak + 1)) * 100;
}

/**
 * docs/SPEC.md FR-9's "trends" — distance per week or month over the trailing year, the
 * first Performance Analysis feature built. Plain divs, same bar-chart approach
 * RangePicker.tsx/ActivityHistogram.tsx already use — no charting library in this project to
 * reach for instead. Mounted on ProfilePage below the activity grid: both are "look back at
 * what I did" views for a signed-in account, not map-screen chrome.
 */
export function Trends() {
  const { bucket, setBucket, trends, error } = useTrends();
  const [hovered, setHovered] = useState<number | null>(null);
  const system = useUnitSystem();

  const periods = trends?.periods ?? [];
  const peak = periods.reduce((max, p) => Math.max(max, p.distanceMeters), 0);
  const hoveredPeriod = hovered !== null ? periods[hovered] : undefined;
  const firstPeriod = periods[0];
  const lastPeriod = periods[periods.length - 1];

  return (
    <section className="trends" aria-label="Trends">
      <div className="trends__head">
        <div>
          <h2 className="trends__title">Trends</h2>
          <p className="trends__subtext">Distance over time — hover a bar for the full breakdown.</p>
        </div>
        <div className="trends__bucket" role="group" aria-label="Group by">
          {(['week', 'month'] as TrendBucket[]).map((b) => (
            <button
              key={b}
              type="button"
              className="trends__bucket-btn"
              aria-pressed={bucket === b}
              onClick={() => setBucket(b)}
            >
              {b === 'week' ? 'Week' : 'Month'}
            </button>
          ))}
        </div>
      </div>

      {error && <p className="trends__error">{error}</p>}

      {trends && periods.length === 0 && !error && (
        <p className="trends__empty">Nothing recorded in this window yet.</p>
      )}

      {periods.length > 0 && (
        // onClick here (not on each bar) is what clears the tooltip on touch — a tap that
        // lands on empty chart space (not a bar, which stops its own click from bubbling
        // here) is the touch equivalent of a mouse just moving off every bar.
        <div className="trends__chart" data-testid="trends-chart" onClick={() => setHovered(null)}>
          <div className="trends__bars">
            {periods.map((p, i) => (
              <div
                key={p.periodStart}
                className="trends__bar"
                style={{ height: `${barHeightPercent(p, peak)}%` }}
                // pointerType-guarded, not onMouseEnter/onMouseLeave — found live (a real
                // bug, not just a test artifact): a tap on a touchscreen still triggers a
                // browser's own compatibility mouse-event sequence for anything only
                // listening for mouse events, mouseleave included, right after the click —
                // which immediately re-cleared the very state onClick below had just set,
                // silently defeating the tap fallback entirely. PointerEvent carries
                // `pointerType` ('mouse' | 'touch' | 'pen'), so this reacts to a genuine
                // mouse hover only and leaves touch to the onClick handler alone.
                onPointerEnter={(e) => {
                  if (e.pointerType === 'mouse') setHovered(i);
                }}
                onPointerLeave={(e) => {
                  if (e.pointerType === 'mouse') setHovered((h) => (h === i ? null : h));
                }}
                // Touch has no hover — tapping a bar is this metric's whole touch
                // equivalent of hovering it: shows the tooltip, and tapping the same bar
                // again hides it. Harmless on desktop too (a mouse click just confirms the
                // hover state already showing). stopPropagation so the chart's own onClick
                // above (which clears) doesn't immediately undo this in the same event.
                onClick={(e) => {
                  e.stopPropagation();
                  setHovered((h) => (h === i ? null : i));
                }}
                data-testid="trends-bar"
              />
            ))}
          </div>
          {firstPeriod && lastPeriod && (
            <div className="trends__axis">
              <span>{formatShortDate(firstPeriod.periodStart)}</span>
              <span>{formatShortDate(lastPeriod.periodStart)}</span>
            </div>
          )}
        </div>
      )}

      {hoveredPeriod && (
        <div className="trends__tooltip" data-testid="trends-tooltip">
          <span className="trends__tooltip-date">{formatShortDate(hoveredPeriod.periodStart)}</span>
          <span>{formatTotalDistance(hoveredPeriod.distanceMeters, system)}</span>
          <span>{hoveredPeriod.count.toLocaleString()} activities</span>
          <span>{formatTotalHours(hoveredPeriod.movingSeconds)} h moving</span>
          <span>{formatElevation(hoveredPeriod.elevationGainM, system)} gain</span>
        </div>
      )}
    </section>
  );
}
