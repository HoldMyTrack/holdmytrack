import { useEffect, useRef, useState } from 'react';
import { Check, ImagePlus, Pencil, Trash2, X } from 'lucide-react';
import {
  API_BASE_URL,
  deletePhoto,
  getActivityTrackMetrics,
  PhotoNeedsPlaceError,
  updatePhoto,
  uploadPhoto,
  type Activity,
  type Photo,
} from '../api';
import { lang, t } from '../i18n';
import type { PhotoMarkerItem } from '../map/photos';
import { ConfirmDialog } from './ConfirmDialog';
import { fractionAt, photoTrack, pointAt, startFraction, type PhotoTrack, type PlaceAnchor } from './photoTrack';
import { preparePhoto, UnreadablePhotoError, type PreparedPhoto } from './photoPrep';

/** Mirrors photos.go's maxPhotoCaptionLen. */
const MAX_CAPTION_LEN = 500;
/** The slider's steps — fine enough that a step on a long route is a few metres. */
const SLIDER_STEPS = 1000;

export interface PhotosTabProps {
  activity: Activity;
  photos: readonly Photo[];
  error: string | null;
  /** Whether this tab is the one showing — only then does it draw a preview on the map. */
  active: boolean;
  /** Something was added, moved or deleted: refetch, for the server's order. */
  onChanged: () => void;
  onReplace: (photo: Photo) => void;
  onRemove: (id: string) => void;
  /** Where to draw the photo being moved or placed while its slider moves, or null for none. */
  onPreview: (preview: PhotoMarkerItem | null) => void;
}

/** A picked photo the server couldn't place (photo_needs_place), waiting for the user to. */
interface Unplaced {
  key: string;
  name: string;
  prepared: PreparedPhoto;
  thumbSrc: string;
  /** Null until it's the one with the slider out — its start depends on where the photo before
   *  it ended up, which may itself still be waiting. */
  fraction: number | null;
}

interface Editing {
  id: string;
  fraction: number;
  startFraction: number;
  caption: string;
}

function iso(seconds: number): string {
  return new Date(seconds * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z');
}

function timeOfDay(seconds: number): string {
  return new Date(seconds * 1000).toLocaleTimeString(lang, { hour: '2-digit', minute: '2-digit' });
}

/**
 * The Edit window's Photos tab (FR-16.6): the activity's photos as a list, each with Edit and
 * Delete, and Add photos. Changes here are saved as they're made, not by the window's Save.
 *
 * Every photo has a place on the track. Add prepares each picked file (photoPrep.ts) and uploads
 * it; the server places it by its capture time or position, and when it can't, the photo waits
 * at the top of the list with a slider until the user says where it was taken (Add here) or
 * drops it (Skip). Edit opens the same slider on a saved photo, along with its caption. The
 * slider walks the track as drawn (photoTrack.ts) and the photo moves along the map with it
 * (`onPreview`); a place is saved as the moment at that point.
 */
export function PhotosTab({ activity, photos, error, active, onChanged, onReplace, onRemove, onPreview }: PhotosTabProps) {
  const input = useRef<HTMLInputElement>(null);
  const [track, setTrack] = useState<PhotoTrack | null>(null);
  const [trackError, setTrackError] = useState<string | null>(null);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [failures, setFailures] = useState<string[]>([]);
  const [unplaced, setUnplaced] = useState<Unplaced[]>([]);
  const [editing, setEditing] = useState<Editing | null>(null);
  const [rowBusy, setRowBusy] = useState(false);
  const [rowError, setRowError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<Photo | null>(null);
  // What each waiting photo was picked after, and where each one that waited ended up (null:
  // skipped) — what a waiting photo's slider starts from (startFraction).
  const anchors = useRef(new Map<string, PlaceAnchor>());
  const placedAt = useRef(new Map<string, number | null>());
  // The row being edited, scrolled into the list's view as it opens — its slider and buttons
  // are below its summary, past the list's bottom edge for a photo near the end.
  const editRow = useRef<HTMLLIElement>(null);
  const editingId = editing?.id ?? null;
  useEffect(() => {
    if (editingId !== null) editRow.current?.scrollIntoView({ block: 'nearest' });
  }, [editingId]);

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

  // The waiting photos' thumbnails are object URLs; let them go with the tab.
  const unplacedRef = useRef(unplaced);
  unplacedRef.current = unplaced;
  useEffect(() => () => unplacedRef.current.forEach((u) => URL.revokeObjectURL(u.thumbSrc)), []);

  // The photo whose slider is out — the first waiting one, else the one being edited — moves on
  // the map with it.
  const waiting = unplaced[0] ?? null;
  // A waiting photo's slider starts just after the photo picked before it.
  useEffect(() => {
    if (!waiting || waiting.fraction !== null || !track) return;
    const fraction = startFraction(track, anchors.current.get(waiting.key) ?? null, placedAt.current, anchors.current);
    setUnplaced((list) => list.map((u) => (u.key === waiting.key ? { ...u, fraction } : u)));
  }, [waiting, track]);
  const editingPhoto = editing ? photos.find((p) => p.id === editing.id) ?? null : null;
  useEffect(() => {
    if (!active || !track) {
      onPreview(null);
      return;
    }
    if (waiting) {
      if (waiting.fraction === null) return;
      const at = pointAt(track, waiting.fraction);
      onPreview({ id: waiting.key, lon: at.lon, lat: at.lat, thumbSrc: waiting.thumbSrc, caption: null });
    } else if (editing && editingPhoto) {
      const at = pointAt(track, editing.fraction);
      onPreview({ id: editing.id, lon: at.lon, lat: at.lat, thumbSrc: API_BASE_URL + editingPhoto.thumbUrl, caption: editing.caption || null });
    } else {
      onPreview(null);
    }
  }, [active, track, waiting, editing, editingPhoto, onPreview]);
  useEffect(() => () => onPreview(null), [onPreview]);

  async function add(files: File[]) {
    const failed: string[] = [];
    setFailures([]);
    // The photo picked before this one, for where a waiting one's slider starts.
    let previous: PlaceAnchor = null;
    for (let i = 0; i < files.length; i++) {
      const file = files[i]!;
      setProgress({ done: i, total: files.length });
      let prepared: PreparedPhoto | null = null;
      try {
        prepared = await preparePhoto(file);
        const photo = await uploadPhoto(activity.id, prepared);
        previous = { t: Date.parse(photo.routeAt) / 1000 };
        onChanged();
      } catch (err) {
        if (err instanceof PhotoNeedsPlaceError && prepared) {
          const thumbSrc = URL.createObjectURL(prepared.thumb);
          const entry: Unplaced = { key: `unplaced-${Date.now()}-${i}`, name: file.name, prepared, thumbSrc, fraction: null };
          anchors.current.set(entry.key, previous);
          previous = { waiting: entry.key };
          setUnplaced((list) => [...list, entry]);
          continue;
        }
        const message = err instanceof UnreadablePhotoError ? t('photos.unreadable') : err instanceof Error ? err.message : String(err);
        failed.push(t('photos.upload_failed', { name: file.name, message }));
        setFailures([...failed]);
      }
    }
    setProgress(null);
  }

  function dropWaiting(entry: Unplaced, at: number | null = null) {
    placedAt.current.set(entry.key, at);
    URL.revokeObjectURL(entry.thumbSrc);
    setUnplaced((list) => list.filter((u) => u.key !== entry.key));
    setRowError(null);
  }

  async function placeWaiting(entry: Unplaced) {
    if (!track || entry.fraction === null) return;
    setRowBusy(true);
    setRowError(null);
    try {
      await uploadPhoto(activity.id, entry.prepared, iso(pointAt(track, entry.fraction).t));
      dropWaiting(entry, entry.fraction);
      onChanged();
    } catch (err) {
      setRowError(err instanceof Error ? err.message : String(err));
    } finally {
      setRowBusy(false);
    }
  }

  function startEdit(photo: Photo) {
    if (!track) return;
    const fraction = fractionAt(track, Date.parse(photo.routeAt) / 1000);
    setEditing({ id: photo.id, fraction, startFraction: fraction, caption: photo.caption ?? '' });
    setRowError(null);
  }

  async function saveEdit(photo: Photo) {
    if (!editing || !track) return;
    const caption = editing.caption.trim();
    if (caption.length > MAX_CAPTION_LEN) {
      setRowError(t('photos.caption_too_long', { max: MAX_CAPTION_LEN }));
      return;
    }
    const change: { caption?: string; routeAt?: string } = {};
    if (caption !== (photo.caption ?? '')) change.caption = caption;
    if (editing.fraction !== editing.startFraction) change.routeAt = iso(pointAt(track, editing.fraction).t);
    if (Object.keys(change).length === 0) {
      setEditing(null);
      return;
    }
    setRowBusy(true);
    setRowError(null);
    try {
      onReplace(await updatePhoto(photo.id, change));
      setEditing(null);
      if (change.routeAt) onChanged();
    } catch (err) {
      setRowError(err instanceof Error ? err.message : String(err));
    } finally {
      setRowBusy(false);
    }
  }

  function slider(fraction: number, onChange: (fraction: number) => void, label: string) {
    const at = track ? pointAt(track, fraction) : null;
    return (
      <label className="photos-tab__slider">
        <span className="photos-tab__slider-label">
          {label}
          {at && <span className="photos-tab__slider-time">{timeOfDay(at.t)}</span>}
        </span>
        <input
          type="range"
          min={0}
          max={SLIDER_STEPS}
          value={Math.round(fraction * SLIDER_STEPS)}
          disabled={!track || rowBusy}
          onChange={(event) => onChange(Number(event.target.value) / SLIDER_STEPS)}
          data-testid="photo-slider"
        />
      </label>
    );
  }

  const busy = progress !== null;

  return (
    <div className="photos-tab" data-testid="photos-tab">
      <div className="photos-tab__head">
        <button type="button" className="edit-track__btn" disabled={busy} onClick={() => input.current?.click()} data-testid="photo-add">
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
        {busy && (
          <span className="photos-tab__status" role="status">
            {t('photos.uploading', { n: progress.done + 1, total: progress.total })}
          </span>
        )}
      </div>
      <p className="photos-tab__note">{t('photos.saved_note')}</p>

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
          {slider(waiting.fraction ?? 0, (fraction) => setUnplaced((list) => list.map((u) => (u.key === waiting.key ? { ...u, fraction } : u))), t('photos.slider'))}
          {rowError && <p className="edit-track__error">{rowError}</p>}
          <div className="photos-tab__actions">
            {unplaced.length > 1 && <span className="photos-tab__meta">{t('photos.more_waiting', { n: unplaced.length - 1 })}</span>}
            <span className="edit-track__spacer" aria-hidden="true" />
            <button type="button" className="edit-track__btn" disabled={rowBusy} onClick={() => dropWaiting(waiting)}>
              {t('photos.skip')}
            </button>
            <button
              type="button"
              className="edit-track__btn edit-track__btn--primary"
              disabled={rowBusy || !track || waiting.fraction === null}
              onClick={() => void placeWaiting(waiting)}
              data-testid="photo-place-here"
            >
              {t('photos.add_here')}
            </button>
          </div>
        </div>
      )}

      {photos.length === 0 && !waiting && !error && <p className="photos-tab__note">{t('photos.empty')}</p>}
      <ul className="photos-tab__list">
        {photos.map((photo, i) => {
          const isEditing = editing?.id === photo.id;
          const title = photo.caption ?? t('photos.photo_n', { n: i + 1 });
          return (
            <li
              key={photo.id}
              ref={isEditing ? editRow : undefined}
              className={isEditing ? 'photos-tab__row photos-tab__row--editing' : 'photos-tab__row'}
              data-testid="photo-row"
            >
              <div className="photos-tab__summary">
                <img className="photos-tab__thumb" src={API_BASE_URL + photo.thumbUrl} alt="" loading="lazy" />
                <span className="photos-tab__text">
                  <span className="photos-tab__title">{title}</span>
                  <span className="photos-tab__meta">{timeOfDay(Date.parse(photo.routeAt) / 1000)}</span>
                </span>
                {!isEditing && (
                  <>
                    <button
                      type="button"
                      className="photos-tab__icon"
                      aria-label={t('photos.edit')}
                      title={t('photos.edit')}
                      disabled={editing !== null || waiting !== null || !track}
                      onClick={() => startEdit(photo)}
                      data-testid="photo-edit"
                    >
                      <Pencil size={16} aria-hidden="true" />
                    </button>
                    <button
                      type="button"
                      className="photos-tab__icon photos-tab__icon--danger"
                      aria-label={t('common.delete')}
                      title={t('common.delete')}
                      disabled={editing !== null}
                      onClick={() => setConfirmDelete(photo)}
                      data-testid="photo-delete"
                    >
                      <Trash2 size={16} aria-hidden="true" />
                    </button>
                  </>
                )}
              </div>
              {isEditing && editing && (
                <>
                  {slider(editing.fraction, (fraction) => setEditing({ ...editing, fraction }), t('photos.slider'))}
                  <input
                    className="settings-page__input"
                    type="text"
                    value={editing.caption}
                    maxLength={MAX_CAPTION_LEN}
                    placeholder={t('photos.caption_placeholder')}
                    aria-label={t('photos.caption')}
                    onChange={(event) => setEditing({ ...editing, caption: event.target.value })}
                    data-testid="photo-caption"
                  />
                  {rowError && <p className="edit-track__error">{rowError}</p>}
                  <div className="photos-tab__actions">
                    <span className="edit-track__spacer" aria-hidden="true" />
                    <button type="button" className="edit-track__btn" disabled={rowBusy} onClick={() => setEditing(null)}>
                      <X size={14} aria-hidden="true" />
                      {t('common.cancel')}
                    </button>
                    <button
                      type="button"
                      className="edit-track__btn edit-track__btn--primary"
                      disabled={rowBusy}
                      onClick={() => void saveEdit(photo)}
                      data-testid="photo-save"
                    >
                      <Check size={14} aria-hidden="true" />
                      {rowBusy ? t('common.saving') : t('photos.save_photo')}
                    </button>
                  </div>
                </>
              )}
            </li>
          );
        })}
      </ul>

      {confirmDelete && (
        <ConfirmDialog
          title={t('photos.delete_title')}
          message={t('photos.delete_confirm')}
          confirmLabel={t('common.delete')}
          busyLabel={t('common.deleting')}
          onConfirm={async () => {
            await deletePhoto(confirmDelete.id);
            onRemove(confirmDelete.id);
          }}
          onClose={() => setConfirmDelete(null)}
        />
      )}
    </div>
  );
}
