import { useEffect, useRef, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { MapPin } from 'lucide-react';
import { getSpotsInArea } from '../api';
import { setSpotsInArea, SPOTS_MIN_ZOOM, type SpotCategory } from '../map/spots';
import { REGION_MIN_ZOOM } from '../map/zoomTiers';
import { t, tn } from '../i18n';

export interface ShowInAreaProps {
  map: MapLibreMap;
  /** The Spots categories the Overlays menu has on; none hides this control. */
  categories: readonly SpotCategory[];
}

/** What the last load found, and for which categories. */
interface Loaded {
  categories: readonly SpotCategory[];
  shown: number;
  total: number;
}

/**
 * "Show in this area" (IMPLEMENTATION.md §4.25, SPEC.md FR-15.5): the Spots tiles start at
 * SPOTS_MIN_ZOOM, and between Region's zoom (REGION_MIN_ZOOM) and there a view holds too many
 * places to load on every pan. So there the places load on request, for the visible map and the
 * chosen categories, and stay drawn until the next request. Once the map has moved, or a
 * category was switched on since, the button comes back to load the new view; until then a
 * line says what the last load found. Hidden outside that zoom range and with no category on.
 */
export function ShowInArea({ map, categories }: ShowInAreaProps) {
  const [zoom, setZoom] = useState(() => map.getZoom());
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [moved, setMoved] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);
  const request = useRef<AbortController | null>(null);

  useEffect(() => {
    const onMoveEnd = () => {
      setZoom(map.getZoom());
      setMoved(true);
    };
    map.on('moveend', onMoveEnd);
    return () => {
      map.off('moveend', onMoveEnd);
      request.current?.abort();
    };
  }, [map]);

  const inRange = categories.length > 0 && zoom >= REGION_MIN_ZOOM && zoom < SPOTS_MIN_ZOOM;
  if (!inRange) return null;

  const load = () => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const b = map.getBounds();
    const wanted = [...categories];
    setLoading(true);
    setError(false);
    getSpotsInArea([b.getWest(), b.getSouth(), b.getEast(), b.getNorth()], wanted, controller.signal)
      .then(({ spots, total }) => {
        setSpotsInArea(map, spots);
        setLoaded({ categories: wanted, shown: spots.length, total });
        setMoved(false);
      })
      .catch(() => {
        if (!controller.signal.aborted) setError(true);
      })
      .finally(() => {
        if (request.current === controller) setLoading(false);
      });
  };

  const current = loaded !== null && !moved && categories.every((c) => loaded.categories.includes(c));
  let status: string | null = null;
  if (current && !error) {
    if (loaded.total === 0) status = t('spots.in_area_none');
    else if (loaded.shown < loaded.total) status = t('spots.in_area_truncated', { shown: loaded.shown, total: loaded.total });
    else status = tn('spots.in_area_count', loaded.total);
  }

  return (
    <div className="show-in-area" data-testid="show-in-area">
      {status !== null ? (
        <span className="show-in-area__status" role="status">
          {status}
        </span>
      ) : (
        <button type="button" className="show-in-area__btn" onClick={load} disabled={loading}>
          <MapPin size={14} aria-hidden="true" />
          {loading ? t('spots.in_area_loading') : error ? t('spots.in_area_failed') : t('spots.in_area')}
        </button>
      )}
    </div>
  );
}