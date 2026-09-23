import { layers, namedFlavor } from '@protomaps/basemaps';
import type { StyleSpecification } from 'maplibre-gl';
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
}

export function buildStyle(options: BuildStyleOptions): StyleSpecification {
  const {
    flavor,
    origin,
    lang = 'en',
    pmtilesPath = PMTILES_PATH,
    glyphsPath = GLYPHS_PATH,
    spriteBasePath = SPRITE_BASE_PATH,
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
    layers: layers(BASEMAP_SOURCE, namedFlavor(flavor), { lang }),
  };
}
