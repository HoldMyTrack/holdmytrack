import { useCallback, useMemo, useState } from 'react';
import type { BikePathOverlays } from './bikePaths';
import { satelliteSource } from './config';
import { loadOverlays, saveOverlays, type Overlays } from './overlays';

export interface OverlayChoices {
  overlays: Overlays;
  change: (next: Overlays) => void;
  /** The paths drawn: each picked, while the Layers checkbox is on. */
  paths: BikePathOverlays;
  /** The deployment configures satellite imagery (FR-4.14). */
  satelliteAvailable: boolean;
  /** Imagery is drawn: available and picked, whatever was saved. */
  satellite: boolean;
}

/** The Satellite button and the Layers menu's paths and Spots categories, remembered per browser (overlays.ts). */
export function useOverlays(): OverlayChoices {
  const [overlays, setOverlays] = useState<Overlays>(loadOverlays);
  const change = useCallback((next: Overlays) => {
    saveOverlays(next);
    setOverlays(next);
  }, []);
  const paths = useMemo(
    () => ({
      bikePaths: overlays.enabled && overlays.bikePaths,
      sharedPaths: overlays.enabled && overlays.sharedPaths,
      mtbTrails: overlays.enabled && overlays.mtbTrails,
    }),
    [overlays.enabled, overlays.bikePaths, overlays.sharedPaths, overlays.mtbTrails],
  );
  const satelliteAvailable = satelliteSource() !== null;
  return { overlays, change, paths, satelliteAvailable, satellite: satelliteAvailable && overlays.satellite };
}
