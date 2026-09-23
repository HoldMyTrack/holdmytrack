import type { GeoJSONSource, Map as MapLibreMap } from 'maplibre-gl';
import type { TrackPoint } from '../api';

/**
 * The Edit track overlay (IMPLEMENTATION.md §4.7.7): one GeoJSON source drawing the track
 * being edited, built client-side from its full-resolution points — the shared MVT tracks
 * layer only has the simplified geometry, and is hidden entirely while editing anyway. Four
 * layers over it: the kept line, the preview of what the knobs would remove (faded and
 * dashed), every recorded point, and the two knob points.
 */
export const TRACK_EDIT_SOURCE_ID = 'track-edit';
const LINE_LAYER_ID = 'track-edit-line';
const PREVIEW_LAYER_ID = 'track-edit-preview';
export const TRACK_EDIT_POINTS_LAYER_ID = 'track-edit-points';
const KNOBS_LAYER_ID = 'track-edit-knobs';

const ACCENT = '#b07e2e';
const REMOVED = '#c53030';

// A little wider than tracks.ts's CLICK_TOLERANCE_PX — a 3px dot is a smaller target than a line.
const CLICK_TOLERANCE_PX = 5;

interface Feature {
  type: 'Feature';
  properties: { role: string; t?: number; removed?: boolean };
  geometry: { type: 'LineString'; coordinates: number[][] } | { type: 'Point'; coordinates: number[] };
}

const EMPTY: { type: 'FeatureCollection'; features: Feature[] } = { type: 'FeatureCollection', features: [] };

/** Adds the source and layers if missing — idempotent for the same styledata reason every
 *  other overlay here is (a theme swap's setStyle discards custom layers). */
export function ensureTrackEditLayer(map: MapLibreMap, beforeId: string | undefined): void {
  if (!map.getSource(TRACK_EDIT_SOURCE_ID)) {
    map.addSource(TRACK_EDIT_SOURCE_ID, { type: 'geojson', data: EMPTY });
  }
  const add = (layer: Parameters<MapLibreMap['addLayer']>[0]) => {
    if (!map.getLayer(layer.id)) map.addLayer(layer, beforeId);
  };
  add({
    id: PREVIEW_LAYER_ID,
    type: 'line',
    source: TRACK_EDIT_SOURCE_ID,
    filter: ['==', ['get', 'role'], 'preview'],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: { 'line-color': REMOVED, 'line-width': 3, 'line-opacity': 0.55, 'line-dasharray': [1.5, 1.5] },
  });
  add({
    id: LINE_LAYER_ID,
    type: 'line',
    source: TRACK_EDIT_SOURCE_ID,
    filter: ['==', ['get', 'role'], 'kept'],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: { 'line-color': ACCENT, 'line-width': 4, 'line-opacity': 0.95 },
  });
  add({
    id: TRACK_EDIT_POINTS_LAYER_ID,
    type: 'circle',
    source: TRACK_EDIT_SOURCE_ID,
    filter: ['==', ['get', 'role'], 'point'],
    paint: {
      'circle-radius': ['interpolate', ['linear'], ['zoom'], 12, 1.5, 16, 3, 19, 5],
      'circle-color': '#ffffff',
      // Points the knobs would remove take the preview's colour, so the dots agree with the
      // dashed line under them about what's going.
      'circle-stroke-color': ['case', ['boolean', ['get', 'removed'], false], REMOVED, ACCENT],
      'circle-stroke-width': 1.2,
      'circle-opacity': ['case', ['boolean', ['get', 'removed'], false], 0.5, 1],
      'circle-stroke-opacity': ['case', ['boolean', ['get', 'removed'], false], 0.5, 1],
    },
  });
  add({
    id: KNOBS_LAYER_ID,
    type: 'circle',
    source: TRACK_EDIT_SOURCE_ID,
    filter: ['==', ['get', 'role'], 'knob'],
    paint: { 'circle-radius': 7, 'circle-color': ACCENT, 'circle-stroke-color': '#ffffff', 'circle-stroke-width': 2.5 },
  });
}

/** What the knobs would do if pressed now — Chop removes outside them, Cut between them. */
export type EditPreview = 'chop' | 'cut';

/**
 * Redraws the overlay for the currently visible points, the two knob indices into them, and
 * which removal to preview. Knobs at both ends preview nothing.
 */
export function setTrackEditData(
  map: MapLibreMap,
  visible: readonly TrackPoint[],
  lo: number,
  hi: number,
  preview: EditPreview,
): void {
  const source = map.getSource(TRACK_EDIT_SOURCE_ID) as GeoJSONSource | undefined;
  if (!source) return;
  const coords = (from: number, to: number) => visible.slice(from, to + 1).map(([lon, lat]) => [lon, lat]);
  const line = (role: string, c: number[][]): Feature[] =>
    c.length >= 2 ? [{ type: 'Feature', properties: { role }, geometry: { type: 'LineString', coordinates: c } }] : [];

  const last = visible.length - 1;
  const cutting = preview === 'cut' && hi - lo >= 2;
  let features: Feature[];
  if (cutting) {
    // The two knob points are joined straight across, exactly as the Cut would leave them.
    features = [
      ...line('kept', coords(0, lo)),
      ...line('kept', [visible[lo]!, visible[hi]!].map(([lon, lat]) => [lon, lat])),
      ...line('kept', coords(hi, last)),
      ...line('preview', coords(lo, hi)),
    ];
  } else {
    features = [...line('preview', coords(0, lo)), ...line('kept', coords(lo, hi)), ...line('preview', coords(hi, last))];
  }
  visible.forEach(([lon, lat, t], i) => {
    const removed = cutting ? i > lo && i < hi : i < lo || i > hi;
    features.push({ type: 'Feature', properties: { role: 'point', t, removed }, geometry: { type: 'Point', coordinates: [lon, lat] } });
  });
  for (const i of lo === hi ? [lo] : [lo, hi]) {
    const p = visible[i];
    if (p) features.push({ type: 'Feature', properties: { role: 'knob' }, geometry: { type: 'Point', coordinates: [p[0], p[1]] } });
  }
  source.setData({ type: 'FeatureCollection', features });
}

export function clearTrackEdit(map: MapLibreMap): void {
  (map.getSource(TRACK_EDIT_SOURCE_ID) as GeoJSONSource | undefined)?.setData(EMPTY);
}

/**
 * Delete point mode's click: calls `onPoint` with the clicked point's timestamp. Attached per
 * session and detached by the returned function — unlike the tracks layer's handlers, which
 * live for the map's whole life, this one exists only while Delete point mode is on.
 */
export function onTrackEditPointClick(map: MapLibreMap, onPoint: (t: number) => void): () => void {
  const click = (e: { point: { x: number; y: number } }) => {
    if (!map.getLayer(TRACK_EDIT_POINTS_LAYER_ID)) return;
    const { x, y } = e.point;
    const hits = map.queryRenderedFeatures(
      [
        [x - CLICK_TOLERANCE_PX, y - CLICK_TOLERANCE_PX],
        [x + CLICK_TOLERANCE_PX, y + CLICK_TOLERANCE_PX],
      ],
      { layers: [TRACK_EDIT_POINTS_LAYER_ID] },
    );
    // The hit nearest the click, not whichever the renderer happened to list first — points
    // at 1 Hz sit a couple of pixels apart once zoomed out.
    let best: { t: number; d: number } | null = null;
    for (const hit of hits) {
      const t = hit.properties?.t as number | undefined;
      if (t === undefined || hit.geometry.type !== 'Point') continue;
      const p = map.project(hit.geometry.coordinates as [number, number]);
      const d = (p.x - x) ** 2 + (p.y - y) ** 2;
      if (!best || d < best.d) best = { t, d };
    }
    if (best) onPoint(best.t);
  };
  const enter = () => (map.getCanvas().style.cursor = 'crosshair');
  const leave = () => (map.getCanvas().style.cursor = '');
  map.on('click', click);
  map.on('mouseenter', TRACK_EDIT_POINTS_LAYER_ID, enter);
  map.on('mouseleave', TRACK_EDIT_POINTS_LAYER_ID, leave);
  return () => {
    map.off('click', click);
    map.off('mouseenter', TRACK_EDIT_POINTS_LAYER_ID, enter);
    map.off('mouseleave', TRACK_EDIT_POINTS_LAYER_ID, leave);
    map.getCanvas().style.cursor = '';
  };
}
