import { useState } from 'react';
import type { BestEffortMetric, BestEffortPoint } from '../api';
import { formatPace } from './format';
import { useUnitSystem, type UnitSystem } from './units';
import { useBestEfforts } from './useBestEfforts';

const CHART_WIDTH = 320;
const CHART_HEIGHT = 140;

/** VISION.md §5.3's fixed window list, mirrored from internal/ingest.bestEffortWindowsS
 *  — the chart's x-axis domain is always this full 5s–1h span, regardless of which windows
 *  the account actually has data for, so the axis doesn't rescale oddly for a short history. */
const ALL_WINDOWS_S = [5, 10, 30, 60, 120, 300, 600, 1200, 1800, 3600];

function windowLabel(windowS: number): string {
  if (windowS < 60) return `${windowS}s`;
  if (windowS < 3600) return `${windowS / 60}m`;
  return `${windowS / 3600}h`;
}

function formatValue(metric: BestEffortMetric, value: number, system: UnitSystem): string {
  return metric === 'pace' ? formatPace(value, system) : `${Math.round(value)} bpm`;
}

/** Log-scaled x — the domain spans three orders of magnitude (5s to 1h), same reasoning
 *  RangePicker/Trends already use log scaling for: a linear axis would crowd every
 *  short-duration point into a sliver at one end. */
function xForWindow(windowS: number): number {
  const logMin = Math.log(ALL_WINDOWS_S[0]!);
  const logMax = Math.log(ALL_WINDOWS_S[ALL_WINDOWS_S.length - 1]!);
  return ((Math.log(windowS) - logMin) / (logMax - logMin)) * CHART_WIDTH;
}

/**
 * VISION.md §5.3's best-effort curve — the best pace or heart rate any activity ever
 * sustained, per standard duration, all-time. Mounted on ProfilePage below Trends. Plain
 * inline SVG (a polyline plus one hoverable circle per point), no charting library —
 * consistent with Trends/RangePicker/ActivityHistogram, none of which use one either.
 */
export function BestEfforts() {
  const { metric, setMetric, bestEfforts, error } = useBestEfforts();
  const [hovered, setHovered] = useState<number | null>(null);
  const system = useUnitSystem();

  const points = bestEfforts?.points ?? [];
  const values = points.map((p) => p.value);
  const minValue = values.length > 0 ? Math.min(...values) : 0;
  const maxValue = values.length > 0 ? Math.max(...values) : 1;
  const valueRange = maxValue - minValue || 1;

  function yForValue(value: number): number {
    return CHART_HEIGHT - ((value - minValue) / valueRange) * (CHART_HEIGHT - 12) - 6;
  }

  const plotted = points.map((p) => ({ point: p, x: xForWindow(p.windowS), y: yForValue(p.value) }));
  const polyline = plotted.map(({ x, y }) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ');
  const hoveredPoint: BestEffortPoint | undefined = hovered !== null ? points[hovered] : undefined;

  return (
    <section className="best-efforts" aria-label="Best efforts">
      <div className="trends__head">
        <div>
          <h2 className="trends__title">Best efforts</h2>
          <p className="trends__subtext">
            Your best pace or heart rate sustained over each duration, ever — hover a point for the exact value.
          </p>
        </div>
        <div className="trends__bucket" role="group" aria-label="Metric">
          {(['pace', 'heartrate'] as BestEffortMetric[]).map((m) => (
            <button
              key={m}
              type="button"
              className="trends__bucket-btn"
              aria-pressed={metric === m}
              onClick={() => setMetric(m)}
            >
              {m === 'pace' ? 'Pace' : 'Heart rate'}
            </button>
          ))}
        </div>
      </div>

      {error && <p className="trends__error">{error}</p>}

      {bestEfforts && points.length === 0 && !error && (
        <p className="trends__empty">Not enough recorded activity yet to plot a curve.</p>
      )}

      {points.length > 0 && (
        // onClick here (not on each point) clears the tooltip on a tap that lands on empty
        // chart space — the touch equivalent of a mouse moving off every point.
        <div className="best-efforts__chart" data-testid="best-efforts-chart" onClick={() => setHovered(null)}>
          <svg
            viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`}
            preserveAspectRatio="none"
            className="best-efforts__svg"
          >
            <polyline className="best-efforts__line" points={polyline} />
            {plotted.map(({ point, x, y }, i) => (
              <g
                key={point.windowS}
                // pointerType-guarded — see Trends.tsx's identical fix for why plain
                // onMouseEnter/onMouseLeave is wrong here: a tap's own browser-synthesized
                // compatibility mouse events (mouseleave included) would otherwise
                // immediately re-clear the state onClick below just set.
                onPointerEnter={(e) => {
                  if (e.pointerType === 'mouse') setHovered(i);
                }}
                onPointerLeave={(e) => {
                  if (e.pointerType === 'mouse') setHovered((h) => (h === i ? null : h));
                }}
                // Touch has no hover — tapping a point is its whole touch equivalent:
                // shows the tooltip, tapping the same point again hides it. stopPropagation
                // so the chart's own onClick above doesn't immediately undo this.
                onClick={(e) => {
                  e.stopPropagation();
                  setHovered((h) => (h === i ? null : i));
                }}
              >
                {/* Invisible, wider than the visible point below — an r=4 circle (8px
                    across) is a real mouse target but too small for a fingertip; this
                    shares the same handlers via the parent <g>, so it doesn't matter which
                    of the two actually receives a given pointer event. */}
                <circle cx={x} cy={y} r={14} fill="transparent" data-testid="best-effort-point-hit-area" />
                <circle className="best-efforts__point" cx={x} cy={y} r={4} data-testid="best-effort-point" />
              </g>
            ))}
          </svg>
          <div className="best-efforts__axis">
            {points.map((p) => (
              <span
                key={p.windowS}
                className="best-efforts__axis-label"
                style={{ left: `${(xForWindow(p.windowS) / CHART_WIDTH) * 100}%` }}
              >
                {windowLabel(p.windowS)}
              </span>
            ))}
          </div>
        </div>
      )}

      {hoveredPoint && (
        <div className="trends__tooltip" data-testid="best-efforts-tooltip">
          <span className="trends__tooltip-date">{windowLabel(hoveredPoint.windowS)}</span>
          <span>{formatValue(metric, hoveredPoint.value, system)}</span>
        </div>
      )}
    </section>
  );
}
