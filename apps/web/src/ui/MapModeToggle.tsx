import { Fragment } from 'react';
import type { MapMode } from '../map/mapMode';
import { t } from '../i18n';

const MODES: readonly MapMode[] = ['normal', 'fog', 'heatmap'];
const LABELS: Record<MapMode, 'map.mode_normal' | 'map.mode_fog' | 'map.mode_heatmap'> = {
  normal: 'map.mode_normal',
  fog: 'map.mode_fog',
  heatmap: 'map.mode_heatmap',
};

/** Normal, Fog and Heatmap: one at a time (docs/SPEC.md FR-4.4). */
export function MapModeToggle({ mode, onChange }: { mode: MapMode; onChange: (mode: MapMode) => void }) {
  return (
    <div className="map-mode-toggle" role="group" aria-label={t('map.mode')} data-testid="map-mode-toggle">
      {MODES.map((value) => (
        <Fragment key={value}>
          {/* Normal on one side, the two coverage views on the other. */}
          {value === 'fog' && <span className="map-mode-toggle__divider" aria-hidden="true" />}
          <button
            type="button"
            className={mode === value ? 'map-mode-toggle__btn map-mode-toggle__btn--active' : 'map-mode-toggle__btn'}
            aria-pressed={mode === value}
            onClick={() => onChange(value)}
          >
            {t(LABELS[value])}
          </button>
        </Fragment>
      ))}
    </div>
  );
}
