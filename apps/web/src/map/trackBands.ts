import type { GeoJSONSource, Map as MapLibreMap } from 'maplibre-gl';
import type { TrackMetricPoint } from '../api';

/**
 * Colored zone segments for whichever single activity is currently selected
 * (MapView.tsx) — a second, small GeoJSON layer drawn over the shared `tracks` MVT layer
 * (tracks.ts), not a modification to it. A vector-tile source has no way to carry per-vertex
 * color the way a client-built GeoJSON FeatureCollection can, and this is a handful of
 * features for one activity at a time, never the whole history — no `lineMetrics`/
 * `line-gradient` needed either, since each segment already carries its own resolved color
 * as a feature property rather than needing a continuous blend computed from one.
 */
export const BAND_SOURCE_ID = 'track-bands';
export const BAND_LAYER_ID = 'track-bands-line';

// Wider than tracks.ts's EMPHASIS_WIDTH (4.5) so this layer fully overlays the plain track
// underneath rather than needing to filter that track out of the shared MVT layer — simplest
// way to avoid drawing the same activity twice in conflicting styles.
const BAND_WIDTH = 6;

export const BAND_COUNT = 5;
// blue (slowest / lowest) -> red (fastest / highest), the same low-to-high reading direction
// regardless of metric.
const BAND_COLORS = ['#2b6cb0', '#38a169', '#d69e2e', '#dd6b20', '#c53030'];

export type BandMetric = 'speed' | 'heartrate';

interface LineStringFeature {
  type: 'Feature';
  properties: { color: string };
  geometry: { type: 'LineString'; coordinates: [number, number][] };
}

interface BandFeatureCollection {
  type: 'FeatureCollection';
  features: LineStringFeature[];
}

const EMPTY: BandFeatureCollection = { type: 'FeatureCollection', features: [] };

/**
 * Adds the band source/layer if they aren't already on the map — same idempotent-on-
 * `styledata` shape `ensureTrackLayer` (tracks.ts) already has, for the same reason
 * (`setStyle` discards custom layers on every theme swap).
 */
export function ensureBandLayer(map: MapLibreMap, beforeId: string | undefined): void {
  if (!map.getSource(BAND_SOURCE_ID)) {
    map.addSource(BAND_SOURCE_ID, { type: 'geojson', data: EMPTY });
  }
  if (!map.getLayer(BAND_LAYER_ID)) {
    map.addLayer(
      {
        id: BAND_LAYER_ID,
        type: 'line',
        source: BAND_SOURCE_ID,
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: {
          // Each feature carries its own resolved color — no match/step expression to
          // maintain here at all.
          'line-color': ['get', 'color'],
          'line-width': BAND_WIDTH,
          'line-opacity': 0.95,
        },
      },
      beforeId,
    );
  }
}

function metricValue(p: TrackMetricPoint, metric: BandMetric): number | null {
  return metric === 'speed' ? p.speedMps : (p.heartrate ?? null);
}

/** Equal-width bins between *this activity's own* min/max for the active metric — "where in
 *  this run was I fastest" is a question about that run, not a fixed scale across every run
 *  ever recorded (the same reasoning Trends.tsx's log-scaled bar heights already apply
 *  locally, per window, rather than globally). */
export interface BandScale {
  min: number;
  max: number;
}

export function computeBandScale(points: TrackMetricPoint[], metric: BandMetric): BandScale {
  const values = points.map((p) => metricValue(p, metric)).filter((v): v is number => v !== null);
  if (values.length === 0) return { min: 0, max: 1 };
  return { min: Math.min(...values), max: Math.max(...values) };
}

function bandIndex(value: number, scale: BandScale): number {
  const span = scale.max - scale.min || 1;
  const idx = Math.floor(((value - scale.min) / span) * BAND_COUNT);
  return Math.min(BAND_COUNT - 1, Math.max(0, idx));
}

/** The band's own color, exported alongside computeBandScale so the legend (MapView.tsx) can
 *  render "this color means this value range" without duplicating the bin math. */
export function bandColor(index: number): string {
  return BAND_COLORS[index]!;
}

/** One contiguous run of consecutive points that land in the same band, as an index range
 *  into the `points` array `computeBandRuns` was given — `endIndex` inclusive. Adjacent runs
 *  share their boundary vertex (one run's `endIndex` is the next run's `startIndex`) so a
 *  consumer rendering each run as its own line segment leaves no gap at a band transition. */
export interface BandRun {
  band: number;
  startIndex: number;
  endIndex: number;
}

/**
 * Groups consecutive points into runs of the same band — shared by `setTrackBands` (which
 * renders each run as a lon/lat `LineString` segment on the map) and `TrackProfile.tsx`
 * (which renders the same runs as distance-fraction-width segments in a straight strip). One
 * merge algorithm, two renderings, rather than two copies of it.
 */
export function computeBandRuns(points: TrackMetricPoint[], metric: BandMetric, scale: BandScale): BandRun[] {
  const runs: BandRun[] = [];
  if (points.length < 2) return runs;

  let currentBand = -1;
  let startIndex = 0;

  for (let i = 1; i < points.length; i++) {
    const value = metricValue(points[i]!, metric);
    if (value === null) continue; // heart rate is all-or-nothing upstream; shouldn't happen
    const band = bandIndex(value, scale);
    if (band !== currentBand) {
      if (currentBand !== -1 && i - 1 > startIndex) {
        runs.push({ band: currentBand, startIndex, endIndex: i - 1 });
      }
      currentBand = band;
      startIndex = i - 1;
    }
  }
  if (points.length - 1 > startIndex) {
    runs.push({ band: currentBand, startIndex, endIndex: points.length - 1 });
  }
  return runs;
}

/**
 * Builds one `LineString` feature per contiguous same-band run (`computeBandRuns`) — fewer
 * features than one per raw vertex pair, identical visual result, since adjacent same-band
 * segments render indistinguishably from one merged segment anyway.
 */
export function setTrackBands(map: MapLibreMap, points: TrackMetricPoint[], metric: BandMetric): void {
  const source = map.getSource(BAND_SOURCE_ID) as GeoJSONSource | undefined;
  if (!source) return;
  if (points.length < 2) {
    source.setData(EMPTY);
    return;
  }

  const scale = computeBandScale(points, metric);
  const runs = computeBandRuns(points, metric, scale);
  const features: LineStringFeature[] = runs.map((run) => ({
    type: 'Feature',
    properties: { color: bandColor(run.band) },
    geometry: {
      type: 'LineString',
      coordinates: points.slice(run.startIndex, run.endIndex + 1).map((p): [number, number] => [p.lon, p.lat]),
    },
  }));

  source.setData({ type: 'FeatureCollection', features });
}

/** Called whenever the selection leaves exactly one activity. */
export function clearTrackBands(map: MapLibreMap): void {
  const source = map.getSource(BAND_SOURCE_ID) as GeoJSONSource | undefined;
  source?.setData(EMPTY);
}
