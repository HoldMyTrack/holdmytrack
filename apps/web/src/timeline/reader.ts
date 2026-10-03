// Reads a Google Maps Timeline export — the `Timeline.json` an Android phone writes from
// Settings → Location → Timeline → Export Timeline data — into the activities the import sends
// to POST /v1/sync/activities (docs/IMPLEMENTATION.md §4.0.5). It runs in the browser, so only
// the selected movement leaves the device: the file's visits, its home and work labels and its
// raw Wi-Fi and activity signals are never uploaded.
//
// The export is undocumented; this follows real files. `semanticSegments` holds three kinds of
// entry: a visit (a place, skipped), an activity (start, end, a guessed mode and a distance,
// but no route), and a `timelinePath` (the points the phone kept, in two-hour buckets that
// don't line up with activities). An activity's route is the path points inside its time span,
// between its own start and end.

export type TimelinePoint = { lat: number; lon: number; time: string };

export type TimelineSegment = {
  /** `seg-<start>-<end>` in epoch seconds: the same segment in a later export keeps it. */
  id: string;
  /** The export's own timestamps, with the offset of where it happened. */
  start: string;
  end: string;
  /** The local date it started on, YYYY-MM-DD. */
  day: string;
  /** Timeline's mode, `WALKING`, `IN_PASSENGER_VEHICLE`… */
  mode: string;
  /** The activity_type it's imported as (`activityType`). */
  type: string;
  distanceM: number;
  points: TimelinePoint[];
};

export type TimelineRead = {
  segments: TimelineSegment[];
  /** Activities with fewer than two distinct places: nothing to draw. */
  skipped: number;
};

/** The other location exports Google has written, recognized so the error can name them. */
export type TimelineFormat = 'ios' | 'takeout';

export class TimelineFormatError extends Error {
  readonly format: TimelineFormat | null;
  constructor(format: TimelineFormat | null) {
    super(format ? `unsupported Timeline format: ${format}` : 'not a Timeline export');
    this.format = format;
  }
}

/** Modes left unticked until chosen: a flight drawn as a line would clear fog across a continent. */
export const MODES_OFF_BY_DEFAULT: ReadonlySet<string> = new Set(['FLYING']);

const TYPES: Record<string, string> = {
  WALKING: 'walking',
  RUNNING: 'running',
  CYCLING: 'cycling',
  IN_PASSENGER_VEHICLE: 'driving',
  IN_VEHICLE: 'driving',
  UNKNOWN_ACTIVITY_TYPE: 'unknown',
};

/** Timeline's mode as an activity_type: the names the app's other sources use where there is
 *  one, otherwise the mode in lower case without its `IN_` (`IN_BUS` → `bus`). */
export function activityType(mode: string): string {
  return TYPES[mode] ?? mode.toLowerCase().replace(/^in_/, '');
}

/** "41.3929419°, -81.7433579°" (also tolerating a `geo:` prefix or no degree signs). */
export function parseLatLng(s: unknown): { lat: number; lon: number } | null {
  if (typeof s !== 'string') return null;
  const parts = s.replace(/^geo:/, '').replace(/°/g, '').split(',');
  if (parts.length !== 2) return null;
  const lat = Number(parts[0]);
  const lon = Number(parts[1]);
  if (!Number.isFinite(lat) || !Number.isFinite(lon) || Math.abs(lat) > 90 || Math.abs(lon) > 180) return null;
  return { lat, lon };
}

type Obj = Record<string, unknown>;
const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v);

function detectOther(data: unknown): TimelineFormat | null {
  if (Array.isArray(data) && data.some((e) => isObj(e) && 'startTime' in e && ('visit' in e || 'activity' in e || 'timelinePath' in e))) return 'ios';
  if (isObj(data) && ('timelineObjects' in data || 'locations' in data)) return 'takeout';
  return null;
}

type Timed = { ms: number; time: string; lat: number; lon: number };

/** Index of the first entry with ms > t. */
function upperBound(arr: Timed[], t: number): number {
  let lo = 0;
  let hi = arr.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (arr[mid]!.ms <= t) lo = mid + 1;
    else hi = mid;
  }
  return lo;
}

const EARTH_M = 6371008.8;
function metersBetween(a: TimelinePoint, b: TimelinePoint): number {
  const rad = Math.PI / 180;
  const dLat = (b.lat - a.lat) * rad;
  const dLon = (b.lon - a.lon) * rad;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * rad) * Math.cos(b.lat * rad) * Math.sin(dLon / 2) ** 2;
  return 2 * EARTH_M * Math.asin(Math.min(1, Math.sqrt(h)));
}

export function readTimeline(data: unknown): TimelineRead {
  if (!isObj(data) || !Array.isArray(data.semanticSegments)) throw new TimelineFormatError(detectOther(data));

  const path: Timed[] = [];
  const activities: Obj[] = [];
  for (const seg of data.semanticSegments) {
    if (!isObj(seg)) continue;
    if (Array.isArray(seg.timelinePath)) {
      for (const p of seg.timelinePath) {
        if (!isObj(p) || typeof p.time !== 'string') continue;
        const at = parseLatLng(p.point);
        const ms = Date.parse(p.time);
        if (at && Number.isFinite(ms)) path.push({ ms, time: p.time, ...at });
      }
    }
    if (isObj(seg.activity)) activities.push(seg);
  }
  path.sort((a, b) => a.ms - b.ms);

  const segments: TimelineSegment[] = [];
  let skipped = 0;
  for (const seg of activities) {
    const act = seg.activity as Obj;
    const start = seg.startTime;
    const end = seg.endTime;
    const startMs = typeof start === 'string' ? Date.parse(start) : NaN;
    const endMs = typeof end === 'string' ? Date.parse(end) : NaN;
    const from = parseLatLng(isObj(act.start) ? act.start.latLng : null);
    const to = parseLatLng(isObj(act.end) ? act.end.latLng : null);
    if (typeof start !== 'string' || typeof end !== 'string' || !(endMs > startMs) || !from || !to) {
      skipped += 1;
      continue;
    }
    const inside = path.slice(upperBound(path, startMs), upperBound(path, endMs - 1));
    const timed: Timed[] = [{ ms: startMs, time: start, ...from }, ...inside, { ms: endMs, time: end, ...to }];
    // Strictly increasing times: the parser keeps one point per instant anyway.
    const points: TimelinePoint[] = [];
    let last = -Infinity;
    for (const p of timed) {
      if (p.ms <= last) continue;
      last = p.ms;
      points.push({ lat: p.lat, lon: p.lon, time: p.time });
    }
    if (new Set(points.map((p) => `${p.lat},${p.lon}`)).size < 2) {
      skipped += 1;
      continue;
    }
    let distanceM = 0;
    for (let i = 1; i < points.length; i++) distanceM += metersBetween(points[i - 1]!, points[i]!);
    const top = isObj(act.topCandidate) ? act.topCandidate : {};
    const mode = typeof top.type === 'string' && top.type !== '' ? top.type : 'UNKNOWN_ACTIVITY_TYPE';
    segments.push({
      id: `seg-${Math.floor(startMs / 1000)}-${Math.floor(endMs / 1000)}`,
      start,
      end,
      day: start.slice(0, 10),
      mode,
      type: activityType(mode),
      distanceM,
      points,
    });
  }
  segments.sort((a, b) => Date.parse(a.start) - Date.parse(b.start));
  return { segments, skipped };
}

export type Selection = { from: string; to: string; modes: ReadonlySet<string> };

/** The segments a selection imports: started on a day within [from, to], in a chosen mode. */
export function selectSegments(segments: readonly TimelineSegment[], sel: Selection): TimelineSegment[] {
  return segments.filter((s) => s.day >= sel.from && s.day <= sel.to && sel.modes.has(s.mode));
}

export type ModeSummary = { mode: string; type: string; count: number; distanceM: number };

/** One row per mode, most activities first. */
export function summarize(segments: readonly TimelineSegment[]): ModeSummary[] {
  const by = new Map<string, ModeSummary>();
  for (const s of segments) {
    const row = by.get(s.mode) ?? { mode: s.mode, type: s.type, count: 0, distanceM: 0 };
    row.count += 1;
    row.distanceM += s.distanceM;
    by.set(s.mode, row);
  }
  return [...by.values()].sort((a, b) => b.count - a.count || a.mode.localeCompare(b.mode));
}

export type SyncActivity = { external_id: string; activity_type: string; points: TimelinePoint[] };

/** The sync endpoint's activities, `size` to a request (its own limit is 100). */
export function syncBatches(segments: readonly TimelineSegment[], size = 100): SyncActivity[][] {
  const out: SyncActivity[][] = [];
  for (let i = 0; i < segments.length; i += size) {
    out.push(segments.slice(i, i + size).map((s) => ({ external_id: s.id, activity_type: s.type, points: s.points })));
  }
  return out;
}
