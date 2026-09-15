import { useState } from 'react';
import type { TrackMetricPoint } from '../api';
import { bandColor, computeBandRuns, computeBandScale, type BandMetric } from '../map/trackBands';
import { formatDistance, formatElevation, formatPace } from './format';
import { useUnitSystem, type UnitSystem } from './units';

// An arbitrary internal coordinate space for the elevation SVG's viewBox, not a rendered
// pixel width — the actual card stretches to the panel's own 100% width (index.css), and
// `preserveAspectRatio="none"` scales this coordinate space to fit whatever that turns out
// to be.
const STRIP_WIDTH = 300;
const CHART_HEIGHT = 32;

export interface TrackProfileProps {
  points: TrackMetricPoint[];
  metric: BandMetric;
  elevationAvailable: boolean;
}

function metricValue(p: TrackMetricPoint, metric: BandMetric): number | null {
  return metric === 'speed' ? p.speedMps : (p.heartrate ?? null);
}

function formatMetricValue(metric: BandMetric, value: number, system: UnitSystem): string {
  return metric === 'speed' ? formatPace(value, system) : `${Math.round(value)} bpm`;
}

/**
 * The straight-line companion to the map's own colored zone segments (trackBands.ts,
 * §4.3.1) — same per-vertex data, same band computation, laid out by distance along the
 * route instead of by geographic position, with an elevation curve underneath so climb and
 * effort can be read side by side. Mounted inside the same floating `.track-metric-toggle`
 * card as the Pace/Heart rate toggle (MapView.tsx) — deliberately small and unlabeled (no
 * axis ticks or gridlines): a general overlook, not a detailed instrument, per how the user
 * described wanting this to fit alongside what that card already shows.
 */
export function TrackProfile({ points, metric, elevationAvailable }: TrackProfileProps) {
  const [hoverIndex, setHoverIndex] = useState<number | null>(null);
  const system = useUnitSystem();

  const totalDistance = points.length > 0 ? points[points.length - 1]!.distanceM : 0;
  if (points.length < 2 || totalDistance <= 0) return null;

  const scale = computeBandScale(points, metric);
  const runs = computeBandRuns(points, metric, scale);

  const elevations = elevationAvailable ? points.map((p) => p.elevationM!) : [];
  const minElev = elevations.length > 0 ? Math.min(...elevations) : 0;
  const maxElev = elevations.length > 0 ? Math.max(...elevations) : 1;
  const elevSpan = maxElev - minElev || 1;

  const chartPoints = elevationAvailable
    ? points
        .map((p) => {
          const x = (p.distanceM / totalDistance) * STRIP_WIDTH;
          const y = CHART_HEIGHT - ((p.elevationM! - minElev) / elevSpan) * (CHART_HEIGHT - 4) - 2;
          return `${x.toFixed(1)},${y.toFixed(1)}`;
        })
        .join(' ')
    : '';
  const areaPath = elevationAvailable
    ? `M0,${CHART_HEIGHT} L${chartPoints} L${STRIP_WIDTH},${CHART_HEIGHT} Z`
    : '';

  function onMove(event: React.MouseEvent<HTMLDivElement | SVGSVGElement>) {
    const rect = event.currentTarget.getBoundingClientRect();
    const fraction = (event.clientX - rect.left) / rect.width;
    const targetDist = fraction * totalDistance;
    // Linear scan is fine here — a simplified activity's vertex count is small (tens to a
    // few hundred), not worth a binary search for a hover-only lookup.
    let nearest = 0;
    let nearestDelta = Infinity;
    for (let i = 0; i < points.length; i++) {
      const delta = Math.abs(points[i]!.distanceM - targetDist);
      if (delta < nearestDelta) {
        nearestDelta = delta;
        nearest = i;
      }
    }
    setHoverIndex(nearest);
  }

  const hovered = hoverIndex !== null ? points[hoverIndex] : undefined;
  const hoveredValue = hovered ? metricValue(hovered, metric) : null;

  return (
    <div className="track-profile">
      <div
        className="track-profile__strip"
        data-testid="track-profile-strip"
        onMouseMove={onMove}
        onMouseLeave={() => setHoverIndex(null)}
      >
        {runs.map((run, i) => {
          const start = points[run.startIndex]!.distanceM;
          const end = points[run.endIndex]!.distanceM;
          return (
            <div
              key={i}
              className="track-profile__segment"
              style={{ flexBasis: `${((end - start) / totalDistance) * 100}%`, background: bandColor(run.band) }}
            />
          );
        })}
      </div>
      {elevationAvailable && (
        <svg
          className="track-profile__elevation"
          viewBox={`0 0 ${STRIP_WIDTH} ${CHART_HEIGHT}`}
          preserveAspectRatio="none"
          onMouseMove={onMove}
          onMouseLeave={() => setHoverIndex(null)}
        >
          <path className="track-profile__elevation-fill" d={areaPath} />
          <polyline className="track-profile__elevation-line" points={chartPoints} />
        </svg>
      )}
      {/* Always rendered, hidden via visibility rather than mounted/unmounted — an
          appearing-and-disappearing row changes the card's own height, and since the card is
          bottom-anchored (position: absolute; bottom: ...), that shift moves the strip/chart
          the cursor is already over, which can push it back out from under the pointer and
          into a hide/show/hide flicker loop. Reserving the space keeps the card's height
          constant across hover states. */}
      <div
        className="track-profile__tooltip"
        data-testid="track-profile-tooltip"
        style={{ visibility: hovered && hoveredValue !== null ? 'visible' : 'hidden' }}
      >
        <span>{hovered ? formatDistance(hovered.distanceM, system) : ''}</span>
        <span>{hovered && hoveredValue !== null ? formatMetricValue(metric, hoveredValue, system) : ''}</span>
        {elevationAvailable && <span>{hovered?.elevationM !== undefined ? formatElevation(hovered.elevationM, system) : ''}</span>}
      </div>
    </div>
  );
}
