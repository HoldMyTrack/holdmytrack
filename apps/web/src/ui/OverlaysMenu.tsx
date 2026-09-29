import { useEffect, useRef, useState } from 'react';
import type { Overlays } from '../map/overlays';
import { ChevronDown, Info, Layers } from 'lucide-react';
import { SPOT_CATEGORIES, type SpotCategory } from '../map/spots';
import { t } from '../i18n';
import type { MessageKey } from '../i18n/en';

export interface OverlaysMenuProps {
  overlays: Overlays;
  /** Whether the deployment configures satellite imagery; without it there is no Base map
   *  section (FR-4.14). */
  satelliteAvailable: boolean;
  onChange: (next: Overlays) => void;
}

const CATEGORY_LABELS: Record<SpotCategory, MessageKey> = {
  playground: 'overlays.playgrounds',
  dog_park: 'overlays.dog_parks',
  monument: 'overlays.monuments',
  viewpoint: 'overlays.viewpoints',
  history: 'overlays.history',
};

/**
 * The Layers dropdown beside the map-mode toggle (IMPLEMENTATION.md §4.24, §4.25, §4.26), in three
 * groups: the Base map, Map or Satellite (FR-4.14), when the deployment has imagery; then layers
 * switched on and off over any mode — Paths (Trails, Tracks, Bike paths, FR-4.13) and Points of
 * interest (each Spots category, FR-15.2). The same open/close rules as the Activities panel's
 * Type dropdown: the button toggles it, and a press outside or Escape closes it. The button shows
 * how many overlays are on; the Base map is a choice between two, not an overlay, so the count
 * leaves it out.
 */
export function OverlaysMenu({ overlays, satelliteAvailable, onChange }: OverlaysMenuProps) {
  const [open, setOpen] = useState(false);
  // The Tracks entry's explanation, opened by its info button; closes with the menu.
  const [tracksInfo, setTracksInfo] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) setTracksInfo(false);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!ref.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  const toggleCategory = (category: SpotCategory) => {
    const on = overlays.spots.includes(category);
    onChange({
      ...overlays,
      spots: SPOT_CATEGORIES.filter((c) => (c === category ? !on : overlays.spots.includes(c))),
    });
  };
  const count = Number(overlays.trails) + Number(overlays.tracks) + Number(overlays.bikePaths) + overlays.spots.length;

  const item = (id: string, label: string, checked: boolean, onToggle: () => void) => (
    <label key={id} htmlFor={id} className="overlays-menu__item">
      <input id={id} type="checkbox" checked={checked} onChange={onToggle} />
      <span>{label}</span>
    </label>
  );

  const basemapOption = (id: string, label: string, checked: boolean, onSelect: () => void) => (
    <label htmlFor={id} className="overlays-menu__item">
      <input id={id} type="radio" name="overlay-basemap" checked={checked} onChange={onSelect} />
      <span>{label}</span>
    </label>
  );

  return (
    <div className="map-mode-toggle overlays-menu" ref={ref} data-testid="map-overlays">
      <button
        type="button"
        className={count > 0 ? 'map-mode-toggle__btn map-mode-toggle__btn--active' : 'map-mode-toggle__btn'}
        aria-haspopup="true"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <Layers size={14} aria-hidden="true" />
        {t('map.overlays')}
        {count > 0 && <span className="overlays-menu__count">{count}</span>}
        <ChevronDown size={14} aria-hidden="true" />
      </button>
      {open && (
        <div className="overlays-menu__panel" role="dialog" aria-label={t('map.overlays')}>
          {satelliteAvailable && (
            <fieldset className="overlays-menu__group">
              <legend>{t('overlays.basemap')}</legend>
              {basemapOption('overlay-basemap-map', t('overlays.basemap_map'), !overlays.satellite, () =>
                onChange({ ...overlays, satellite: false }),
              )}
              {basemapOption('overlay-basemap-satellite', t('overlays.basemap_satellite'), overlays.satellite, () =>
                onChange({ ...overlays, satellite: true }),
              )}
            </fieldset>
          )}
          <fieldset className="overlays-menu__group">
            <legend>{t('overlays.paths')}</legend>
            {item('overlay-trails', t('overlays.trails'), overlays.trails, () => onChange({ ...overlays, trails: !overlays.trails }))}
            <div className="overlays-menu__row">
              {item('overlay-tracks', t('overlays.tracks'), overlays.tracks, () => onChange({ ...overlays, tracks: !overlays.tracks }))}
              <button
                type="button"
                className="overlays-menu__info"
                aria-label={t('overlays.tracks_info_label')}
                aria-expanded={tracksInfo}
                aria-controls="overlay-tracks-info"
                onClick={() => setTracksInfo((shown) => !shown)}
              >
                <Info size={14} aria-hidden="true" />
              </button>
            </div>
            {tracksInfo && (
              <p id="overlay-tracks-info" className="overlays-menu__hint" role="note">
                {t('overlays.tracks_info')}
              </p>
            )}
            {item('overlay-bike-paths', t('overlays.bike_paths'), overlays.bikePaths, () =>
              onChange({ ...overlays, bikePaths: !overlays.bikePaths }),
            )}
          </fieldset>
          <fieldset className="overlays-menu__group">
            <legend>{t('overlays.places')}</legend>
            {SPOT_CATEGORIES.map((category) =>
              item(`overlay-spots-${category}`, t(CATEGORY_LABELS[category]), overlays.spots.includes(category), () =>
                toggleCategory(category),
              ),
            )}
          </fieldset>
        </div>
      )}
    </div>
  );
}