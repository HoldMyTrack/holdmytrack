import { layers, namedFlavor } from '@protomaps/basemaps';
import type { FilterSpecification, LayerSpecification, LineLayerSpecification, StyleSpecification } from 'maplibre-gl';
import { GLYPHS_PATH, PMTILES_PATH, SPRITE_BASE_PATH } from './config';

/**
 * Style construction, kept pure and DOM-free on purpose.
 *
 * This module must not touch React, `window`, or a live Map. It is the seam the
 * print pipeline needs: the same function has to run headlessly server-side to
 * render an identical map at print DPI (IMPLEMENTATION.md §5.5).
 * Anything browser-shaped added here breaks that without breaking the app, which
 * is the worst kind of regression, so the rule is worth keeping strictly — hence
 * `origin` being a parameter rather than a `window.location` read.
 */

/** Flavors shipped by @protomaps/basemaps 5.7.2, each with a matching sprite sheet. */
export const FLAVORS = ['light', 'dark', 'white', 'black', 'grayscale'] as const;
export type Flavor = (typeof FLAVORS)[number];

export function isFlavor(value: string): value is Flavor {
  return (FLAVORS as readonly string[]).includes(value);
}

/** The flavors drawn light-on-dark — the ones the cream fog veil and the export watermark's
 *  light colors are for. */
export function isDarkFlavor(flavor: Flavor): boolean {
  return flavor === 'dark' || flavor === 'black';
}

/** The source id every basemap layer is bound to; fog and track layers will not reuse it. */
export const BASEMAP_SOURCE = 'protomaps';

/**
 * Mandatory attribution, not decoration: the Protomaps basemap is an ODbL
 * "Produced Work", so OSM credit has to be visible on the map and on any export.
 */
export const ATTRIBUTION =
  '<a href="https://protomaps.com">Protomaps</a> © <a href="https://openstreetmap.org">OpenStreetMap</a>';

/** Plain-text form of `ATTRIBUTION` — derived, not hand-duplicated, so the two can never say
 *  different things. `AttributionControl` renders the HTML above as a DOM overlay, which is
 *  fine for the live map but invisible to anything that reads pixels off a canvas instead
 *  (`exportMap.ts`'s PNG export bakes this string into the raster itself for exactly that
 *  reason — canvas `fillText` takes a plain string, not markup). */
export const ATTRIBUTION_TEXT = ATTRIBUTION.replace(/<[^>]+>/g, '');

export interface BuildStyleOptions {
  flavor: Flavor;
  /** Absolute origin, e.g. `https://cdn.example.com` — no trailing slash. */
  origin: string;
  /** Label language. Protomaps carries per-language name fields in the `places` layer. */
  lang?: string;
  pmtilesPath?: string;
  glyphsPath?: string;
  spriteBasePath?: string;
  /** Which of the path layers start visible: trails (`TRAIL_LAYER_IDS`) and bike paths
   *  (`BIKE_PATH_LAYER_IDS`). Both off by default, so the served style documents look the same
   *  as the stock basemap to a client that never flips them. */
  paths?: PathOverlays;
}

/**
 * The trails and bike paths toggle's layers (docs/SPEC.md FR-4.5). Protomaps already carries
 * every OSM path in the `roads` layer (`kind=path`, sorted by `kind_detail`), but the stock
 * style draws them all through `roads_other` as one hairline grey from z14, which reads as no
 * paths at all. These draw cycleways and trails over that same geometry from lower zoom, so
 * hiding them just falls back to the stock look. The Android app toggles the same ids by
 * name (`MapPaths.LAYER_IDS`), so they are part of the served style's contract.
 */
export const PATH_LAYER_IDS = [
  'paths_cycleway',
  'paths_trail',
  'paths_track',
  'paths_bridges_cycleway',
  'paths_bridges_trail',
  'paths_bridges_track',
] as const;

/** The three kinds the web's Overlays menu shows separately (overlays.ts). Android toggles all
 *  six together. */
export const TRAIL_LAYER_IDS = ['paths_trail', 'paths_bridges_trail'] as const;
export const TRACK_LAYER_IDS = ['paths_track', 'paths_bridges_track'] as const;
export const BIKE_PATH_LAYER_IDS = ['paths_cycleway', 'paths_bridges_cycleway'] as const;

/** Which path layers are showing. */
export interface PathOverlays {
  trails: boolean;
  tracks: boolean;
  bikePaths: boolean;
}

/** Sidewalks, crossings, steps and pedestrian areas stay on the stock grey on purpose: in a
 *  city they outnumber real paths and would bury them. Tracks — OSM's `highway=track`, dirt,
 *  farm and forest roads — are their own layer: vehicle-width, often the very thing a rider
 *  looks for or avoids, and where OSM maps every field and forestry road (much of Europe) far
 *  more of them than trails. */
const TRAIL_DETAILS = ['path', 'footway', 'bridleway'];
const TRACK_DETAIL = 'track';

/** Cool for cycleways, green for trails, brown for tracks, all clear of the ochre activity
 *  tracks (tracks.ts). The monochrome flavors stay monochrome; there the three differ by weight
 *  and dash. */
const PATH_COLORS: Record<Flavor, { cycleway: string; trail: string; track: string }> = {
  light: { cycleway: '#1f7fa8', trail: '#4f7a3a', track: '#8a5a2b' },
  dark: { cycleway: '#5cbfe0', trail: '#8fbf6a', track: '#c9955e' },
  white: { cycleway: '#4a4a4a', trail: '#6e6e6e', track: '#5c5c5c' },
  grayscale: { cycleway: '#3d3d3d', trail: '#666666', track: '#555555' },
  black: { cycleway: '#c4c4c4', trail: '#9a9a9a', track: '#b0b0b0' },
};

function pathLayers(flavor: Flavor, bridges: boolean, visible: PathOverlays): LineLayerSpecification[] {
  const colors = PATH_COLORS[flavor];
  // Legacy filter syntax, the same as the stock road layers use.
  const structure = bridges ? [['has', 'is_bridge']] : [['!has', 'is_tunnel'], ['!has', 'is_bridge']];
  const filter = (detail: unknown[]) =>
    ['all', ...structure, ['==', 'kind', 'path'], detail] as unknown as FilterSpecification;
  const prefix = bridges ? 'paths_bridges_' : 'paths_';
  const visibility = (on: boolean) => (on ? 'visible' : 'none');
  // Tracks first, so a trail sharing a stretch with one draws over it.
  return [
    {
      id: `${prefix}track`,
      type: 'line',
      source: BASEMAP_SOURCE,
      'source-layer': 'roads',
      minzoom: 13,
      filter: filter(['==', 'kind_detail', TRACK_DETAIL]),
      layout: { visibility: visibility(visible.tracks) },
      paint: {
        'line-color': colors.track,
        // Longer dashes and a wider line than a trail: a road a vehicle fits on.
        'line-dasharray': [3, 1.5],
        'line-width': ['interpolate', ['exponential', 1.6], ['zoom'], 13, 1, 18, 3.5],
      },
    },
    {
      id: `${prefix}trail`,
      type: 'line',
      source: BASEMAP_SOURCE,
      'source-layer': 'roads',
      minzoom: 13,
      filter: filter(['in', 'kind_detail', ...TRAIL_DETAILS]),
      layout: { visibility: visibility(visible.trails) },
      paint: {
        'line-color': colors.trail,
        'line-dasharray': [2, 1],
        'line-width': ['interpolate', ['exponential', 1.6], ['zoom'], 13, 0.8, 18, 3],
      },
    },
    {
      id: `${prefix}cycleway`,
      type: 'line',
      source: BASEMAP_SOURCE,
      'source-layer': 'roads',
      minzoom: 12,
      filter: filter(['==', 'kind_detail', 'cycleway']),
      layout: { visibility: visibility(visible.bikePaths), 'line-cap': 'round' },
      paint: {
        'line-color': colors.cycleway,
        'line-width': ['interpolate', ['exponential', 1.6], ['zoom'], 12, 1, 18, 4],
      },
    },
  ];
}

/** Stock layers with the path layers spliced in right after the one that draws the same
 *  features, so bridges, casings and labels keep stacking over them as they did. */
function withPathLayers(base: LayerSpecification[], flavor: Flavor, visible: PathOverlays): LayerSpecification[] {
  return base.flatMap((layer) => {
    if (layer.id === 'roads_other') return [layer, ...pathLayers(flavor, false, visible)];
    if (layer.id === 'roads_bridges_other') return [layer, ...pathLayers(flavor, true, visible)];
    return [layer];
  });
}

export function buildStyle(options: BuildStyleOptions): StyleSpecification {
  const {
    flavor,
    origin,
    lang = 'en',
    pmtilesPath = PMTILES_PATH,
    glyphsPath = GLYPHS_PATH,
    spriteBasePath = SPRITE_BASE_PATH,
    paths = { trails: false, tracks: false, bikePaths: false },
  } = options;

  const base = origin.replace(/\/$/, '');

  return {
    version: 8,
    glyphs: `${base}${glyphsPath}`,
    // One sheet per flavor; MapLibre appends `.json`/`.png` and picks @2x itself.
    // Must be absolute: MapLibre 6 throws on a relative sprite URL.
    sprite: `${base}${spriteBasePath}/${flavor}`,
    sources: {
      // `url` (not `tiles`) lets the pmtiles Protocol answer with TileJSON built
      // from the archive header, so MapLibre learns the real bounds and zoom
      // range and never requests a tile the archive cannot contain.
      [BASEMAP_SOURCE]: {
        type: 'vector',
        url: `pmtiles://${base}${pmtilesPath}`,
        attribution: ATTRIBUTION,
      },
    },
    layers: withPathLayers(layers(BASEMAP_SOURCE, namedFlavor(flavor), { lang }), flavor, paths),
  };
}
