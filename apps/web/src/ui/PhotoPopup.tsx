import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Popup, type Map as MapLibreMap } from 'maplibre-gl';
import { ExternalLink } from 'lucide-react';
import { API_BASE_URL, type Photo } from '../api';
import { t } from '../i18n';
import { formatStartedAt } from './format';

export interface PhotoPopupProps {
  map: MapLibreMap;
  photo: Photo;
  onClose: () => void;
}

/**
 * A photo's popup (FR-16.7), opened from its marker: the picture, its caption and when it was
 * taken, anchored at its place on the route so it moves with the map — no window over the page.
 * Full size opens the stored copy in a browser tab. Changing a photo is the Edit window's Photos
 * tab's job, not this. A MapLibre popup with React rendered into it through a portal, as
 * SpotPopup.tsx does.
 */
export function PhotoPopup({ map, photo, onClose }: PhotoPopupProps) {
  const [container, setContainer] = useState<HTMLElement | null>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  const popupRef = useRef<Popup | null>(null);
  const { lon, lat } = photo;

  useEffect(() => {
    if (lon === null || lat === null) return;
    const element = document.createElement('div');
    const popup = new Popup({ offset: 24, maxWidth: '300px', className: 'spot-popup photo-popup', focusAfterOpen: false, closeOnClick: false })
      .setLngLat([lon, lat])
      .setDOMContent(element)
      .addTo(map);
    const handleClose = () => onCloseRef.current();
    popup.on('close', handleClose);
    popupRef.current = popup;
    setContainer(element);
    return () => {
      popupRef.current = null;
      setContainer(null);
      popup.off('close', handleClose);
      popup.remove();
    };
    // A new place is a new popup; the same photo re-read with the same place keeps this one.
  }, [map, photo.id, lon, lat]);

  // MapLibre picks which side of the marker the popup opens on from its size when placed — empty
  // then, before the portal renders. Placing it again once the content is in (and again once the
  // picture has its height) lets it open below a marker near the top of the map instead of off it.
  const reanchor = () => {
    if (lon !== null && lat !== null) popupRef.current?.setLngLat([lon, lat]);
  };
  useEffect(reanchor, [container]);

  if (!container) return null;
  const full = API_BASE_URL + photo.url;
  return createPortal(
    <div className="photo-popup__body" data-testid="photo-popup">
      <a href={full} target="_blank" rel="noopener noreferrer" className="photo-popup__image-link">
        <img
          className="photo-popup__image"
          src={full}
          alt={photo.caption ?? ''}
          width={photo.width}
          height={photo.height}
          decoding="async"
          onLoad={reanchor}
        />
      </a>
      {photo.caption && <p className="photo-popup__caption">{photo.caption}</p>}
      <div className="photo-popup__meta">
        {photo.takenAt && <span>{t('photos.taken', { when: formatStartedAt(photo.takenAt) })}</span>}
        <a className="photo-popup__full" href={full} target="_blank" rel="noopener noreferrer">
          <ExternalLink size={13} aria-hidden="true" />
          {t('photos.full_size')}
        </a>
      </div>
    </div>,
    container,
  );
}
