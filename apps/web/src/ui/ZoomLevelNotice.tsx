import { useEffect, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import type { MapMode } from '../map/mapMode';
import { coverageOverlays } from '../map/coverageLayers';
import { zoomTier } from '../map/coverageStyle';
import { levelNoticeKey } from '../map/zoomTiers';
import { t } from '../i18n';

/** How long the notice stays before it fades. */
const SHOW_MS = 3000;

export interface ZoomLevelNoticeProps {
  map: MapLibreMap;
  mode: MapMode;
}

/**
 * Fog draws at three levels by zoom (`SPEC.md` FR-4.2): whole countries, then whole states or
 * provinces, then exactly where you've been, at the tiers the style sets (coverageStyle.ts). Nothing else on the map says which
 * is in view, and a veil lifted from a whole country reads as a bug when you don't know that's
 * the level you're at. So this names it — a pill under the mode toggle (MapView renders it in
 * `.map-toggles`, on a line of its own below however many rows those wrap to), on entering the
 * mode and whenever a zoom crosses into another level, faded after SHOW_MS. Heatmap gets the
 * same pill with one message at every zoom (levelNoticeKey), shown on entering it.
 * Read on `zoomend`, not mid-gesture, so a pinch through two levels names only where it lands.
 * Never in Normal mode, which draws tracks at every zoom.
 */
export function ZoomLevelNotice({ map, mode }: ZoomLevelNoticeProps) {
  const [tier, setTier] = useState(() => tierAt(map));
  const [shown, setShown] = useState<string | null>(null);
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    const onZoomEnd = () => setTier(tierAt(map));
    map.on('zoomend', onZoomEnd);
    return () => {
      map.off('zoomend', onZoomEnd);
    };
  }, [map]);

  const key = mode === 'normal' ? null : levelNoticeKey(mode, tier);
  useEffect(() => {
    if (key === null) {
      setVisible(false);
      return;
    }
    setShown(t(key));
    setVisible(true);
    const timer = window.setTimeout(() => setVisible(false), SHOW_MS);
    return () => window.clearTimeout(timer);
  }, [key]);

  if (shown === null) return null;
  return (
    <div className={`zoom-level-notice${visible ? ' zoom-level-notice--visible' : ''}`} role="status" data-testid="zoom-level-notice">
      <span className="zoom-level-notice__pill">{shown}</span>
    </div>
  );
}

function tierAt(map: MapLibreMap) {
  return zoomTier(coverageOverlays(map)?.tiers ?? [], map.getZoom());
}
