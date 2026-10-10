import { createElement, type ComponentType } from 'react';
import { flushSync } from 'react-dom';
import { createRoot } from 'react-dom/client';
import type { ExpressionSpecification, FilterSpecification, GeoJSONSource, Map as MapLibreMap, MapGeoJSONFeature } from 'maplibre-gl';
import type { FeatureCollection, Point } from 'geojson';
import { Binoculars, Castle, createLucideIcon, Dog, Landmark, type LucideProps } from 'lucide-react';
import { API_BASE_URL, TILES_V1 } from '../api';
import { versionedTileURL } from './coverageVersion';
import { PATHS_MIN_ZOOM } from './style';

/**
 * The Spots layers (IMPLEMENTATION.md §4.25, ADR-0021): outdoor places from OpenStreetMap, each an
 * icon over its area, drawn in every map mode for the categories the Layers menu has on. The tiles come from
 * `/tiles/v1/spots`, cached under the account's tile version like every other map tile
 * (coverageVersion.ts).
 *
 * Unlike every other overlay they're added on top of the whole stack rather than beneath the
 * basemap's labels (layers.ts): a spot is something to find on the map, so the fog veil mustn't
 * bury it, and a label crossing it is less of a loss than the icon.
 */
export const SPOTS_SOURCE_ID = 'spots';
export const SPOTS_LAYER_ID = 'spots-icons';
const SPOTS_AREA_FILL_LAYER_ID = 'spots-area-fill';
const SPOTS_AREA_LINE_LAYER_ID = 'spots-area-line';
const SPOTS_CIRCLE_LINE_LAYER_ID = 'spots-circle-line';
// "Show in this area" (ShowInArea.tsx): the places it fetched, below the tiles' zoom.
const SPOTS_IN_AREA_SOURCE_ID = 'spots-in-area';
const SPOTS_IN_AREA_LAYER_ID = 'spots-in-area-icons';
/** Bottom to top: every area's fill, the outlines, the circles' dashed edges, "Show in this
 *  area"'s badges, the tiles' badges. The two badge layers never overlap: one stops where the
 *  other starts, at SPOTS_MIN_ZOOM. */
const SPOTS_LAYER_IDS = [
  SPOTS_AREA_FILL_LAYER_ID,
  SPOTS_AREA_LINE_LAYER_ID,
  SPOTS_CIRCLE_LINE_LAYER_ID,
  SPOTS_IN_AREA_LAYER_ID,
  SPOTS_LAYER_ID,
];
// The backend query's two ST_AsMVT layers: one point per spot, and each spot's area.
const SPOTS_SOURCE_LAYER = 'spots';
const SPOTS_AREA_SOURCE_LAYER = 'spot_areas';

/** The server sends nothing below the first (internal/httpapi's spotsMinZoom) — "Show in this
 *  area" covers the zooms down to Region's. It's the trails' zoom (style.ts's PATHS_MIN_ZOOM), so
 *  places and trails appear together. Past the second, its tiles serve every zoom above by
 *  overzooming, the way the tracks tiles do past z14. */
export const SPOTS_MIN_ZOOM = PATHS_MIN_ZOOM;

/** The lowest zoom "Show in this area" (ShowInArea.tsx) offers places at, where a screen is
 *  about a metro area (~100 km across). Below it a view spans a state or more, where the dense
 *  categories run into the request's cap and the rest are too small to tell apart. */
export const SPOTS_IN_AREA_MIN_ZOOM = 10;
const SPOTS_MAX_ZOOM = 14;

const SPOTS_TILE_URL = `${API_BASE_URL}${TILES_V1}/spots/{z}/{x}/{y}.mvt`;

export type SpotCategory = 'playground' | 'dog_park' | 'monument' | 'viewpoint' | 'history';

/** Every category, in the order the Layers menu lists them. */
export const SPOT_CATEGORIES: readonly SpotCategory[] = ['playground', 'dog_park', 'monument', 'viewpoint', 'history'];

/** Lucide has no seesaw, so this one is drawn in its grid and stroke: a plank tilted over an
 *  A-frame, a handle at each end, the ground under it. */
const Seesaw = createLucideIcon('seesaw', [
  ['path', { d: 'M2 15 22 9', key: 'plank' }],
  ['path', { d: 'm8 20 4-8 4 8', key: 'frame' }],
  ['path', { d: 'M5 14v-3', key: 'handle-low' }],
  ['path', { d: 'M19 10V7', key: 'handle-high' }],
  ['path', { d: 'M3 20h18', key: 'ground' }],
]);

const CATEGORY_ICONS: Record<SpotCategory, ComponentType<LucideProps>> = {
  playground: Seesaw,
  dog_park: Dog,
  monument: Landmark,
  viewpoint: Binoculars,
  history: Castle,
};

/** One place as `GET /v1/spots` returns it (api.ts), text fields absent when OSM has none. */
export interface SpotInArea {
  id: number;
  category: SpotCategory;
  name?: string;
  address?: string;
  description?: string;
  inscription?: string;
  memorial?: string;
  start_date?: string;
  wikipedia?: string;
  lon: number;
  lat: number;
}

/** One spot as the popup needs it — a tile feature's properties, typed. */
export interface Spot {
  id: number;
  category: SpotCategory;
  name: string | null;
  address: string | null;
  /** OSM's text about it, each null when it has none — see SpotPopup. */
  description: string | null;
  inscription: string | null;
  memorial: string | null;
  startDate: string | null;
  /** OSM's `wikipedia` tag, "lang:Article title". */
  wikipedia: string | null;
  lon: number;
  lat: number;
}

// The marker: a round badge, the category icon in ink on white, ringed in the accent — or, for
// a place the account has captured (FR-15.6), white on the accent, ringed darker. Fixed colors,
// not the theme's: the badge carries its own background, so it reads the same on either basemap.
const INK = '#202b25'; // --fm-ink
const ACCENT = '#b07e2e'; // --fm-accent
const ACCENT_DARK = '#7d5820';
const WHITE = '#ffffff';
const BADGE_PX = 30;
const PIXEL_RATIO = 2;

function imageName(category: SpotCategory, captured: boolean): string {
  return captured ? `spot-captured-${category}` : `spot-${category}`;
}

/** Whether a feature's id is among the account's captures. */
function isCaptured(captured: readonly number[]): ExpressionSpecification {
  return ['in', ['to-number', ['get', 'id']], ['literal', [...captured]]];
}

/** Each badge's image: the captured one when its id is among the account's captures. */
function iconImage(captured: readonly number[]): ExpressionSpecification {
  return [
    'case',
    isCaptured(captured),
    ['concat', 'spot-captured-', ['get', 'category']],
    ['concat', 'spot-', ['get', 'category']],
  ];
}

/** The icon's own SVG children, rendered once from its Lucide component, so the map's icon is
 *  the same drawing the rest of the app uses. */
function iconMarkup(Icon: ComponentType<LucideProps>): string {
  const host = document.createElement('div');
  const root = createRoot(host);
  flushSync(() => root.render(createElement(Icon)));
  const markup = host.querySelector('svg')?.innerHTML ?? '';
  root.unmount();
  return markup;
}

function badgeSVG(icon: string, captured: boolean): string {
  const [fill, ring, ink] = captured ? [ACCENT, ACCENT_DARK, WHITE] : [WHITE, ACCENT, INK];
  // Lucide draws in a 24-unit box; 16 of the badge's 30 units leaves a comfortable margin.
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" width="${BADGE_PX * PIXEL_RATIO}" height="${BADGE_PX * PIXEL_RATIO}" viewBox="0 0 30 30">` +
    `<circle cx="15" cy="15" r="13.5" fill="${fill}" stroke="${ring}" stroke-width="2"/>` +
    `<g transform="translate(7 7) scale(${16 / 24})" fill="none" stroke="${ink}" stroke-width="2.25" stroke-linecap="round" stroke-linejoin="round">${icon}</g>` +
    `</svg>`
  );
}

async function rasterize(svg: string): Promise<ImageData> {
  const img = new Image();
  img.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  await img.decode();
  const size = BADGE_PX * PIXEL_RATIO;
  const canvas = document.createElement('canvas');
  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('no 2d canvas');
  ctx.drawImage(img, 0, 0, size, size);
  return ctx.getImageData(0, 0, size, size);
}

/**
 * The badge images, plain and captured for each of the five categories, drawn once per page. Module-level because a theme swap (`setStyle`)
 * drops a style's images along with its layers, and they're added back from here — the drawing
 * is asynchronous (an SVG has to decode before it can be drawn), the adding isn't.
 */
let images: Map<string, ImageData> | null = null;
let imagesLoading: Promise<void> | null = null;

function loadImages(): Promise<void> {
  imagesLoading ??= (async () => {
    // Out of the caller's stack first: ensureSpotsLayer runs inside a React effect, where
    // iconMarkup's flushSync can't render — the first icon came out blank.
    await Promise.resolve();
    const drawn = new Map<string, ImageData>();
    for (const category of SPOT_CATEGORIES) {
      const icon = iconMarkup(CATEGORY_ICONS[category]);
      for (const captured of [false, true]) {
        drawn.set(imageName(category, captured), await rasterize(badgeSVG(icon, captured)));
      }
    }
    images = drawn;
  })();
  return imagesLoading;
}

/** Which categories each map was last asked to show — what a layer added later (the first
 *  call's, once the images are drawn, or one re-added after a theme swap) starts with. */
const spotsWanted = new WeakMap<MapLibreMap, readonly SpotCategory[]>();

/** The ids of the places the account has captured, per map — what a badge layer added later
 *  starts with. */
const capturedIds = new WeakMap<MapLibreMap, readonly number[]>();

/** "Show in this area"'s places for each map, kept so a theme swap puts them back. */
const inAreaData = new WeakMap<MapLibreMap, FeatureCollection<Point>>();

const EMPTY: FeatureCollection<Point> = { type: 'FeatureCollection', features: [] };

/**
 * Adds the spots sources, their images and their layers if they aren't already there —
 * idempotent for the same reason ensureTrackLayer is (tracks.ts): `styledata` re-runs it after
 * every theme swap. The first call on a page returns before the layers exist, and adds them
 * once the badge images are drawn, showing whatever categories were asked for last by then.
 */
export function ensureSpotsLayer(map: MapLibreMap, categories: readonly SpotCategory[]): void {
  spotsWanted.set(map, categories);
  if (!images) {
    void loadImages().then(() => {
      // The map may have been removed (the page moved on) while the images were drawn.
      if (map.getStyle()) ensureSpotsLayer(map, spotsWanted.get(map) ?? categories);
    });
    return;
  }
  for (const [name, image] of images) {
    if (!map.hasImage(name)) map.addImage(name, image, { pixelRatio: PIXEL_RATIO });
  }
  if (!map.getSource(SPOTS_SOURCE_ID)) {
    map.addSource(SPOTS_SOURCE_ID, {
      type: 'vector',
      tiles: [versionedTileURL(SPOTS_TILE_URL)],
      minzoom: SPOTS_MIN_ZOOM,
      maxzoom: SPOTS_MAX_ZOOM,
      promoteId: 'id',
    });
  }
  if (!map.getSource(SPOTS_IN_AREA_SOURCE_ID)) {
    map.addSource(SPOTS_IN_AREA_SOURCE_ID, { type: 'geojson', data: inAreaData.get(map) ?? EMPTY, promoteId: 'id' });
  }
  addAreaLayers(map);
  for (const [id, source, sourceLayer, minzoom, maxzoom] of [
    [SPOTS_IN_AREA_LAYER_ID, SPOTS_IN_AREA_SOURCE_ID, undefined, SPOTS_IN_AREA_MIN_ZOOM, SPOTS_MIN_ZOOM],
    [SPOTS_LAYER_ID, SPOTS_SOURCE_ID, SPOTS_SOURCE_LAYER, SPOTS_MIN_ZOOM, 24],
  ] as const) {
    if (map.getLayer(id)) continue;
    map.addLayer(
      {
        id,
        type: 'symbol',
        source,
        ...(sourceLayer ? { 'source-layer': sourceLayer } : {}),
        minzoom,
        maxzoom,
        layout: {
          visibility: 'none',
          'icon-image': iconImage(capturedIds.get(map) ?? []),
          // Every spot is drawn, however close to another: a hidden playground is a missed one.
          'icon-allow-overlap': true,
          'icon-ignore-placement': true,
        },
      },
      layerAbove(map, id),
    );
  }
  attachSpotInteractivity(map);
  setSpotsVisible(map, categories);
}

/** The first of SPOTS_LAYER_IDS above `id` that's on the map — where a layer re-added after a
 *  style swap goes back in, so the stack keeps its order. */
function layerAbove(map: MapLibreMap, id: string): string | undefined {
  return SPOTS_LAYER_IDS.slice(SPOTS_LAYER_IDS.indexOf(id) + 1).find((next) => map.getLayer(next));
}

/**
 * Each spot's area under its badge, from the tiles' `spot_areas`: a faint accent fill, edged
 * with a solid line where it's the place's OSM outline and a dashed one where it's the 30 m
 * circle a place mapped as a single node stands in with (the tile's `circle`). Two line layers
 * because a dash pattern can't vary per feature. "Show in this area" draws no areas: below the
 * tiles' zoom a playground is a pixel.
 */
function addAreaLayers(map: MapLibreMap): void {
  if (!map.getLayer(SPOTS_AREA_FILL_LAYER_ID)) {
    map.addLayer(
      {
        id: SPOTS_AREA_FILL_LAYER_ID,
        type: 'fill',
        source: SPOTS_SOURCE_ID,
        'source-layer': SPOTS_AREA_SOURCE_LAYER,
        minzoom: SPOTS_MIN_ZOOM,
        layout: { visibility: 'none' },
        paint: { 'fill-color': ACCENT, 'fill-opacity': 0.15 },
      },
      layerAbove(map, SPOTS_AREA_FILL_LAYER_ID),
    );
  }
  for (const id of [SPOTS_AREA_LINE_LAYER_ID, SPOTS_CIRCLE_LINE_LAYER_ID]) {
    if (map.getLayer(id)) continue;
    map.addLayer(
      {
        id,
        type: 'line',
        source: SPOTS_SOURCE_ID,
        'source-layer': SPOTS_AREA_SOURCE_LAYER,
        minzoom: SPOTS_MIN_ZOOM,
        layout: { visibility: 'none', 'line-join': 'round' },
        paint: {
          'line-color': ACCENT,
          'line-width': ['interpolate', ['linear'], ['zoom'], SPOTS_MIN_ZOOM, 1, 17, 2],
          ...(id === SPOTS_CIRCLE_LINE_LAYER_ID ? { 'line-dasharray': [2, 2] } : {}),
        },
      },
      layerAbove(map, id),
    );
  }
}

/**
 * Shows the chosen categories (the Layers menu, overlays.ts) and hides the rest — a filter on
 * every layer, and every layer hidden when none is chosen. The tiles' layers also leave out a
 * retired place (ADR-0027) unless the account captured it; "Show in this area" gets only those
 * from the server already. Diffs first, like mapMode.ts's setVisible: a bare setLayoutProperty or
 * setFilter would start the styledata loop its comment describes.
 */
export function setSpotsVisible(map: MapLibreMap, categories: readonly SpotCategory[]): void {
  spotsWanted.set(map, categories);
  const visibility = categories.length > 0 ? 'visible' : 'none';
  const byCategory: FilterSpecification = ['in', ['get', 'category'], ['literal', [...categories]]];
  const shown: FilterSpecification = [
    'all',
    byCategory,
    ['any', ['!', ['to-boolean', ['get', 'retired']]], isCaptured(capturedIds.get(map) ?? [])],
  ];
  const filters: Record<string, FilterSpecification> = {
    [SPOTS_AREA_FILL_LAYER_ID]: shown,
    [SPOTS_AREA_LINE_LAYER_ID]: ['all', ['!', ['get', 'circle']], shown],
    [SPOTS_CIRCLE_LINE_LAYER_ID]: ['all', ['get', 'circle'], shown],
    [SPOTS_IN_AREA_LAYER_ID]: byCategory,
    [SPOTS_LAYER_ID]: shown,
  };
  for (const id of SPOTS_LAYER_IDS) {
    if (!map.getLayer(id)) continue;
    if (map.getLayoutProperty(id, 'visibility') !== visibility) map.setLayoutProperty(id, 'visibility', visibility);
    if (JSON.stringify(map.getFilter(id) ?? null) !== JSON.stringify(filters[id])) map.setFilter(id, filters[id]);
  }
}

/** Draws the account's captured places (FR-15.6) with the captured badge, the rest plain, and
 *  shows the retired ones among them (setSpotsVisible). */
export function setSpotsCaptured(map: MapLibreMap, captured: readonly number[]): void {
  capturedIds.set(map, captured);
  const image = iconImage(captured);
  for (const id of [SPOTS_IN_AREA_LAYER_ID, SPOTS_LAYER_ID]) {
    if (!map.getLayer(id)) continue;
    if (JSON.stringify(map.getLayoutProperty(id, 'icon-image') ?? null) !== JSON.stringify(image)) {
      map.setLayoutProperty(id, 'icon-image', image);
    }
  }
  const categories = spotsWanted.get(map);
  if (categories) setSpotsVisible(map, categories);
}

/** Draws "Show in this area"'s places (ShowInArea.tsx), replacing the last ones; null clears. */
export function setSpotsInArea(map: MapLibreMap, spots: readonly SpotInArea[] | null): void {
  const data: FeatureCollection<Point> = {
    type: 'FeatureCollection',
    features: (spots ?? []).map(({ lon, lat, ...properties }) => ({
      type: 'Feature',
      geometry: { type: 'Point', coordinates: [lon, lat] },
      properties: { ...properties, lon, lat },
    })),
  };
  inAreaData.set(map, data);
  (map.getSource(SPOTS_IN_AREA_SOURCE_ID) as GeoJSONSource | undefined)?.setData(data);
}

/** The showing spot at a map point, if any — from the tiles or from "Show in this area".
 *  tracks.ts's click handler asks first, so a click on a spot doesn't also select, or unfocus,
 *  a track under it. */
export function spotAt(map: MapLibreMap, point: { x: number; y: number }): Spot | null {
  const layers = [SPOTS_LAYER_ID, SPOTS_IN_AREA_LAYER_ID].filter(
    (id) => map.getLayer(id) && map.getLayoutProperty(id, 'visibility') !== 'none',
  );
  if (layers.length === 0) return null;
  const hit = map.queryRenderedFeatures([point.x, point.y], { layers })[0];
  return hit ? toSpot(hit) : null;
}

/** A tile feature's text property: absent (OSM had none) is null, as is an empty string. */
function text(value: unknown): string | null {
  return typeof value === 'string' && value ? value : null;
}

function toSpot(feature: MapGeoJSONFeature): Spot {
  const p = feature.properties;
  return {
    id: Number(p.id),
    category: p.category as SpotCategory,
    name: text(p.name),
    address: text(p.address),
    description: text(p.description),
    inscription: text(p.inscription),
    memorial: text(p.memorial),
    startDate: text(p.start_date),
    wikipedia: text(p.wikipedia),
    lon: Number(p.lon),
    lat: Number(p.lat),
  };
}

let onSpotClick: ((spot: Spot | null) => void) | null = null;

/** Set once from MapView: a click on a spot opens its popup (SpotPopup), a click anywhere else
 *  on the map closes it (null). */
export function setSpotClickHandler(next: (spot: Spot | null) => void): void {
  onSpotClick = next;
}

// Once per map instance, like tracks.ts's interactiveMaps: a layer-scoped listener keeps
// matching the layer re-added under the same id after a theme swap.
const interactiveMaps = new WeakSet<MapLibreMap>();

function attachSpotInteractivity(map: MapLibreMap): void {
  if (interactiveMaps.has(map)) return;
  interactiveMaps.add(map);
  for (const id of [SPOTS_LAYER_ID, SPOTS_IN_AREA_LAYER_ID]) {
    map.on('mouseenter', id, () => {
      map.getCanvas().style.cursor = 'pointer';
    });
    map.on('mouseleave', id, () => {
      map.getCanvas().style.cursor = '';
    });
  }
  // One map-wide handler deciding both, rather than a layer click plus the popup's own
  // closeOnClick: those two fire on the same click, and a click on a second spot while one
  // popup was open opened the new one and closed it again.
  map.on('click', (e) => onSpotClick?.(spotAt(map, e.point)));
}
