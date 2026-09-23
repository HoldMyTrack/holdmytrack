import { useCallback, useEffect, useMemo, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { getActivityTrackPoints, saveActivityTrackEdit, type Activity, type TrackEdit, type TrackPoint } from '../api';
import { labelInsertionPoint } from '../map/layers';
import { clearTrackEdit, ensureTrackEditLayer, onTrackEditPointClick, setTrackEditData, type EditPreview } from '../map/trackEdit';
import { applyEdit, chopOp, cumulativeDistances, cutOp, foldEdit, isEmptyEdit, type EditOp } from './editTrackOps';
import { distanceValue, formatActivityType, formatStartedAt, unitLabel } from './format';
import { useUnitSystem } from './units';

/**
 * The Edit track window (IMPLEMENTATION.md §4.7.7) — floats over the map while one activity's
 * recorded points are being edited. MapView owns entering and leaving the session (hiding the
 * other tracks, flying to this one); this component owns the session itself.
 *
 * The session is `base` (the edit the track already had) plus a stack of ops (editTrackOps.ts).
 * Everything drawn is a replay of that stack over the original points: Undo pops one op,
 * Cancel throws the stack away and closes, Apply folds it into one edit and sends it. The
 * knobs are a live preview only — nothing is committed until Chop, Cut, or a Delete point
 * click pushes an op, and every op resets the knobs to the new track's ends.
 */
export interface EditTrackPanelProps {
  map: MapLibreMap;
  activity: Activity;
  /** Leaves the session. `applied` is true when an edit was sent, so MapView reloads the list
   *  (the row now reads Pending) rather than just restoring the map. */
  onClose: (applied: boolean) => void;
}

interface Session {
  points: TrackPoint[];
  base: TrackEdit | null;
}

/** Knobs as point timestamps rather than indices, so a Delete point click that shifts every
 *  later index leaves the knobs on the same points. Null means "at that end". */
interface Knobs {
  lo: number | null;
  hi: number | null;
}

const ENDS: Knobs = { lo: null, hi: null };

export function EditTrackPanel({ map, activity, onClose }: EditTrackPanelProps) {
  const system = useUnitSystem();
  const [session, setSession] = useState<Session | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [ops, setOps] = useState<EditOp[]>([]);
  const [knobs, setKnobs] = useState<Knobs>(ENDS);
  const [deleteMode, setDeleteMode] = useState(false);
  const [preview, setPreview] = useState<EditPreview>('chop');
  const [applying, setApplying] = useState(false);
  const [applyError, setApplyError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    getActivityTrackPoints(activity.id, controller.signal)
      .then((res) => setSession({ points: res.points, base: res.edit }))
      .catch((err: unknown) => {
        if (!controller.signal.aborted) setLoadError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, [activity.id]);

  const edit = useMemo(() => (session ? foldEdit(session.base, ops) : {}), [session, ops]);
  const visible = useMemo(() => (session ? applyEdit(session.points, edit) : []), [session, edit]);
  const distances = useMemo(() => cumulativeDistances(visible), [visible]);
  const last = visible.length - 1;

  // Timestamps back to indices into what's visible now: the first point at or after lo, the
  // last at or before hi.
  const lo = knobs.lo === null ? 0 : Math.max(0, visible.findIndex(([, , t]) => t >= knobs.lo!));
  const hiFound = knobs.hi === null ? last : findLastIndex(visible, ([, , t]) => t <= knobs.hi!);
  const hi = Math.max(lo, hiFound < 0 ? last : hiFound);

  const push = useCallback((op: EditOp | null) => {
    if (!op) return;
    setOps((prev) => [...prev, op]);
    setApplyError(null);
  }, []);

  const chop = chopOp(visible, lo, hi);
  const cut = cutOp(visible, lo, hi);
  const canReset = session !== null && !isEmptyEdit(edit);

  const undo = useCallback(() => {
    setOps((prev) => prev.slice(0, -1));
    setKnobs(ENDS);
    setApplyError(null);
  }, []);

  // Cmd/Ctrl+Z steps back one op, the same as the Undo button — the one shortcut an editor
  // is expected to have.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && !event.shiftKey && event.key.toLowerCase() === 'z' && ops.length > 0) {
        event.preventDefault();
        undo();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [ops.length, undo]);

  // Draw, and redraw after a theme swap discards custom layers (styledata) — this overlay is
  // owned here, not by MapView's reattachOverlays, since it only exists during a session.
  useEffect(() => {
    const draw = () => {
      ensureTrackEditLayer(map, labelInsertionPoint(map));
      if (visible.length > 0) setTrackEditData(map, visible, lo, hi, preview);
    };
    draw();
    map.on('styledata', draw);
    return () => {
      map.off('styledata', draw);
    };
  }, [map, visible, lo, hi, preview]);
  useEffect(() => () => clearTrackEdit(map), [map]);

  // Delete point mode: a click on a point drops it. Never below two points — a track needs
  // two to stay a track.
  useEffect(() => {
    if (!deleteMode) return;
    return onTrackEditPointClick(map, (t) => {
      if (visible.length > 2) push({ kind: 'drop', t });
    });
  }, [map, deleteMode, visible.length, push]);

  async function apply() {
    setApplying(true);
    setApplyError(null);
    try {
      await saveActivityTrackEdit(activity.id, isEmptyEdit(edit) ? null : edit);
      onClose(true);
    } catch (err) {
      setApplyError(err instanceof Error ? err.message : String(err));
      setApplying(false);
    }
  }

  const label = activity.name?.trim() || formatStartedAt(activity.startedAt);
  const unit = unitLabel(system);

  return (
    <section className="edit-track" aria-label="Edit track" data-testid="edit-track">
      <header className="edit-track__head">
        <span className="edit-track__title">Edit track</span>
        <span className="edit-track__subtitle">
          {label} · {formatActivityType(activity.activityType)}
        </span>
      </header>

      {loadError && <p className="edit-track__error">{loadError}</p>}
      {!session && !loadError && <p className="edit-track__note">Loading points…</p>}

      {session && visible.length >= 2 && (
        <>
          <div className="activity-filters__section">
            <div className="activity-filters__head">
              <span className="activity-filters__label">Range</span>
              <span className="activity-filters__readout" data-testid="edit-track-readout">
                {distanceValue(distances[lo]!, system)} – {distanceValue(distances[hi]!, system)} {unit} ·{' '}
                {clockTime(visible[lo]![2])}–{clockTime(visible[hi]![2])}
              </span>
            </div>
            <div className="activity-filters__track-wrap">
              <div className="activity-filters__track" />
              <div
                className="activity-filters__fill"
                style={{ left: `${(lo / last) * 100}%`, right: `${100 - (hi / last) * 100}%` }}
              />
              {/* Point index, not distance: a stationary stretch (the mall, the sofa) is many
                  points over almost no distance, and a distance axis would squash exactly the
                  part being cut into a single pixel. */}
              <input
                type="range"
                className="activity-filters__range activity-filters__range--min"
                aria-label="Start of range"
                min={0}
                max={last}
                value={lo}
                onChange={(e) => setKnobs((k) => ({ ...k, lo: visible[Math.min(Number(e.target.value), hi)]![2] }))}
              />
              <input
                type="range"
                className="activity-filters__range activity-filters__range--max"
                aria-label="End of range"
                min={0}
                max={last}
                value={hi}
                onChange={(e) => setKnobs((k) => ({ ...k, hi: visible[Math.max(Number(e.target.value), lo)]![2] }))}
              />
            </div>
            <div className="activity-filters__bounds">
              <span>0.0 {unit}</span>
              <span>{visible.length.toLocaleString()} points</span>
              <span>
                {distanceValue(distances[last]!, system)} {unit}
              </span>
            </div>
          </div>

          <div className="edit-track__row">
            <button
              type="button"
              className="edit-track__btn"
              disabled={!chop}
              onClick={() => {
                push(chop);
                setKnobs(ENDS);
              }}
              onMouseEnter={() => setPreview('chop')}
              onFocus={() => setPreview('chop')}
              title="Keep only the part between the knobs"
            >
              Chop
            </button>
            <button
              type="button"
              className="edit-track__btn"
              disabled={!cut}
              onClick={() => {
                push(cut);
                setKnobs(ENDS);
                // The pointer is still over this (now disabled) button, and a disabled button
                // may never see its mouseleave — don't leave a Cut preview up over the ends.
                setPreview('chop');
              }}
              onMouseEnter={() => setPreview('cut')}
              onMouseLeave={() => setPreview('chop')}
              onFocus={() => setPreview('cut')}
              onBlur={() => setPreview('chop')}
              title="Remove the part between the knobs and join its two ends"
            >
              Cut
            </button>
            <button
              type="button"
              className="edit-track__btn"
              aria-pressed={deleteMode}
              onClick={() => setDeleteMode((on) => !on)}
              title="Click points on the map to delete them"
            >
              Delete point
            </button>
          </div>
          {deleteMode && <p className="edit-track__note">Click a point on the map to delete it.</p>}

          <div className="edit-track__row edit-track__row--footer">
            <button type="button" className="edit-track__btn" disabled={ops.length === 0 || applying} onClick={undo} title="Undo the last change (⌘Z)">
              Undo
            </button>
            {canReset && (
              <button
                type="button"
                className="edit-track__btn edit-track__btn--quiet"
                disabled={applying}
                onClick={() => {
                  push({ kind: 'reset' });
                  setKnobs(ENDS);
                }}
                title="Go back to the track as it was recorded (Undo brings the edits back)"
              >
                Reset
              </button>
            )}
            <span className="edit-track__spacer" aria-hidden="true" />
            <button type="button" className="edit-track__btn" disabled={applying} onClick={() => onClose(false)}>
              Cancel
            </button>
            <button
              type="button"
              className="edit-track__btn edit-track__btn--primary"
              disabled={ops.length === 0 || applying}
              onClick={() => void apply()}
            >
              {applying ? 'Applying…' : 'Apply'}
            </button>
          </div>
          {applyError && <p className="edit-track__error">{applyError}</p>}
        </>
      )}

      {(loadError || (!session && !loadError)) && (
        <div className="edit-track__row edit-track__row--footer">
          <span className="edit-track__spacer" aria-hidden="true" />
          <button type="button" className="edit-track__btn" onClick={() => onClose(false)}>
            Cancel
          </button>
        </div>
      )}
    </section>
  );
}

function findLastIndex<T>(items: readonly T[], pred: (item: T) => boolean): number {
  for (let i = items.length - 1; i >= 0; i--) if (pred(items[i]!)) return i;
  return -1;
}

function clockTime(ms: number): string {
  return new Date(ms).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}
