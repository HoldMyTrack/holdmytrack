import { useEffect, useRef, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import type { MapMode } from '../map/mapMode';
import { zoomTier } from '../map/zoomTiers';
import { t } from '../i18n';

/** How long the notice stays before it fades. */
const SHOW_MS = 3000;

export interface ZoomLevelNoticeProps {
  map: MapLibreMap;
  mode: MapMode;
}

/**
 * Fog and Heatmap draw at three levels by zoom (`SPEC.md` FR-4.2, FR-4.3): whole countries,
 * then whole states or provinces, then exactly where you've been (zoomTiers.ts). Nothing else
 * on the map says which is in view, and a veil lifted from a whole country reads as a bug when
 * you don't know that's the level you're at. So this names it — a pill under the mode toggle
 * (MapView renders it in `.map-toggles`, on a line of its own below however many rows those wrap to),
 * on entering either mode and whenever a zoom crosses into another level, faded after SHOW_MS.
 * Read on `zoomend`, not mid-gesture, so a pinch through two levels names only where it lands.
 * Never in Normal mode, which draws tracks at every zoom.
 */
export function ZoomLevelNotice({ map, mode }: ZoomLevelNoticeProps) {
  const [tier, setTier] = useState(() => zoomTier(map.getZoom()));
  const [shown, setShown] = useState<string | null>(null);
  const [visible, setVisible] = useState(false);
  const lastKey = useRef<string | null>(null);

  useEffect(() => {
    const onZoomEnd = () => setTier(zoomTier(map.getZoom()));
    map.on('zoomend', onZoomEnd);
    return () => {
      map.off('zoomend', onZoomEnd);
    };
  }, [map]);

  useEffect(() => {
    if (mode === 'normal') {
      lastKey.current = null;
      setVisible(false);
      return;
    }
    const key = `${mode}.${tier}`;
    if (key === lastKey.current) return;
    lastKey.current = key;
    setShown(t(`map.level.${mode}.${tier}`));
    setVisible(true);
    const timer = window.setTimeout(() => setVisible(false), SHOW_MS);
    return () => window.clearTimeout(timer);
  }, [mode, tier]);

  if (shown === null) return null;
  return (
    <div className={`zoom-level-notice${visible ? ' zoom-level-notice--visible' : ''}`} role="status" data-testid="zoom-level-notice">
      <span className="zoom-level-notice__pill">{shown}</span>
    </div>
  );
}
