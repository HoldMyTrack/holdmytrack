import type { TrackEdit, TrackPoint } from '../api';

/**
 * The Edit track session's history (§4.7.7) — pure functions, no React, so the replay/fold
 * logic is unit-testable on its own (tests/editTrackOps.test.ts).
 *
 * A session is the edit the track already had when the editor opened (`base`) plus a stack of
 * ops committed since. What the map shows is always a replay of the whole stack on top of the
 * original points, never a mutated copy: Undo pops one op, Cancel drops the stack, and Apply
 * folds it into the one `TrackEdit` the server stores. Every op addresses points by timestamp,
 * the same way the server's spec does, so the fold is just collecting them.
 */
export type EditOp =
  /** Keep only the points from one timestamp to another (inclusive) — the Chop button. */
  | { kind: 'chop'; keep: [number, number] }
  /** Remove the points between two knobs, keeping both knob points — the Cut button. */
  | { kind: 'cut'; remove: [number, number] }
  /** Remove one point — a click in Delete point mode. */
  | { kind: 'drop'; t: number }
  /** Put one point somewhere else — a drag in Move point mode. */
  | { kind: 'move'; t: number; to: [number, number] }
  /** Discard every earlier edit, including the one saved before this session. */
  | { kind: 'reset' };

/** Folds `base` and the op stack into one edit. Never returns empty arrays — an unedited
 *  track folds to `{}`. */
export function foldEdit(base: TrackEdit | null, ops: readonly EditOp[]): TrackEdit {
  let keep = base?.keep ? ([...base.keep] as [number, number]) : undefined;
  let remove = base?.remove ? base.remove.map((r) => [...r] as [number, number]) : [];
  let drop = base?.drop ? [...base.drop] : [];
  let move: Record<string, [number, number]> = { ...base?.move };
  for (const op of ops) {
    switch (op.kind) {
      case 'reset':
        keep = undefined;
        remove = [];
        drop = [];
        move = {};
        break;
      case 'chop':
        keep = keep ? [Math.max(keep[0], op.keep[0]), Math.min(keep[1], op.keep[1])] : op.keep;
        break;
      case 'cut':
        remove.push(op.remove);
        break;
      case 'drop':
        drop.push(op.t);
        break;
      case 'move':
        // A point dragged twice ends where it was dropped last.
        move[String(op.t)] = op.to;
        break;
    }
  }
  return {
    ...(keep ? { keep } : {}),
    ...(remove.length > 0 ? { remove } : {}),
    ...(drop.length > 0 ? { drop } : {}),
    ...(Object.keys(move).length > 0 ? { move } : {}),
  };
}

export function isEmptyEdit(edit: TrackEdit): boolean {
  return !edit.keep && !edit.remove?.length && !edit.drop?.length && Object.keys(edit.move ?? {}).length === 0;
}

/** The points that survive `edit`, in order, moved where it moves them — the same as the
 *  server's `TrackEdit.Apply`, so what the editor shows is exactly what Apply will produce. */
export function applyEdit(points: readonly TrackPoint[], edit: TrackEdit): TrackPoint[] {
  if (isEmptyEdit(edit)) return [...points];
  const drop = new Set(edit.drop ?? []);
  const remove = edit.remove ?? [];
  const move = edit.move ?? {};
  return points
    .filter(([, , t]) => {
      if (edit.keep && (t < edit.keep[0] || t > edit.keep[1])) return false;
      if (drop.has(t)) return false;
      return !remove.some(([a, b]) => t >= a && t <= b);
    })
    .map((p) => {
      const to = move[String(p[2])];
      return to ? [to[0], to[1], p[2]] : p;
    });
}

/** The op a Move point drag commits: the point at `t` dropped at `lon`/`lat`, rounded to the
 *  1e-6° (~0.1 m) the server sends points in. */
export function moveOp(t: number, lon: number, lat: number): EditOp {
  const round = (v: number) => Math.round(v * 1e6) / 1e6;
  return { kind: 'move', t, to: [round(lon), round(lat)] };
}

/** The op a Chop press commits for knobs on `visible[lo]` and `visible[hi]`, or null when the
 *  knobs sit on both ends and there's nothing to chop. */
export function chopOp(visible: readonly TrackPoint[], lo: number, hi: number): EditOp | null {
  if (lo <= 0 && hi >= visible.length - 1) return null;
  if (hi - lo < 1) return null; // a track needs two points to stay a track
  return { kind: 'chop', keep: [visible[lo]![2], visible[hi]![2]] };
}

/** The op a Cut press commits: removes every point strictly between the knobs, so the two
 *  knob points survive and are joined. Null when there's nothing between them. */
export function cutOp(visible: readonly TrackPoint[], lo: number, hi: number): EditOp | null {
  if (hi - lo < 2) return null;
  return { kind: 'cut', remove: [visible[lo + 1]![2], visible[hi - 1]![2]] };
}

/** Cumulative walked distance in metres at each point — the slider's readout. */
export function cumulativeDistances(points: readonly TrackPoint[]): number[] {
  const out = new Array<number>(points.length);
  let acc = 0;
  for (let i = 0; i < points.length; i++) {
    if (i > 0) acc += haversineM(points[i - 1]!, points[i]!);
    out[i] = acc;
  }
  return out;
}

const EARTH_RADIUS_M = 6371008.8;

function haversineM(a: TrackPoint, b: TrackPoint): number {
  const toRad = Math.PI / 180;
  const dLat = (b[1] - a[1]) * toRad;
  const dLon = (b[0] - a[0]) * toRad;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a[1] * toRad) * Math.cos(b[1] * toRad) * Math.sin(dLon / 2) ** 2;
  return 2 * EARTH_RADIUS_M * Math.asin(Math.min(1, Math.sqrt(h)));
}
