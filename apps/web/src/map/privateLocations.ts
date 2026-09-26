import type { ExpressionSpecification, GeoJSONSource, Map as MapLibreMap, MapLayerMouseEvent, MapLayerTouchEvent } from 'maplibre-gl';

/**
 * The Private locations overlay (FR-8.1): every saved circle, the one being edited, and its
 * drag handle. Only drawn while PrivateLocationsPanel is open — the circles are the one thing
 * on the map that points straight at the places they hide, so they never appear in the normal
 * view or in an Export (which renders its own offscreen map from fog and tracks alone —
 * exportMap.ts).
 */
export const PRIVATE_LOCATIONS_SOURCE_ID = 'private-locations';
const FILL_LAYER_ID = 'private-locations-fill';
const OUTLINE_LAYER_ID = 'private-locations-outline';
const CENTER_LAYER_ID = 'private-locations-center';
const HANDLE_LAYER_ID = 'private-locations-handle';

const COLOR = '#7a4bc2';
const EARTH_RADIUS_M = 6371008.8;
const CIRCLE_STEPS = 64;

export interface Circle {
  /** Absent for a circle not saved yet. */
  id?: string;
  lon: number;
  lat: number;
  radiusM: number;
}

interface Feature {
  type: 'Feature';
  properties: { role: 'circle' | 'center' | 'handle'; id: string; selected: boolean };
  geometry: { type: 'Polygon'; coordinates: number[][][] } | { type: 'Point'; coordinates: number[] };
}

// A `case` condition has to be typed boolean — a bare ['get', 'selected'] is rejected as
// "value", and the layer silently never renders (nor answers queryRenderedFeatures).
const SELECTED: ExpressionSpecification = ['boolean', ['get', 'selected'], false];

const EMPTY = { type: 'FeatureCollection' as const, features: [] as Feature[] };

/** Below this zoom Create flies in before placing its circle in the middle of the map: at
 *  country scale a 200 m circle is under a pixel, somewhere nobody could see, let alone aim. */
export const PRIVATE_LOCATIONS_MIN_PLACE_ZOOM = 12;

/** A geodesic circle as a polygon — drawn from destination points at a true ground distance,
 *  so it matches the server's haversine test (ingest's clip.go) rather than a screen circle
 *  that would stretch with latitude. */
function circlePolygon(c: Circle): number[][] {
  const lat1 = (c.lat * Math.PI) / 180;
  const lon1 = (c.lon * Math.PI) / 180;
  const d = c.radiusM / EARTH_RADIUS_M;
  const ring: number[][] = [];
  for (let i = 0; i <= CIRCLE_STEPS; i++) {
    const bearing = (2 * Math.PI * i) / CIRCLE_STEPS;
    const lat2 = Math.asin(Math.sin(lat1) * Math.cos(d) + Math.cos(lat1) * Math.sin(d) * Math.cos(bearing));
    const lon2 = lon1 + Math.atan2(Math.sin(bearing) * Math.sin(d) * Math.cos(lat1), Math.cos(d) - Math.sin(lat1) * Math.sin(lat2));
    ring.push([(lon2 * 180) / Math.PI, (lat2 * 180) / Math.PI]);
  }
  return ring;
}

/** Adds the source and layers if missing — idempotent for the same styledata reason every
 *  other overlay here is (a theme swap's setStyle discards custom layers). */
export function ensurePrivateLocationsLayer(map: MapLibreMap, beforeId: string | undefined): void {
  if (!map.getSource(PRIVATE_LOCATIONS_SOURCE_ID)) {
    map.addSource(PRIVATE_LOCATIONS_SOURCE_ID, { type: 'geojson', data: EMPTY });
  }
  const add = (layer: Parameters<MapLibreMap['addLayer']>[0]) => {
    if (!map.getLayer(layer.id)) map.addLayer(layer, beforeId);
  };
  add({
    id: FILL_LAYER_ID,
    type: 'fill',
    source: PRIVATE_LOCATIONS_SOURCE_ID,
    filter: ['==', ['get', 'role'], 'circle'],
    paint: { 'fill-color': COLOR, 'fill-opacity': ['case', SELECTED, 0.3, 0.16] },
  });
  add({
    id: OUTLINE_LAYER_ID,
    type: 'line',
    source: PRIVATE_LOCATIONS_SOURCE_ID,
    filter: ['==', ['get', 'role'], 'circle'],
    paint: { 'line-color': COLOR, 'line-width': ['case', SELECTED, 2.5, 1.5] },
  });
  // Every saved circle's center, at a fixed pixel size: the circle itself vanishes below a
  // pixel when zoomed out, and this keeps each location visible and clickable at any zoom.
  add({
    id: CENTER_LAYER_ID,
    type: 'circle',
    source: PRIVATE_LOCATIONS_SOURCE_ID,
    filter: ['==', ['get', 'role'], 'center'],
    paint: { 'circle-radius': 5, 'circle-color': COLOR, 'circle-stroke-color': '#ffffff', 'circle-stroke-width': 1.5 },
  });
  add({
    id: HANDLE_LAYER_ID,
    type: 'circle',
    source: PRIVATE_LOCATIONS_SOURCE_ID,
    filter: ['==', ['get', 'role'], 'handle'],
    paint: { 'circle-radius': 8, 'circle-color': COLOR, 'circle-stroke-color': '#ffffff', 'circle-stroke-width': 2.5 },
  });
}

/** Redraws every circle; `selected` (saved or not) is highlighted and gets a drag handle. */
export function setPrivateLocationsData(map: MapLibreMap, circles: readonly Circle[], selected: Circle | null): void {
  const source = map.getSource(PRIVATE_LOCATIONS_SOURCE_ID) as GeoJSONSource | undefined;
  if (!source) return;
  const features: Feature[] = [];
  for (const c of circles) {
    if (selected && c.id !== undefined && c.id === selected.id) continue; // drawn from the draft below
    features.push(
      { type: 'Feature', properties: { role: 'circle', id: c.id ?? '', selected: false }, geometry: { type: 'Polygon', coordinates: [circlePolygon(c)] } },
      { type: 'Feature', properties: { role: 'center', id: c.id ?? '', selected: false }, geometry: { type: 'Point', coordinates: [c.lon, c.lat] } },
    );
  }
  if (selected) {
    const id = selected.id ?? '';
    features.push(
      { type: 'Feature', properties: { role: 'circle', id, selected: true }, geometry: { type: 'Polygon', coordinates: [circlePolygon(selected)] } },
      { type: 'Feature', properties: { role: 'handle', id, selected: true }, geometry: { type: 'Point', coordinates: [selected.lon, selected.lat] } },
    );
  }
  source.setData({ type: 'FeatureCollection', features });
}

export function clearPrivateLocations(map: MapLibreMap): void {
  (map.getSource(PRIVATE_LOCATIONS_SOURCE_ID) as GeoJSONSource | undefined)?.setData(EMPTY);
}

/** What a click on the map landed on. */
export type PrivateLocationsClick =
  | { kind: 'selected' }
  | { kind: 'saved'; id: string }
  | { kind: 'empty'; lngLat: { lng: number; lat: number } };

export interface PrivateLocationsHandlers {
  onClick: (target: PrivateLocationsClick) => void;
  /** The handle being dragged, every frame. */
  onDrag: (lngLat: { lng: number; lat: number }) => void;
}

/**
 * Click-to-select/place and drag-the-handle. The handle drag suspends the map's own drag-pan
 * for its duration, so moving a circle doesn't also pan the map under it. Returns a detach
 * function — attached only while the panel is open.
 */
export function attachPrivateLocationsHandlers(map: MapLibreMap, handlers: PrivateLocationsHandlers): () => void {
  let dragging = false;

  const move = (e: { lngLat: { lng: number; lat: number } }) => {
    if (dragging) handlers.onDrag(e.lngLat);
  };
  const end = () => {
    if (!dragging) return;
    dragging = false;
    map.dragPan.enable();
    map.getCanvas().style.cursor = '';
  };
  const start = (e: MapLayerMouseEvent | MapLayerTouchEvent) => {
    if ('points' in e && e.points.length !== 1) return;
    e.preventDefault();
    dragging = true;
    map.dragPan.disable();
    map.getCanvas().style.cursor = 'grabbing';
  };
  const click = (e: { point: { x: number; y: number }; lngLat: { lng: number; lat: number } }) => {
    const hits = map.getLayer(FILL_LAYER_ID)
      ? map.queryRenderedFeatures([e.point.x, e.point.y], { layers: [HANDLE_LAYER_ID, CENTER_LAYER_ID, FILL_LAYER_ID] })
      : [];
    // The circle being edited wins over a saved one overlapping it.
    if (hits.some((f) => f.properties?.['selected'])) {
      handlers.onClick({ kind: 'selected' });
      return;
    }
    const id = hits[0] ? String(hits[0].properties?.['id'] ?? '') : '';
    handlers.onClick(id ? { kind: 'saved', id } : { kind: 'empty', lngLat: e.lngLat });
  };
  const hoverOn = () => {
    if (!dragging) map.getCanvas().style.cursor = 'grab';
  };
  const hoverOff = () => {
    if (!dragging) map.getCanvas().style.cursor = '';
  };

  map.on('mousedown', HANDLE_LAYER_ID, start);
  map.on('touchstart', HANDLE_LAYER_ID, start);
  map.on('mouseenter', HANDLE_LAYER_ID, hoverOn);
  map.on('mouseleave', HANDLE_LAYER_ID, hoverOff);
  map.on('mousemove', move);
  map.on('touchmove', move);
  map.on('mouseup', end);
  map.on('touchend', end);
  map.on('click', click);
  return () => {
    end();
    map.off('mousedown', HANDLE_LAYER_ID, start);
    map.off('touchstart', HANDLE_LAYER_ID, start);
    map.off('mouseenter', HANDLE_LAYER_ID, hoverOn);
    map.off('mouseleave', HANDLE_LAYER_ID, hoverOff);
    map.off('mousemove', move);
    map.off('touchmove', move);
    map.off('mouseup', end);
    map.off('touchend', end);
    map.off('click', click);
  };
}
