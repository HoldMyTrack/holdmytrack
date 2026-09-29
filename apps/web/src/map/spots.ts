import { createElement, type ComponentType } from 'react';
import { flushSync } from 'react-dom';
import { createRoot } from 'react-dom/client';
import type { Map as MapLibreMap, MapGeoJSONFeature, VectorTileSource } from 'maplibre-gl';
import { Binoculars, Castle, Dog, FerrisWheel, Landmark, type LucideProps } from 'lucide-react';
import { API_BASE_URL, TILES_V1 } from '../api';
import { versionedTileURL } from './coverageVersion';

/**
 * The Spots layer (IMPLEMENTATION.md §4.25, ADR-0021): outdoor places from OpenStreetMap, one
 * icon each, drawn in every map mode behind the Show POI toggle. The tiles come from
 * `/tiles/v1/spots` with the caller's own `visited` flag per place, cached under the account's
 * tile version like every other per-user tile (coverageVersion.ts).
 *
 * Unlike every other overlay it's a symbol layer, added on top of the whole stack rather than
 * beneath the basemap's labels (layers.ts): a spot is something to find on the map, so the fog
 * veil mustn't bury it, and a label crossing it is less of a loss than the icon.
 */
export const SPOTS_SOURCE_ID = 'spots';
export const SPOTS_LAYER_ID = 'spots-icons';
const SPOTS_SOURCE_LAYER = 'spots'; // ST_AsMVT(t, 'spots', ...) in the backend query

/** The server sends nothing below it (internal/httpapi's spotsMinZoom). One zoom's tiles serve
 *  every zoom above it by overzooming, the way the tracks tiles do past z14. */
const SPOTS_ZOOM = 14;

const SPOTS_TILE_URL = `${API_BASE_URL}${TILES_V1}/spots/{z}/{x}/{y}.mvt`;

export type SpotCategory = 'playground' | 'dog_park' | 'monument' | 'viewpoint' | 'history';

const CATEGORIES: readonly SpotCategory[] = ['playground', 'dog_park', 'monument', 'viewpoint', 'history'];

const CATEGORY_ICONS: Record<SpotCategory, ComponentType<LucideProps>> = {
  playground: FerrisWheel,
  dog_park: Dog,
  monument: Landmark,
  viewpoint: Binoculars,
  history: Castle,
};

/** One spot as the popup needs it — a tile feature's properties, typed. */
export interface Spot {
  id: number;
  category: SpotCategory;
  name: string | null;
  address: string | null;
  lon: number;
  lat: number;
  visited: boolean;
}

// The marker: a round badge, the category icon inside. Not visited: the icon in ink on white,
// ringed in the accent. Visited: filled with the accent, the icon in white. Fixed colors, not
// the theme's: the badge carries its own background, so it reads the same on either basemap.
const INK = '#202b25'; // --fm-ink
const ACCENT = '#b07e2e'; // --fm-accent
const WHITE = '#ffffff';
const BADGE_PX = 30;
const PIXEL_RATIO = 2;

function imageName(category: SpotCategory, visited: boolean): string {
  return `spot-${category}-${visited ? 'visited' : 'unvisited'}`;
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

function badgeSVG(icon: string, visited: boolean): string {
  const fill = visited ? ACCENT : WHITE;
  const stroke = visited ? WHITE : INK;
  // Lucide draws in a 24-unit box; 16 of the badge's 30 units leaves a comfortable margin.
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" width="${BADGE_PX * PIXEL_RATIO}" height="${BADGE_PX * PIXEL_RATIO}" viewBox="0 0 30 30">` +
    `<circle cx="15" cy="15" r="13.5" fill="${fill}" stroke="${ACCENT}" stroke-width="2"/>` +
    `<g transform="translate(7 7) scale(${16 / 24})" fill="none" stroke="${stroke}" stroke-width="2.25" stroke-linecap="round" stroke-linejoin="round">${icon}</g>` +
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
 * The ten badge images, drawn once per page. Module-level because a theme swap (`setStyle`)
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
    for (const category of CATEGORIES) {
      const icon = iconMarkup(CATEGORY_ICONS[category]);
      for (const visited of [false, true]) {
        drawn.set(imageName(category, visited), await rasterize(badgeSVG(icon, visited)));
      }
    }
    images = drawn;
  })();
  return imagesLoading;
}

/** What visibility each map was last asked for — what a layer added later (the first call's,
 *  once the images are drawn, or one re-added after a theme swap) starts with. */
const spotsWanted = new WeakMap<MapLibreMap, boolean>();

/**
 * Adds the spots source, its images and its layer if they aren't already there — idempotent
 * for the same reason ensureTrackLayer is (tracks.ts): `styledata` re-runs it after every theme
 * swap. The first call on a page returns before the layer exists, and adds it once the badge
 * images are drawn, with whatever visibility was asked for last by then.
 */
export function ensureSpotsLayer(map: MapLibreMap, visible: boolean): void {
  spotsWanted.set(map, visible);
  if (!images) {
    void loadImages().then(() => {
      // The map may have been removed (the page moved on) while the images were drawn.
      if (map.getStyle()) ensureSpotsLayer(map, spotsWanted.get(map) ?? visible);
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
      minzoom: SPOTS_ZOOM,
      maxzoom: SPOTS_ZOOM,
      promoteId: 'id',
    });
  }
  if (!map.getLayer(SPOTS_LAYER_ID)) {
    map.addLayer({
      id: SPOTS_LAYER_ID,
      type: 'symbol',
      source: SPOTS_SOURCE_ID,
      'source-layer': SPOTS_SOURCE_LAYER,
      minzoom: SPOTS_ZOOM,
      layout: {
        visibility: visible ? 'visible' : 'none',
        'icon-image': ['concat', 'spot-', ['get', 'category'], ['case', ['get', 'visited'], '-visited', '-unvisited']],
        // Every spot is drawn, however close to another: a hidden playground is a missed one.
        'icon-allow-overlap': true,
        'icon-ignore-placement': true,
        // The not-yet-visited on top — they're what the layer is for.
        'symbol-sort-key': ['case', ['get', 'visited'], 0, 1],
      },
    });
    attachSpotInteractivity(map);
  }
  setSpotsVisible(map, visible);
}

/** Shows or hides the layer. Diffs first, like mapMode.ts's setVisible — see its comment for
 *  the styledata loop a bare setLayoutProperty would start. */
export function setSpotsVisible(map: MapLibreMap, visible: boolean): void {
  spotsWanted.set(map, visible);
  if (!map.getLayer(SPOTS_LAYER_ID)) return;
  const next = visible ? 'visible' : 'none';
  if (map.getLayoutProperty(SPOTS_LAYER_ID, 'visibility') === next) return;
  map.setLayoutProperty(SPOTS_LAYER_ID, 'visibility', next);
}

/** Re-points the source at the current tile version, so visited marks from newly processed
 *  activities show — the caller (useCoverageRefresh.ts) has already set the new version. */
export function refreshSpotsLayer(map: MapLibreMap): void {
  (map.getSource(SPOTS_SOURCE_ID) as VectorTileSource | undefined)?.setTiles([versionedTileURL(SPOTS_TILE_URL)]);
}

/** The showing spot at a map point, if any — tracks.ts's click handler asks first, so a click
 *  on a spot doesn't also select, or unfocus, a track under it. */
export function spotAt(map: MapLibreMap, point: { x: number; y: number }): Spot | null {
  if (!map.getLayer(SPOTS_LAYER_ID) || map.getLayoutProperty(SPOTS_LAYER_ID, 'visibility') === 'none') return null;
  const hit = map.queryRenderedFeatures([point.x, point.y], { layers: [SPOTS_LAYER_ID] })[0];
  return hit ? toSpot(hit) : null;
}

function toSpot(feature: MapGeoJSONFeature): Spot {
  const p = feature.properties;
  return {
    id: Number(p.id),
    category: p.category as SpotCategory,
    name: typeof p.name === 'string' && p.name ? p.name : null,
    address: typeof p.address === 'string' && p.address ? p.address : null,
    lon: Number(p.lon),
    lat: Number(p.lat),
    visited: p.visited === true,
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
  map.on('mouseenter', SPOTS_LAYER_ID, () => {
    map.getCanvas().style.cursor = 'pointer';
  });
  map.on('mouseleave', SPOTS_LAYER_ID, () => {
    map.getCanvas().style.cursor = '';
  });
  // One map-wide handler deciding both, rather than a layer click plus the popup's own
  // closeOnClick: those two fire on the same click, and a click on a second spot while one
  // popup was open opened the new one and closed it again.
  map.on('click', (e) => onSpotClick?.(spotAt(map, e.point)));
}
