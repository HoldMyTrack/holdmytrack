import { useCallback, useEffect, useMemo, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { getActivityTrackPoints, type Activity, type TrackEdit, type TrackPoint } from '../api';
import { labelInsertionPoint } from '../map/layers';
import { clearTrackEdit, ensureTrackEditLayer, onTrackEditPointClick, setTrackEditData, type EditPreview } from '../map/trackEdit';
import { applyEdit, chopOp, cumulativeDistances, cutOp, foldEdit, isEmptyEdit, type EditOp } from './editTrackOps';
import { distanceValue, unitLabel } from './format';
import { useUnitSystem } from './units';
import { lang, t, tn } from '../i18n';

/**
 * The Edit window's Track tab (IMPLEMENTATION.md §4.7.7) — one activity's recorded points,
 * edited on the map. MapView owns entering and leaving the session (hiding the other tracks,
 * flying to this one); EditActivityWindow owns saving it, alongside the Activity tab's fields,
 * behind the window's shared Save/Cancel. This component owns the session itself.
 *
 * The session is `base` (the edit the track already had) plus a stack of ops (editTrackOps.ts).
 * Everything drawn is a replay of that stack over the original points: Undo pops one op,
 * Cancel throws the stack away, Save folds it into one edit and sends it. The knobs are a live
 * preview only — nothing is committed until Chop, Cut, or a Delete point click pushes an op,
 * and every op resets the knobs to the new track's ends.
 *
 * Stays mounted while the Activity tab is showing, so switching tabs loses nothing; `active`
 * is false then, which turns off Delete point mode (a map click there would otherwise drop a
 * point the user can't see being edited) and Cmd/Ctrl+Z (which belongs to the text fields).
 */
export interface TrackEditorProps {
  map: MapLibreMap;
  activity: Activity;
  /** Whether the Track tab is the one showing. */
  active: boolean;
  /** A save is in flight — the editing controls wait for it. */
  busy: boolean;
  /** Reports the session on every change: null while nothing has been done (Save leaves the
   *  track alone), otherwise the edit Save sends — itself null when the ops undo every edit
   *  the track had, which clears it. */
  onChange: (pending: { edit: TrackEdit | null } | null) => void;
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

export function TrackEditor({ map, activity, active, busy, onChange }: TrackEditorProps) {
  const system = useUnitSystem();
  const [session, setSession] = useState<Session | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [ops, setOps] = useState<EditOp[]>([]);
  const [knobs, setKnobs] = useState<Knobs>(ENDS);
  const [deleteMode, setDeleteMode] = useState(false);
  const [preview, setPreview] = useState<EditPreview>('chop');

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

  useEffect(() => {
    onChange(ops.length === 0 ? null : { edit: isEmptyEdit(edit) ? null : edit });
  }, [ops.length, edit, onChange]);

  useEffect(() => {
    if (!active) setDeleteMode(false);
  }, [active]);

  // Timestamps back to indices into what's visible now: the first point at or after lo, the
  // last at or before hi.
  const lo = knobs.lo === null ? 0 : Math.max(0, visible.findIndex(([, , t]) => t >= knobs.lo!));
  const hiFound = knobs.hi === null ? last : findLastIndex(visible, ([, , t]) => t <= knobs.hi!);
  const hi = Math.max(lo, hiFound < 0 ? last : hiFound);

  const push = useCallback((op: EditOp | null) => {
    if (!op) return;
    setOps((prev) => [...prev, op]);
  }, []);

  const chop = chopOp(visible, lo, hi);
  const cut = cutOp(visible, lo, hi);
  const canReset = session !== null && !isEmptyEdit(edit);

  const undo = useCallback(() => {
    setOps((prev) => prev.slice(0, -1));
    setKnobs(ENDS);
  }, []);

  // Cmd/Ctrl+Z steps back one op, the same as the Undo button — the one shortcut an editor
  // is expected to have. Only while this tab shows: on the Activity tab it's the text fields'.
  useEffect(() => {
    if (!active || busy) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && !event.shiftKey && event.key.toLowerCase() === 'z' && ops.length > 0) {
        event.preventDefault();
        undo();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [active, busy, ops.length, undo]);

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
    if (!deleteMode || busy) return;
    return onTrackEditPointClick(map, (t) => {
      if (visible.length > 2) push({ kind: 'drop', t });
    });
  }, [map, deleteMode, busy, visible.length, push]);

  const unit = unitLabel(system);

  if (loadError) return <p className="edit-track__error">{loadError}</p>;
  if (!session) return <p className="edit-track__note">{t('edit_track.loading')}</p>;
  if (visible.length < 2) return null;

  return (
    <>
      <div className="activity-filters__section">
        <div className="activity-filters__head">
          <span className="activity-filters__label">{t('edit_track.range')}</span>
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
            aria-label={t('edit_track.range_start')}
            min={0}
            max={last}
            value={lo}
            disabled={busy}
            onChange={(e) => setKnobs((k) => ({ ...k, lo: visible[Math.min(Number(e.target.value), hi)]![2] }))}
          />
          <input
            type="range"
            className="activity-filters__range activity-filters__range--max"
            aria-label={t('edit_track.range_end')}
            min={0}
            max={last}
            value={hi}
            disabled={busy}
            onChange={(e) => setKnobs((k) => ({ ...k, hi: visible[Math.max(Number(e.target.value), lo)]![2] }))}
          />
        </div>
        <div className="activity-filters__bounds">
          <span>{distanceValue(0, system)} {unit}</span>
          <span>{tn('edit_track.points', visible.length)}</span>
          <span>
            {distanceValue(distances[last]!, system)} {unit}
          </span>
        </div>
      </div>

      <div className="edit-track__row">
        <button
          type="button"
          className="edit-track__btn"
          disabled={!chop || busy}
          onClick={() => {
            push(chop);
            setKnobs(ENDS);
          }}
          onMouseEnter={() => setPreview('chop')}
          onFocus={() => setPreview('chop')}
          title={t('edit_track.chop_title')}
        >
          {t('edit_track.chop')}
        </button>
        <button
          type="button"
          className="edit-track__btn"
          disabled={!cut || busy}
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
          title={t('edit_track.cut_title')}
        >
          {t('edit_track.cut')}
        </button>
        <button
          type="button"
          className="edit-track__btn"
          aria-pressed={deleteMode}
          disabled={busy}
          onClick={() => setDeleteMode((on) => !on)}
          title={t('edit_track.delete_point_title')}
        >
          {t('edit_track.delete_point')}
        </button>
        <span className="edit-track__spacer" aria-hidden="true" />
        <button type="button" className="edit-track__btn" disabled={ops.length === 0 || busy} onClick={undo} title={t('edit_track.undo_title')}>
          {t('edit_track.undo')}
        </button>
        {canReset && (
          <button
            type="button"
            className="edit-track__btn edit-track__btn--quiet"
            disabled={busy}
            onClick={() => {
              push({ kind: 'reset' });
              setKnobs(ENDS);
            }}
            title={t('edit_track.reset_title')}
          >
            {t('edit_track.reset')}
          </button>
        )}
      </div>
      {deleteMode && <p className="edit-track__note">{t('edit_track.delete_point_note')}</p>}
    </>
  );
}

function findLastIndex<T>(items: readonly T[], pred: (item: T) => boolean): number {
  for (let i = items.length - 1; i >= 0; i--) if (pred(items[i]!)) return i;
  return -1;
}

function clockTime(ms: number): string {
  return new Date(ms).toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit' });
}
