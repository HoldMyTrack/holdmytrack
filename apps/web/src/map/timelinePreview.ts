import type { GeoJSONSource, LngLatBoundsLike, Map as MapLibreMap } from 'maplibre-gl';
import type { TimelineSegment } from '../timeline/reader';

/**
 * The Google Maps Timeline import's preview (TimelineImportWindow.tsx): what the current
 * selection would import, drawn as dashed lines over the map before anything is sent. A
 * GeoJSON source of its own, like trackBands.ts; the window re-adds it on `styledata`, since a
 * theme swap's `setStyle` drops custom layers.
 */
const SOURCE_ID = 'timeline-preview';
const LAYER_ID = 'timeline-preview-line';

type Collection = {
  type: 'FeatureCollection';
  features: { type: 'Feature'; properties: Record<string, never>; geometry: { type: 'LineString'; coordinates: [number, number][] } }[];
};

function collection(segments: readonly TimelineSegment[]): Collection {
  return {
    type: 'FeatureCollection',
    features: segments.map((s) => ({
      type: 'Feature',
      properties: {},
      geometry: { type: 'LineString', coordinates: s.points.map((p) => [p.lon, p.lat]) },
    })),
  };
}

export function setTimelinePreview(map: MapLibreMap, segments: readonly TimelineSegment[]): void {
  const data = collection(segments);
  const source = map.getSource(SOURCE_ID) as GeoJSONSource | undefined;
  if (source) source.setData(data);
  else map.addSource(SOURCE_ID, { type: 'geojson', data });
  if (!map.getLayer(LAYER_ID)) {
    map.addLayer({
      id: LAYER_ID,
      type: 'line',
      source: SOURCE_ID,
      layout: { 'line-cap': 'round', 'line-join': 'round' },
      paint: { 'line-color': '#d9480f', 'line-width': 2.5, 'line-opacity': 0.9, 'line-dasharray': [2, 1.5] },
    });
  }
}

export function clearTimelinePreview(map: MapLibreMap): void {
  if (map.getLayer(LAYER_ID)) map.removeLayer(LAYER_ID);
  if (map.getSource(SOURCE_ID)) map.removeSource(SOURCE_ID);
}

/** The selection's bounds, for fitting the map to it; null when there's nothing selected. */
export function previewBounds(segments: readonly TimelineSegment[]): LngLatBoundsLike | null {
  let w = Infinity;
  let s = Infinity;
  let e = -Infinity;
  let n = -Infinity;
  for (const seg of segments) {
    for (const p of seg.points) {
      w = Math.min(w, p.lon);
      e = Math.max(e, p.lon);
      s = Math.min(s, p.lat);
      n = Math.max(n, p.lat);
    }
  }
  return Number.isFinite(w) ? [w, s, e, n] : null;
}
