import { distanceValue, unitLabel } from './format';
import { useUnitSystem } from './units';
import type { DistanceRange } from './activityFacets';

/**
 * The Activities panel's DISTANCE dual slider — standalone and always visible now (§4.7.6),
 * not folded into the Type dropdown or hidden behind the old "Filter" toggle button. TYPE
 * moved into its own dropdown (checkboxes inline in ActivitiesPanel.tsx's toolbar); this
 * component now does one job, distance only, and no longer takes any TYPE-related props.
 */
export interface DistanceFilterProps {
  bounds: DistanceRange | null;
  value: DistanceRange | null;
  onChangeDistance: (next: DistanceRange | null) => void;
  onReset: () => void;
  hasActiveFilters: boolean;
}

export function DistanceFilter({ bounds, value, onChangeDistance, onReset, hasActiveFilters }: DistanceFilterProps) {
  const system = useUnitSystem();
  const current = value ?? bounds;

  if (!bounds || bounds.min >= bounds.max || !current) return null;

  return (
    <div className="activity-filters" data-testid="distance-filter">
      <div className="activity-filters__section">
        <div className="activity-filters__head">
          <span className="activity-filters__label">Distance</span>
          <span className="activity-filters__readout" data-testid="distance-readout">
            {value === null
              ? 'any distance'
              : `${distanceValue(current.min, system)} – ${distanceValue(current.max, system)} ${unitLabel(system)}`}
          </span>
        </div>
        <div className="activity-filters__track-wrap">
          <div className="activity-filters__track" />
          <div
            className="activity-filters__fill"
            style={{
              left: `${((current.min - bounds.min) / (bounds.max - bounds.min)) * 100}%`,
              right: `${100 - ((current.max - bounds.min) / (bounds.max - bounds.min)) * 100}%`,
            }}
          />
          <input
            type="range"
            className="activity-filters__range activity-filters__range--min"
            aria-label="Minimum distance"
            min={bounds.min}
            max={bounds.max}
            value={current.min}
            onChange={(event) => onChangeDistance({ min: Math.min(Number(event.target.value), current.max), max: current.max })}
          />
          <input
            type="range"
            className="activity-filters__range activity-filters__range--max"
            aria-label="Maximum distance"
            min={bounds.min}
            max={bounds.max}
            value={current.max}
            onChange={(event) => onChangeDistance({ min: current.min, max: Math.max(Number(event.target.value), current.min) })}
          />
        </div>
        <div className="activity-filters__bounds">
          <span>
            {distanceValue(bounds.min, system)} {unitLabel(system)}
          </span>
          <span>
            {distanceValue(bounds.max, system)} {unitLabel(system)}
          </span>
        </div>
      </div>

      {hasActiveFilters && (
        <button type="button" className="activity-filters__reset" onClick={onReset}>
          Reset filters
        </button>
      )}
    </div>
  );
}
