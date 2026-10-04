import { useEffect, useRef, useState } from 'react';
import { pickedCount, type Overlays } from '../map/overlays';
import { ChevronDown, Info, Layers } from 'lucide-react';
import { SPOT_CATEGORIES, type SpotCategory } from '../map/spots';
import { t } from '../i18n';
import type { MessageKey } from '../i18n/en';

export interface OverlaysMenuProps {
  overlays: Overlays;
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
 * The Layers dropdown beside the map-mode toggle (IMPLEMENTATION.md §4.24, §4.25): layers picked
 * over any mode, in two groups — Paths (Trails, Tracks, Bike paths, FR-4.13) and Points of interest
 * (each Spots category, FR-15.2). The same open/close rules as the Activities panel's Type
 * dropdown: the button toggles it, and a press outside or Escape closes it. The button shows how
 * many are picked, and the checkbox before it shows or hides all of them at once, keeping the
 * picks; picking one while it's off turns it back on, or the pick would seem to do nothing.
 */
export function OverlaysMenu({ overlays, onChange }: OverlaysMenuProps) {
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

  // `on`: whether this change picks something, which turns the checkbox back on.
  const change = (patch: Partial<Overlays>, on: boolean) =>
    onChange({ ...overlays, ...patch, enabled: on || overlays.enabled });
  const toggleCategory = (category: SpotCategory) => {
    const on = overlays.spots.includes(category);
    change({ spots: SPOT_CATEGORIES.filter((c) => (c === category ? !on : overlays.spots.includes(c))) }, !on);
  };
  const togglePath = (kind: 'trails' | 'tracks' | 'bikePaths') => change({ [kind]: !overlays[kind] }, !overlays[kind]);
  const count = pickedCount(overlays);
  const shown = overlays.enabled && count > 0;

  const item = (id: string, label: string, checked: boolean, onToggle: () => void) => (
    <label key={id} htmlFor={id} className="overlays-menu__item">
      <input id={id} type="checkbox" checked={checked} onChange={onToggle} />
      <span>{label}</span>
    </label>
  );

  return (
    <div className="map-mode-toggle overlays-menu" ref={ref} data-testid="map-overlays">
      <label className="overlays-menu__master" title={count === 0 ? t('overlays.master_empty') : undefined}>
        <input
          id="overlay-master"
          type="checkbox"
          aria-label={t('overlays.master')}
          checked={overlays.enabled}
          disabled={count === 0}
          onChange={() => onChange({ ...overlays, enabled: !overlays.enabled })}
        />
      </label>
      <button
        type="button"
        className={shown ? 'map-mode-toggle__btn map-mode-toggle__btn--active' : 'map-mode-toggle__btn'}
        aria-haspopup="true"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <Layers size={14} aria-hidden="true" />
        {t('map.overlays')}
        {count > 0 && <span className={shown ? 'overlays-menu__count' : 'overlays-menu__count overlays-menu__count--off'}>{count}</span>}
        <ChevronDown size={14} aria-hidden="true" />
      </button>
      {open && (
        <div className="overlays-menu__panel" role="dialog" aria-label={t('map.overlays')}>
          <fieldset className="overlays-menu__group">
            <legend>{t('overlays.paths')}</legend>
            {item('overlay-trails', t('overlays.trails'), overlays.trails, () => togglePath('trails'))}
            <div className="overlays-menu__row">
              {item('overlay-tracks', t('overlays.tracks'), overlays.tracks, () => togglePath('tracks'))}
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
            {item('overlay-bike-paths', t('overlays.bike_paths'), overlays.bikePaths, () => togglePath('bikePaths'))}
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