import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Popup, type Map as MapLibreMap } from 'maplibre-gl';
import { Check, Copy, Navigation } from 'lucide-react';
import type { Spot, SpotCategory } from '../map/spots';
import { t } from '../i18n';
import type { MessageKey } from '../i18n/en';

export interface SpotPopupProps {
  map: MapLibreMap;
  spot: Spot;
  /** The popup's × button was clicked. */
  onClose: () => void;
}

const CATEGORY_KEYS: Record<SpotCategory, MessageKey> = {
  playground: 'spots.category_playground',
  dog_park: 'spots.category_dog_park',
  monument: 'spots.category_monument',
  viewpoint: 'spots.category_viewpoint',
  history: 'spots.category_history',
};

/** How long "Copied" shows in place of Copy address. */
const COPIED_MS = 1500;

/** What Copy address copies: OSM's address, or the coordinates when it has none — every
 *  navigator accepts "lat, lon". */
export function spotAddress(spot: Spot): string {
  return spot.address ?? `${spot.lat.toFixed(6)}, ${spot.lon.toFixed(6)}`;
}

/** Navigate's link: Google Maps' directions to the spot's coordinates, which opens the
 *  platform's own app where there is one and the web version elsewhere. */
export function navigateURL(spot: Spot): string {
  return `https://www.google.com/maps/dir/?api=1&destination=${spot.lat},${spot.lon}`;
}

/**
 * A spot's popup (IMPLEMENTATION.md §4.25, SPEC.md FR-15.3): its name, category and whether
 * the user has been there, with Copy address and Navigate. A MapLibre popup anchored at the
 * spot, so it moves with the map; React renders into it through a portal, the way
 * ExportControl renders into its map control.
 */
export function SpotPopup({ map, spot, onClose }: SpotPopupProps) {
  const [container, setContainer] = useState<HTMLElement | null>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  useEffect(() => {
    const element = document.createElement('div');
    // closeOnClick off: a click elsewhere on the map closes it through spots.ts's own handler.
    const popup = new Popup({ offset: 18, maxWidth: '280px', className: 'spot-popup', focusAfterOpen: false, closeOnClick: false })
      .setLngLat([spot.lon, spot.lat])
      .setDOMContent(element)
      .addTo(map);
    const handleClose = () => onCloseRef.current();
    popup.on('close', handleClose);
    setContainer(element);
    return () => {
      setContainer(null);
      // Off first: removing it here is React replacing or dropping it, not the user closing it,
      // and reporting that would close the popup for the next spot clicked, too.
      popup.off('close', handleClose);
      popup.remove();
    };
  }, [map, spot]);

  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), COPIED_MS);
    return () => window.clearTimeout(timer);
  }, [copied]);

  const copy = () => {
    navigator.clipboard.writeText(spotAddress(spot)).then(
      () => setCopied(true),
      () => {
        // Clipboard refused (an insecure origin, or permission denied): nothing to show for it.
      },
    );
  };

  if (!container) return null;
  const category = t(CATEGORY_KEYS[spot.category]);
  return createPortal(
    <div className="spot-popup__body" data-testid="spot-popup">
      <div className="spot-popup__title">{spot.name ?? category}</div>
      <div className="spot-popup__meta">
        {spot.name && <span>{category}</span>}
        <span className={spot.visited ? 'spot-popup__state spot-popup__state--visited' : 'spot-popup__state'}>
          {spot.visited ? t('spots.visited') : t('spots.not_visited')}
        </span>
      </div>
      {spot.address && <div className="spot-popup__address">{spot.address}</div>}
      <div className="spot-popup__actions">
        <button type="button" className="spot-popup__btn" onClick={copy}>
          {copied ? <Check size={15} aria-hidden="true" /> : <Copy size={15} aria-hidden="true" />}
          {copied ? t('spots.copied') : t('spots.copy_address')}
        </button>
        <a className="spot-popup__btn" href={navigateURL(spot)} target="_blank" rel="noopener noreferrer">
          <Navigation size={15} aria-hidden="true" />
          {t('spots.navigate')}
        </a>
      </div>
    </div>,
    container,
  );
}