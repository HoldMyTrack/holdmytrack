import { Satellite } from 'lucide-react';
import { t } from '../i18n';

export interface BasemapToggleProps {
  satellite: boolean;
  onChange: (satellite: boolean) => void;
}

/**
 * The Satellite button beside the Layers menu (FR-4.14, IMPLEMENTATION.md §4.26): one press
 * switches the base map between the vector map and satellite imagery. A choice between two, so a
 * pressed button rather than a menu. MapView shows it only when the deployment has imagery.
 */
export function BasemapToggle({ satellite, onChange }: BasemapToggleProps) {
  return (
    <div className="map-mode-toggle basemap-toggle" data-testid="map-basemap">
      <button
        type="button"
        className={satellite ? 'map-mode-toggle__btn map-mode-toggle__btn--active' : 'map-mode-toggle__btn'}
        aria-pressed={satellite}
        onClick={() => onChange(!satellite)}
      >
        <Satellite size={14} aria-hidden="true" />
        {t('overlays.basemap_satellite')}
      </button>
    </div>
  );
}
