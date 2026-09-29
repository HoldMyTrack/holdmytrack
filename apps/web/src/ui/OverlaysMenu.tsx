import { useEffect, useRef, useState } from 'react';
import { NO_OVERLAYS, type Overlays } from '../map/overlays';
import { ChevronDown, Layers } from 'lucide-react';
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
 * The Overlays dropdown beside the map-mode toggle (IMPLEMENTATION.md §4.24, §4.25, §4.26): first
 * the Base map, Map or Satellite (FR-4.14), when the deployment has imagery; then layers
 * switched on and off over any mode, in two groups — Routes (Trails, Tracks, Bike paths, FR-4.13) and
 * Points of interest (each Spots category, FR-15.2) — under one All that switches every one of
 * them, and each group's own All. The Base map is a choice between two, not an overlay, so All
 * leaves it and the count leaves it out. The same open/close rules as the
 * Activities panel's Type dropdown: the button toggles it, and a press outside or Escape closes
 * it. The button shows how many overlays are on.
 */
export function OverlaysMenu({ overlays, satelliteAvailable, onChange }: OverlaysMenuProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

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
  const allSpots = overlays.spots.length === SPOT_CATEGORIES.length;
  const count = Number(overlays.trails) + Number(overlays.tracks) + Number(overlays.bikePaths) + overlays.spots.length;
  const total = 3 + SPOT_CATEGORIES.length;

  // An All box is ticked when every entry under it is, half-ticked when some are; clicking it
  // turns them all on, or all off when they already were.
  const item = (id: string, label: string, checked: boolean, onToggle: () => void, some = false, all = false) => (
    <label key={id} htmlFor={id} className={all ? 'overlays-menu__item overlays-menu__item--all' : 'overlays-menu__item'}>
      <input
        id={id}
        type="checkbox"
        checked={checked}
        ref={(input) => {
          if (input) input.indeterminate = some && !checked;
        }}
        onChange={onToggle}
      />
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
            <fieldset className="overlays-menu__group overlays-menu__group--basemap">
              <legend>{t('overlays.basemap')}</legend>
              {basemapOption('overlay-basemap-map', t('overlays.basemap_map'), !overlays.satellite, () =>
                onChange({ ...overlays, satellite: false }),
              )}
              {basemapOption('overlay-basemap-satellite', t('overlays.basemap_satellite'), overlays.satellite, () =>
                onChange({ ...overlays, satellite: true }),
              )}
            </fieldset>
          )}
          {item(
            'overlay-all',
            t('overlays.all'),
            count === total,
            () =>
              onChange(
                count === total
                  ? { ...NO_OVERLAYS, satellite: overlays.satellite }
                  : { satellite: overlays.satellite, trails: true, tracks: true, bikePaths: true, spots: [...SPOT_CATEGORIES] },
              ),
            count > 0,
            true,
          )}
          <fieldset className="overlays-menu__group">
            <legend>{t('overlays.routes')}</legend>
            {item('overlay-trails', t('overlays.trails'), overlays.trails, () => onChange({ ...overlays, trails: !overlays.trails }))}
            {item('overlay-tracks', t('overlays.tracks'), overlays.tracks, () => onChange({ ...overlays, tracks: !overlays.tracks }))}
            {item('overlay-bike-paths', t('overlays.bike_paths'), overlays.bikePaths, () =>
              onChange({ ...overlays, bikePaths: !overlays.bikePaths }),
            )}
          </fieldset>
          <fieldset className="overlays-menu__group">
            <legend>{t('overlays.places')}</legend>
            {item(
              'overlay-spots-all',
              t('overlays.all_places'),
              allSpots,
              () => onChange({ ...overlays, spots: allSpots ? [] : [...SPOT_CATEGORIES] }),
              overlays.spots.length > 0,
            )}
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