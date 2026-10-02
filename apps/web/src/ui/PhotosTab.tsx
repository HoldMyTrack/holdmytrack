import { useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from 'react';
import { ImagePlus, Pencil, Trash2, Undo2 } from 'lucide-react';
import { API_BASE_URL, checkPhotoPlace, getActivityTrackMetrics, PhotoNeedsPlaceError, type Activity, type Photo } from '../api';
import { lang, t } from '../i18n';
import type { PhotoMarkerItem, PhotoMarkerOverlay } from '../map/photos';
import { draftRows, waitingPhotos, type NewPhoto, type PhotoDraft } from './photoDraft';
import { fractionAt, photoTrack, pointAt, startFraction, type PhotoTrack, type PlaceAnchor } from './photoTrack';
import { preparePhoto, UnreadablePhotoError } from './photoPrep';

/** Mirrors photos.go's maxPhotoCaptionLen. */
export const MAX_PHOTO_CAPTION_LEN = 500;
/** The slider's steps — fine enough that a step on a long route is a few metres. */
const SLIDER_STEPS = 1000;

export interface PhotosTabProps {
  activity: Activity;
  /** The saved photos, as the server has them. */
  photos: readonly Photo[];
  error: string | null;
  /** The unsaved changes, owned by the Edit window, whose Save writes them. */
  draft: PhotoDraft;
  setDraft: Dispatch<SetStateAction<PhotoDraft>>;
  /** Whether this tab is the one showing — only then does it draw its changes on the map. */
  active: boolean;
  /** The window is saving: nothing here can change meanwhile. */
  busy: boolean;
  /** Add is preparing picked photos — the window holds its Save and Cancel until it's done. */
  onPreparingChange: (preparing: boolean) => void;
  /** How the map should show the draft: photos moved, added or deleted, and the one in hand. */
  onOverlay: (overlay: PhotoMarkerOverlay | null) => void;
}

function timeOfDay(seconds: number): string {
  return new Date(seconds * 1000).toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit' });
}

/**
 * The Edit window's Photos tab (FR-16.6): the activity's photos as a list, each with Edit and
 * Delete, and Add photos. Nothing here is written until the window's Save; Cancel throws it all
 * away. The tab edits `draft` (photoDraft.ts), and the list and the map show the photos as Save
 * would leave them.
 *
 * Every photo has a place on the track. Add prepares each picked file (photoPrep.ts) and asks
 * the server where it goes (`checkPhotoPlace`, which stores nothing); one it can't place waits at
 * the top of the list with a slider, starting just after the photo picked before it, until the
 * user puts it somewhere (Place here) or removes it. Edit opens the same slider on a photo, with
 * its caption. The slider walks the track as drawn (photoTrack.ts) and the photo moves along the
 * map with it; a place is kept as the moment at that point.
 */
export function PhotosTab({ activity, photos, error, draft, setDraft, active, busy, onPreparingChange, onOverlay }: PhotosTabProps) {
  const input = useRef<HTMLInputElement>(null);
  const [track, setTrack] = useState<PhotoTrack | null>(null);
  const [trackError, setTrackError] = useState<string | null>(null);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [failures, setFailures] = useState<string[]>([]);
  const [editingId, setEditingId] = useState<string | null>(null);
  // The waiting photo's slider: which photo it's for, and where it is.
  const [waitingAt, setWaitingAt] = useState<{ key: string; fraction: number } | null>(null);
  // What each waiting photo was picked after, and which ones were removed rather than placed —
  // what a waiting photo's slider starts from (startFraction).
  const anchors = useRef(new Map<string, PlaceAnchor>());
  const removed = useRef(new Set<string>());
  // Cleared on unmount, so an Add still running stops instead of preparing photos into a draft
  // nothing will save or free.
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    getActivityTrackMetrics(activity.id, controller.signal)
      .then((m) => {
        if (m.points.length === 0) throw new Error(t('photos.no_track'));
        setTrack(photoTrack(m.points.map((p) => ({ lon: p.lon, lat: p.lat, t: p.timeS }))));
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) setTrackError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, [activity.id]);

  const sorted = useMemo(() => draftRows(photos, draft), [photos, draft]);
  // The list keeps its order while a row is open — re-sorting under a slider being dragged would
  // move the row out from under the pointer — and takes up its new place when the row closes.
  const frozenOrder = useRef<string[]>([]);
  if (editingId === null) frozenOrder.current = sorted.map((r) => r.id);
  const rows = useMemo(() => {
    if (editingId === null) return sorted;
    const byId = new Map(sorted.map((r) => [r.id, r]));
    const kept = frozenOrder.current.flatMap((id) => (byId.has(id) ? [byId.get(id)!] : []));
    return [...kept, ...sorted.filter((r) => !frozenOrder.current.includes(r.id))];
  }, [sorted, editingId]);
  const waitingList = waitingPhotos(draft);
  const waiting = waitingList[0] ?? null;

  // A waiting photo's slider starts just after the photo picked before it — worked out when its
  // turn comes, since the one before may only just have been placed.
  useEffect(() => {
    if (!waiting || !track || waitingAt?.key === waiting.key) return;
    const placed = new Map<string, number | null>();
    for (const p of draft.added) if (p.routeAt !== null) placed.set(p.key, fractionAt(track, p.routeAt));
    for (const key of removed.current) placed.set(key, null);
    setWaitingAt({ key: waiting.key, fraction: startFraction(track, anchors.current.get(waiting.key) ?? null, placed, anchors.current) });
  }, [waiting, track, waitingAt, draft.added]);

  // The map shows the photos as Save would leave them.
  useEffect(() => {
    if (!active || !track) {
      onOverlay(null);
      return;
    }
    const at = (routeAt: number) => pointAt(track, fractionAt(track, routeAt));
    const upserts: PhotoMarkerItem[] = [];
    for (const row of rows) {
      if (row.kind === 'saved' && (row.deleted || draft.changed[row.id] === undefined)) continue;
      const p = at(row.routeAt);
      const thumbSrc = row.kind === 'new' ? row.photo.thumbSrc : API_BASE_URL + row.photo.thumbUrl;
      upserts.push({ id: row.id, lon: p.lon, lat: p.lat, thumbSrc, caption: row.caption || null });
    }
    if (waiting && waitingAt?.key === waiting.key) {
      const p = pointAt(track, waitingAt.fraction);
      upserts.push({ id: waiting.key, lon: p.lon, lat: p.lat, thumbSrc: waiting.thumbSrc, caption: null });
    }
    onOverlay({ upserts, hidden: [...draft.deleted], activeId: waiting?.key ?? editingId });
  }, [active, track, rows, draft, waiting, waitingAt, editingId, onOverlay]);
  useEffect(() => () => onOverlay(null), [onOverlay]);

  async function add(files: File[]) {
    const failed: string[] = [];
    setFailures([]);
    // The photo picked before this one, for where a waiting one's slider starts.
    let previous: PlaceAnchor = null;
    for (let i = 0; i < files.length; i++) {
      if (!mounted.current) return;
      const file = files[i]!;
      setProgress({ done: i, total: files.length });
      try {
        const prepared = await preparePhoto(file);
        let routeAt: number | null = null;
        const key = `new-${Date.now()}-${i}`;
        try {
          routeAt = Date.parse((await checkPhotoPlace(activity.id, prepared.exif)).routeAt) / 1000;
        } catch (err) {
          if (!(err instanceof PhotoNeedsPlaceError)) throw err;
          anchors.current.set(key, previous);
        }
        if (!mounted.current) return;
        const entry: NewPhoto = { key, name: file.name, prepared, thumbSrc: URL.createObjectURL(prepared.thumb), routeAt, caption: '' };
        previous = routeAt !== null ? { t: routeAt } : { waiting: key };
        setDraft((d) => ({ ...d, added: [...d.added, entry] }));
      } catch (err) {
        const message = err instanceof UnreadablePhotoError ? t('photos.unreadable') : err instanceof Error ? err.message : String(err);
        failed.push(t('photos.upload_failed', { name: file.name, message }));
        setFailures([...failed]);
      }
    }
    setProgress(null);
  }

  function updateNew(key: string, change: Partial<NewPhoto>) {
    setDraft((d) => ({ ...d, added: d.added.map((p) => (p.key === key ? { ...p, ...change } : p)) }));
  }

  function removeNew(photo: NewPhoto) {
    removed.current.add(photo.key);
    URL.revokeObjectURL(photo.thumbSrc);
    setDraft((d) => ({ ...d, added: d.added.filter((p) => p.key !== photo.key) }));
    if (editingId === photo.key) setEditingId(null);
  }

  function changeSaved(photo: Photo, change: { routeAt?: number; caption?: string }) {
    setDraft((d) => {
      const next = { ...(d.changed[photo.id] ?? {}), ...change };
      // A change back to what's saved is no change.
      if (next.routeAt !== undefined && next.routeAt === Date.parse(photo.routeAt) / 1000) delete next.routeAt;
      if (next.caption !== undefined && next.caption === (photo.caption ?? '')) delete next.caption;
      const changed = { ...d.changed };
      if (Object.keys(next).length === 0) delete changed[photo.id];
      else changed[photo.id] = next;
      return { ...d, changed };
    });
  }

  function setDeleted(id: string, deleted: boolean) {
    setDraft((d) => ({ ...d, deleted: deleted ? [...d.deleted, id] : d.deleted.filter((x) => x !== id) }));
    if (deleted && editingId === id) setEditingId(null);
  }

  function slider(fraction: number, onChange: (fraction: number) => void) {
    const at = track ? pointAt(track, fraction) : null;
    return (
      <label className="photos-tab__slider">
        <span className="photos-tab__slider-label">
          {t('photos.slider')}
          {at && <span className="photos-tab__slider-time">{timeOfDay(at.t)}</span>}
        </span>
        <input
          type="range"
          min={0}
          max={SLIDER_STEPS}
          value={Math.round(fraction * SLIDER_STEPS)}
          disabled={!track || busy}
          onChange={(event) => onChange(Number(event.target.value) / SLIDER_STEPS)}
          data-testid="photo-slider"
        />
      </label>
    );
  }

  // The row being edited, scrolled into the list's view as it opens.
  const editRow = useRef<HTMLLIElement>(null);
  useEffect(() => {
    if (editingId !== null) editRow.current?.scrollIntoView({ block: 'nearest' });
  }, [editingId]);

  const picking = progress !== null;
  useEffect(() => {
    onPreparingChange(picking);
    return () => onPreparingChange(false);
  }, [picking, onPreparingChange]);

  return (
    <div className="photos-tab" data-testid="photos-tab">
      <div className="photos-tab__head">
        <button type="button" className="edit-track__btn" disabled={picking || busy} onClick={() => input.current?.click()} data-testid="photo-add">
          <ImagePlus size={16} aria-hidden="true" />
          {t('photos.add')}
        </button>
        <input
          ref={input}
          type="file"
          accept="image/*"
          multiple
          hidden
          onChange={(event) => {
            const files = Array.from(event.target.files ?? []);
            event.target.value = '';
            void add(files);
          }}
          data-testid="photo-input"
        />
        {picking && (
          <span className="photos-tab__status" role="status">
            {t('photos.preparing', { n: progress.done + 1, total: progress.total })}
          </span>
        )}
      </div>

      {(error || trackError) && <p className="edit-track__error">{error ?? trackError}</p>}
      {failures.length > 0 && (
        <ul className="photos-tab__failures" aria-live="polite">
          {failures.map((f, i) => (
            <li key={i}>{f}</li>
          ))}
        </ul>
      )}

      {waiting && (
        <div className="photos-tab__row photos-tab__row--editing photos-tab__row--waiting" data-testid="photo-waiting">
          <div className="photos-tab__summary">
            <img className="photos-tab__thumb" src={waiting.thumbSrc} alt="" />
            <span className="photos-tab__text">
              <span className="photos-tab__title">{waiting.name}</span>
              <span className="photos-tab__meta">{t('photos.needs_place')}</span>
            </span>
          </div>
          {waitingAt?.key === waiting.key && slider(waitingAt.fraction, (fraction) => setWaitingAt({ key: waiting.key, fraction }))}
          <div className="photos-tab__actions">
            {waitingList.length > 1 && <span className="photos-tab__meta">{t('photos.more_waiting', { n: waitingList.length - 1 })}</span>}
            <span className="edit-track__spacer" aria-hidden="true" />
            <button type="button" className="edit-track__btn" disabled={busy} onClick={() => removeNew(waiting)}>
              {t('photos.remove')}
            </button>
            <button
              type="button"
              className="edit-track__btn edit-track__btn--primary"
              disabled={busy || !track || waitingAt?.key !== waiting.key}
              onClick={() => track && waitingAt && updateNew(waiting.key, { routeAt: pointAt(track, waitingAt.fraction).t })}
              data-testid="photo-place-here"
            >
              {t('photos.place_here')}
            </button>
          </div>
        </div>
      )}

      {rows.length === 0 && !waiting && !error && <p className="photos-tab__note">{t('photos.empty')}</p>}
      <ul className="photos-tab__list">
        {rows.map((row, i) => {
          const isEditing = editingId === row.id;
          const deleted = row.kind === 'saved' && row.deleted;
          const thumbSrc = row.kind === 'new' ? row.photo.thumbSrc : API_BASE_URL + row.photo.thumbUrl;
          const changed = row.kind === 'saved' && draft.changed[row.id] !== undefined;
          const classes = ['photos-tab__row'];
          if (isEditing) classes.push('photos-tab__row--editing');
          if (deleted) classes.push('photos-tab__row--deleted');
          return (
            <li key={row.id} ref={isEditing ? editRow : undefined} className={classes.join(' ')} data-testid="photo-row">
              <div className="photos-tab__summary">
                <img className="photos-tab__thumb" src={thumbSrc} alt="" loading="lazy" />
                <span className="photos-tab__text">
                  <span className="photos-tab__title">{row.caption || t('photos.photo_n', { n: i + 1 })}</span>
                  <span className="photos-tab__meta">
                    {timeOfDay(row.routeAt)}
                    {row.kind === 'new' && <span className="photos-tab__badge">{t('photos.new')}</span>}
                    {changed && !deleted && <span className="photos-tab__badge">{t('photos.changed')}</span>}
                    {deleted && <span className="photos-tab__badge photos-tab__badge--deleted">{t('photos.deleted')}</span>}
                  </span>
                </span>
                {deleted ? (
                  <button
                    type="button"
                    className="photos-tab__icon"
                    aria-label={t('photos.undo_delete')}
                    title={t('photos.undo_delete')}
                    disabled={busy}
                    onClick={() => setDeleted(row.id, false)}
                    data-testid="photo-undo"
                  >
                    <Undo2 size={16} aria-hidden="true" />
                  </button>
                ) : (
                  <>
                    <button
                      type="button"
                      className="photos-tab__icon"
                      aria-label={t('photos.edit')}
                      title={t('photos.edit')}
                      aria-pressed={isEditing}
                      disabled={!track || busy}
                      onClick={() => setEditingId(isEditing ? null : row.id)}
                      data-testid="photo-edit"
                    >
                      <Pencil size={16} aria-hidden="true" />
                    </button>
                    <button
                      type="button"
                      className="photos-tab__icon photos-tab__icon--danger"
                      aria-label={t('common.delete')}
                      title={t('common.delete')}
                      disabled={busy}
                      onClick={() => (row.kind === 'new' ? removeNew(row.photo) : setDeleted(row.id, true))}
                      data-testid="photo-delete"
                    >
                      <Trash2 size={16} aria-hidden="true" />
                    </button>
                  </>
                )}
              </div>
              {isEditing && track && (
                <>
                  {slider(fractionAt(track, row.routeAt), (fraction) => {
                    const routeAt = pointAt(track, fraction).t;
                    if (row.kind === 'new') updateNew(row.id, { routeAt });
                    else changeSaved(row.photo, { routeAt });
                  })}
                  <input
                    className="settings-page__input"
                    type="text"
                    value={row.caption}
                    maxLength={MAX_PHOTO_CAPTION_LEN}
                    placeholder={t('photos.caption_placeholder')}
                    aria-label={t('photos.caption')}
                    disabled={busy}
                    onChange={(event) => {
                      if (row.kind === 'new') updateNew(row.id, { caption: event.target.value });
                      else changeSaved(row.photo, { caption: event.target.value });
                    }}
                    data-testid="photo-caption"
                  />
                  <div className="photos-tab__actions">
                    <span className="edit-track__spacer" aria-hidden="true" />
                    <button type="button" className="edit-track__btn" onClick={() => setEditingId(null)} data-testid="photo-done">
                      {t('photos.done')}
                    </button>
                  </div>
                </>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
