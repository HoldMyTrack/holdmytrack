import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Popup, type Map as MapLibreMap } from 'maplibre-gl';
import { BookOpen, Check, Copy } from 'lucide-react';
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

/** The article OSM's `wikipedia` tag names, "lang:Article title" — the server only keeps that
 *  form — on that language's Wikipedia. */
export function wikipediaURL(tag: string): string {
  const colon = tag.indexOf(':');
  const title = tag.slice(colon + 1).trim().replace(/ /g, '_');
  return `https://${tag.slice(0, colon)}.wikipedia.org/wiki/${encodeURIComponent(title)}`;
}

/** OSM's `memorial` type as words: "war_memorial" → "War memorial". OSM's own English
 *  vocabulary, shown as it is in every language. */
export function memorialLabel(memorial: string): string {
  const words = memorial.replace(/[_;]+/g, ' ').trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/**
 * A spot's popup (IMPLEMENTATION.md §4.25, SPEC.md FR-15.3): its name and category, what OSM
 * says about it — memorial type, date, description, inscription, address — each only when OSM
 * has it, with Copy address and, when OSM names one, its Wikipedia article. A MapLibre popup anchored at the
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
  // Under the title: the category (unless it already is the title), the memorial type, the date.
  const meta = [
    spot.name ? category : null,
    spot.memorial ? memorialLabel(spot.memorial) : null,
    spot.startDate ? t('spots.since', { date: spot.startDate }) : null,
  ].filter((item): item is string => item !== null);
  return createPortal(
    <div className="spot-popup__body" data-testid="spot-popup">
      <div className="spot-popup__title">{spot.name ?? category}</div>
      {meta.length > 0 && (
        <div className="spot-popup__meta">
          {meta.map((item) => (
            <span key={item}>{item}</span>
          ))}
        </div>
      )}
      {spot.description && <p className="spot-popup__description">{spot.description}</p>}
      {spot.inscription && <blockquote className="spot-popup__inscription">{spot.inscription}</blockquote>}
      {spot.address && <div className="spot-popup__address">{spot.address}</div>}
      <div className="spot-popup__actions">
        <button type="button" className="spot-popup__btn" onClick={copy}>
          {copied ? <Check size={15} aria-hidden="true" /> : <Copy size={15} aria-hidden="true" />}
          {copied ? t('spots.copied') : t('spots.copy_address')}
        </button>
        {spot.wikipedia && (
          <a className="spot-popup__btn" href={wikipediaURL(spot.wikipedia)} target="_blank" rel="noopener noreferrer">
            <BookOpen size={15} aria-hidden="true" />
            {t('spots.wikipedia')}
          </a>
        )}
      </div>
    </div>,
    container,
  );
}