import { useRef, useState } from 'react';
import { ImagePlus, Images, MapPinOff } from 'lucide-react';
import { API_BASE_URL, uploadPhoto, type Photo } from '../api';
import { t } from '../i18n';
import { preparePhoto, UnreadablePhotoError } from './photoPrep';

interface Failure {
  name: string;
  message: string;
}

export interface PhotoStripProps {
  photos: readonly Photo[];
  error: string | null;
  /** The activity new photos go to, or null where there's none to add to (an open Story with
   *  no activity selected) — `addHint` then says what to do instead. */
  activityId: string | null;
  addHint?: string;
  readOnly: boolean;
  /** A photo landed; the list should be refetched (its place in the order is the server's). */
  onUploaded: () => void;
  onOpen: (id: string) => void;
}

/**
 * The selected activity's photos, or the open Story's (FR-16), as a strip of thumbnails along
 * the bottom of the map, with Add photos. Picked files are prepared and uploaded one at a time
 * (photoPrep.ts resizes each in the browser first), each landing in the strip as it's done; a
 * file that fails is named with the reason and the rest carry on. A photo with no place on the
 * map carries a badge saying so. A thumbnail opens the viewer.
 */
export function PhotoStrip({ photos, error, activityId, addHint, readOnly, onUploaded, onOpen }: PhotoStripProps) {
  const input = useRef<HTMLInputElement>(null);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [failures, setFailures] = useState<Failure[]>([]);

  async function upload(files: File[]) {
    if (activityId === null || files.length === 0) return;
    const failed: Failure[] = [];
    setFailures([]);
    for (let i = 0; i < files.length; i++) {
      const file = files[i]!;
      setProgress({ done: i, total: files.length });
      try {
        await uploadPhoto(activityId, await preparePhoto(file));
        onUploaded();
      } catch (err) {
        failed.push({
          name: file.name,
          message: err instanceof UnreadablePhotoError ? t('photos.unreadable') : err instanceof Error ? err.message : String(err),
        });
        setFailures([...failed]);
      }
    }
    setProgress(null);
  }

  const busy = progress !== null;
  const addDisabledReason = readOnly ? t('photos.demo_add') : undefined;

  return (
    <section className="photo-strip" aria-label={t('photos.title')} data-testid="photo-strip">
      <header className="photo-strip__head">
        <Images size={16} aria-hidden="true" />
        <span className="photo-strip__title">{t('photos.title')}</span>
        {photos.length > 0 && <span className="photo-strip__count">{photos.length}</span>}
        <span className="photo-strip__spacer" aria-hidden="true" />
        {busy && (
          <span className="photo-strip__status" role="status">
            {t('photos.uploading', { n: progress.done + 1, total: progress.total })}
          </span>
        )}
        {activityId !== null && (
          <>
            <button
              type="button"
              className="photo-strip__add"
              disabled={readOnly || busy}
              title={addDisabledReason}
              onClick={() => input.current?.click()}
              data-testid="photo-add"
            >
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
                void upload(files);
              }}
              data-testid="photo-input"
            />
          </>
        )}
      </header>

      {error && <p className="photo-strip__error">{error}</p>}
      {failures.length > 0 && (
        <ul className="photo-strip__failures" aria-live="polite">
          {failures.map((f, i) => (
            <li key={i}>{t('photos.upload_failed', { name: f.name, message: f.message })}</li>
          ))}
        </ul>
      )}

      {photos.length > 0 ? (
        <ul className="photo-strip__list">
          {photos.map((photo) => {
            const label = photo.caption ?? t('photos.open');
            return (
              <li key={photo.id}>
                <button type="button" className="photo-strip__thumb" title={label} onClick={() => onOpen(photo.id)}>
                  <img src={API_BASE_URL + photo.thumbUrl} alt={label} loading="lazy" decoding="async" />
                  {photo.lon === null && (
                    <span className="photo-strip__badge" title={t('photos.not_on_map')}>
                      <MapPinOff size={12} aria-label={t('photos.not_on_map')} />
                    </span>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      ) : (
        !error && (
          <p className="photo-strip__hint">
            {activityId !== null ? (readOnly ? t('photos.empty_demo') : t('photos.empty')) : addHint}
          </p>
        )
      )}
      {photos.length > 0 && activityId === null && addHint && <p className="photo-strip__hint">{addHint}</p>}
    </section>
  );
}
