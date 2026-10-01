import { useEffect } from 'react';
import { t } from '../i18n';

export interface PhotoPlacementProps {
  /** Whether the photo already has a marker to drag. */
  placed: boolean;
  busy: boolean;
  error: string | null;
  onCancel: () => void;
}

/**
 * The bar shown while a photo is being placed on the map by hand (FR-16.8): what to do, a
 * Cancel, and the server's refusal if there is one. Escape cancels too. The placing itself —
 * the map's click, the marker's drag — is MapView's.
 */
export function PhotoPlacement({ placed, busy, error, onCancel }: PhotoPlacementProps) {
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented) onCancel();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [onCancel]);

  return (
    <div className="photo-placement" role="status" data-testid="photo-placement">
      <span className="photo-placement__text">
        {busy ? t('common.saving') : error ?? (placed ? t('photos.place_hint_move') : t('photos.place_hint'))}
      </span>
      <button type="button" className="photo-placement__cancel" onClick={onCancel}>
        {t('common.cancel')}
      </button>
    </div>
  );
}
