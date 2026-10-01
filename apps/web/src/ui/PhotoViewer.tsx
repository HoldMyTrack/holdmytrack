import { useEffect, useRef, useState } from 'react';
import { ChevronLeft, ChevronRight, MapPin, MapPinOff, Trash2, X } from 'lucide-react';
import { API_BASE_URL, deletePhoto, updatePhoto, type Photo } from '../api';
import { t } from '../i18n';
import { ConfirmDialog } from './ConfirmDialog';
import { formatStartedAt } from './format';

/** Mirrors photos.go's maxPhotoCaptionLen. */
const MAX_CAPTION_LEN = 500;

export interface PhotoViewerProps {
  photos: readonly Photo[];
  id: string;
  readOnly: boolean;
  onNavigate: (id: string) => void;
  /** A PATCH answered with this photo. */
  onChanged: (photo: Photo) => void;
  onDeleted: (id: string) => void;
  /** Close and fly the map to the photo's place. */
  onShowOnMap: (photo: Photo) => void;
  onClose: () => void;
}

/**
 * One photo full size (FR-16.7), over everything — a modal `<dialog>`, like ConfirmDialog, for
 * its Escape, backdrop and focus trap. Steps through `photos` in their order with the arrows
 * (buttons or the keyboard). Shows when it was taken, or that it has no place on the map, and
 * lets the owner write a caption (saved on blur or Enter) and delete it.
 */
export function PhotoViewer({ photos, id, readOnly, onNavigate, onChanged, onDeleted, onShowOnMap, onClose }: PhotoViewerProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const index = photos.findIndex((p) => p.id === id);
  const photo = index >= 0 ? photos[index]! : null;
  const prev = index > 0 ? photos[index - 1]! : null;
  const next = index >= 0 && index < photos.length - 1 ? photos[index + 1]! : null;
  const [caption, setCaption] = useState(photo?.caption ?? '');
  const [error, setError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    const el = ref.current;
    if (el && !el.open) el.showModal();
  }, []);

  // A different photo starts from its own caption.
  useEffect(() => {
    setCaption(photo?.caption ?? '');
    setError(null);
  }, [photo?.id, photo?.caption]);

  // Gone from the list (deleted here, or by a reload) — nothing left to show.
  useEffect(() => {
    if (!photo) ref.current?.close();
  }, [photo]);

  async function saveCaption() {
    if (!photo || readOnly || caption.trim() === (photo.caption ?? '')) return;
    if (caption.trim().length > MAX_CAPTION_LEN) {
      setError(t('photos.caption_too_long', { max: MAX_CAPTION_LEN }));
      return;
    }
    try {
      onChanged(await updatePhoto(photo.id, { caption: caption.trim() }));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('common.something_wrong'));
    }
  }

  if (!photo) return null;
  const placed = photo.lon !== null && photo.lat !== null;

  return (
    <dialog
      ref={ref}
      className="photo-viewer"
      aria-label={t('photos.viewer')}
      data-testid="photo-viewer"
      // React carries a nested dialog's close (the delete confirmation's) up to this handler too;
      // only this dialog's own closing closes the viewer.
      onClose={(event) => {
        if (event.target === ref.current) onClose();
      }}
      onClick={(event) => {
        if (event.target === ref.current) ref.current?.close();
      }}
      onKeyDown={(event) => {
        if (confirmDelete || (event.target as HTMLElement).tagName === 'INPUT') return;
        if (event.key === 'ArrowLeft' && prev) onNavigate(prev.id);
        if (event.key === 'ArrowRight' && next) onNavigate(next.id);
      }}
    >
      <div className="photo-viewer__stage">
        <img
          key={photo.id}
          className="photo-viewer__image"
          src={API_BASE_URL + photo.url}
          alt={photo.caption ?? ''}
          width={photo.width}
          height={photo.height}
        />
        <button type="button" className="photo-viewer__close" aria-label={t('photos.close')} onClick={() => ref.current?.close()}>
          <X size={20} aria-hidden="true" />
        </button>
        {prev && (
          <button type="button" className="photo-viewer__nav photo-viewer__nav--prev" aria-label={t('photos.previous')} onClick={() => onNavigate(prev.id)}>
            <ChevronLeft size={28} aria-hidden="true" />
          </button>
        )}
        {next && (
          <button type="button" className="photo-viewer__nav photo-viewer__nav--next" aria-label={t('photos.next')} onClick={() => onNavigate(next.id)}>
            <ChevronRight size={28} aria-hidden="true" />
          </button>
        )}
      </div>

      <div className="photo-viewer__bar">
        <div className="photo-viewer__meta">
          <span className="photo-viewer__position">{t('photos.position', { n: index + 1, total: photos.length })}</span>
          {photo.takenAt && <span>{t('photos.taken', { when: formatStartedAt(photo.takenAt) })}</span>}
          {!placed && (
            <span className="photo-viewer__unplaced">
              <MapPinOff size={14} aria-hidden="true" />
              {t('photos.not_on_map')}
            </span>
          )}
        </div>
        <input
          className="photo-viewer__caption"
          type="text"
          value={caption}
          maxLength={MAX_CAPTION_LEN}
          placeholder={readOnly ? '' : t('photos.caption_placeholder')}
          aria-label={t('photos.caption')}
          readOnly={readOnly}
          onChange={(event) => setCaption(event.target.value)}
          onBlur={() => void saveCaption()}
          onKeyDown={(event) => {
            if (event.key === 'Enter') void saveCaption();
          }}
          data-testid="photo-caption"
        />
        <div className="photo-viewer__actions">
          {placed && (
            <button type="button" className="photo-viewer__btn" onClick={() => onShowOnMap(photo)}>
              <MapPin size={16} aria-hidden="true" />
              {t('photos.show_on_map')}
            </button>
          )}
          <span className="photo-viewer__spacer" aria-hidden="true" />
          <button
            type="button"
            className="photo-viewer__btn photo-viewer__btn--danger"
            disabled={readOnly}
            title={readOnly ? t('photos.demo_edit') : undefined}
            onClick={() => setConfirmDelete(true)}
          >
            <Trash2 size={16} aria-hidden="true" />
            {t('common.delete')}
          </button>
        </div>
        {error && <p className="photo-viewer__error">{error}</p>}
      </div>

      {confirmDelete && (
        <ConfirmDialog
          title={t('photos.delete_title')}
          message={t('photos.delete_confirm')}
          confirmLabel={t('common.delete')}
          busyLabel={t('common.deleting')}
          onConfirm={async () => {
            await deletePhoto(photo.id);
            onDeleted(photo.id);
            // The next photo, else the one before, else the viewer closes (above).
            const after = next ?? prev;
            if (after) onNavigate(after.id);
          }}
          onClose={() => setConfirmDelete(false)}
        />
      )}
    </dialog>
  );
}
