import type { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, TILES_V1 } from '../api';
import { versionedTileURL } from './coverageVersion';
import { BOUNDARIES_ATTRIBUTION, CITY_MIN_ZOOM, COUNTRY_MAX_ZOOM, PIXELS_MAX_ZOOM, REGION_MAX_ZOOM, REGION_MIN_ZOOM } from './zoomTiers';

/**
 * The Fog of War raster layer (IMPLEMENTATION.md §4.2). Unlike tracks, Fog of War is never
 * scoped by the date range/TYPE/DISTANCE/hidden-track state that drives Normal mode — it
 * shows true all-time coverage, unconditionally (a place once cleared stays cleared, which is
 * the whole point of the mechanic). There is therefore no query to build: one fixed tile URL,
 * changed only by the account's tile version (coverageVersion.ts), which moves whenever its
 * coverage does. See heatmap.ts, which has its own fixed URL for the
 * same reason, just a different fixed rolling window computed server-side.
 *
 * Below city zoom (§4.2.4), this per-pixel raster is replaced entirely by the Country/Region
 * fill layers below — the two tiers are mutually exclusive by MapLibre `minzoom`/`maxzoom`,
 * not layered, so there's nothing here reconciling "unlocked" with "actually explored": at
 * country/region zoom you can't see fine-grained detail anyway, so the coarse reveal is
 * already the whole truth at that scale.
 */
export const FOG_SOURCE_ID = 'fog';
export const FOG_LAYER_ID = 'fog-raster';
export const COUNTRY_FOG_SOURCE_ID = 'country-fog';
export const COUNTRY_FOG_LAYER_ID = 'country-fog-fill';
export const REGION_FOG_SOURCE_ID = 'region-fog';
export const REGION_FOG_LAYER_ID = 'region-fog-fill';

const FOG_TILE_URL = `${API_BASE_URL}${TILES_V1}/fog/{z}/{x}/{y}.png`;
const COUNTRY_FOG_TILE_URL = `${API_BASE_URL}${TILES_V1}/country-fog/{z}/{x}/{y}.mvt`;
const REGION_FOG_TILE_URL = `${API_BASE_URL}${TILES_V1}/region-fog/{z}/{x}/{y}.mvt`;

// The same two veils as the raster tier's (internal/fog/raster.go's LightVeil/DarkVeil) — kept
// identical so nothing shifts hue when a pan/zoom crosses the Region/City boundary. Each is the
// opposite of the basemap it sits on: dark ink over a light map, a cream mist over a dark one.
const LIGHT_VEIL = { color: '#202b25', opacity: 0.82 };
const DARK_VEIL = { color: '#f7f4ec', opacity: 0.6 };

/** Which veil each map currently has, so a coverage refresh keeps it and a theme swap that
 *  somehow left the fog in place can still repaint it. */
const darkVeil = new WeakMap<MapLibreMap, boolean>();

/** The raster tile URL at the current coverage version, plus `theme=dark` for the cream veil
 *  (the server's fog.VeilForTheme). */
function fogRasterURL(dark: boolean): string {
  const url = versionedTileURL(FOG_TILE_URL);
  return dark ? `${url}${url.includes('?') ? '&' : '?'}theme=dark` : url;
}

/**
 * Adds the fog source and layer if not already present — idempotent for the same reason
 * ensureTrackLayer is (tracks.ts): `styledata` fires on every theme setStyle, which
 * discards custom layers, so this has to be safe to call repeatedly.
 *
 * `beforeId` is the same insertion point tracks uses (layers.ts) — beneath the basemap's
 * first symbol layer, so place labels stay legible over the fog (§4.2's own reasoning).
 * Layer order between fog and tracks matters too: fog is added first here and MapView adds
 * tracks after it, both with the same `beforeId`, which makes tracks paint *above* fog —
 * a cleared route should be visible through the veil, not hidden under it.
 *
 * `dark` picks the veil: the cream one on a dark basemap flavor (style.ts's isDarkFlavor).
 */
export function ensureFogLayer(map: MapLibreMap, beforeId: string | undefined, dark: boolean): void {
  const veil = dark ? DARK_VEIL : LIGHT_VEIL;
  const veilChanged = darkVeil.has(map) && darkVeil.get(map) !== dark;
  darkVeil.set(map, dark);

  if (!map.getSource(FOG_SOURCE_ID)) {
    map.addSource(FOG_SOURCE_ID, {
      type: 'raster',
      tiles: [fogRasterURL(dark)],
      tileSize: 512,
      minzoom: 0,
      maxzoom: 14,
    });
  }
  if (!map.getLayer(FOG_LAYER_ID)) {
    map.addLayer(
      {
        id: FOG_LAYER_ID,
        type: 'raster',
        source: FOG_SOURCE_ID,
        minzoom: CITY_MIN_ZOOM,
        layout: { visibility: 'none' }, // starts hidden — Normal is the default mode
        // Square pixels below z14, where each tile pixel shows 8 screen pixels wide however far out
        // the map is; smoothed from z14, where there's no finer tile and the z14 one is stretched
        // further with every zoom (ADR-0038).
        paint: { 'raster-resampling': ['step', ['zoom'], 'nearest', PIXELS_MAX_ZOOM, 'linear'] },
      },
      beforeId,
    );
  }

  if (!map.getSource(COUNTRY_FOG_SOURCE_ID)) {
    map.addSource(COUNTRY_FOG_SOURCE_ID, {
      type: 'vector',
      tiles: [versionedTileURL(COUNTRY_FOG_TILE_URL)],
      attribution: BOUNDARIES_ATTRIBUTION,
      minzoom: 0,
      maxzoom: COUNTRY_MAX_ZOOM,
    });
  }
  if (!map.getLayer(COUNTRY_FOG_LAYER_ID)) {
    map.addLayer(
      {
        id: COUNTRY_FOG_LAYER_ID,
        type: 'fill',
        source: COUNTRY_FOG_SOURCE_ID,
        'source-layer': 'countries',
        minzoom: 0,
        maxzoom: COUNTRY_MAX_ZOOM,
        paint: { 'fill-color': veil.color, 'fill-opacity': veil.opacity },
        layout: { visibility: 'none' },
      },
      beforeId,
    );
  }

  if (!map.getSource(REGION_FOG_SOURCE_ID)) {
    map.addSource(REGION_FOG_SOURCE_ID, {
      type: 'vector',
      tiles: [versionedTileURL(REGION_FOG_TILE_URL)],
      attribution: BOUNDARIES_ATTRIBUTION,
      minzoom: REGION_MIN_ZOOM,
      maxzoom: REGION_MAX_ZOOM,
    });
  }
  if (!map.getLayer(REGION_FOG_LAYER_ID)) {
    map.addLayer(
      {
        id: REGION_FOG_LAYER_ID,
        type: 'fill',
        source: REGION_FOG_SOURCE_ID,
        'source-layer': 'regions',
        minzoom: REGION_MIN_ZOOM,
        maxzoom: REGION_MAX_ZOOM,
        paint: { 'fill-color': veil.color, 'fill-opacity': veil.opacity },
        layout: { visibility: 'none' },
      },
      beforeId,
    );
  }

  if (veilChanged) {
    (map.getSource(FOG_SOURCE_ID) as { setTiles?: (tiles: string[]) => unknown } | undefined)?.setTiles?.([fogRasterURL(dark)]);
    for (const layerId of [COUNTRY_FOG_LAYER_ID, REGION_FOG_LAYER_ID]) {
      map.setPaintProperty(layerId, 'fill-color', veil.color);
      map.setPaintProperty(layerId, 'fill-opacity', veil.opacity);
    }
  }
}

/** Re-points this mode's sources at the current coverage version (coverageVersion.ts), so
 *  MapLibre refetches every tile — the caller sets the new version first. A no-op for a source
 *  that isn't on the map yet; ensureFogLayer will create it at the current version. */
export function refreshFogLayers(map: MapLibreMap): void {
  for (const [sourceId, url] of [
    [FOG_SOURCE_ID, fogRasterURL(darkVeil.get(map) ?? false)],
    [COUNTRY_FOG_SOURCE_ID, versionedTileURL(COUNTRY_FOG_TILE_URL)],
    [REGION_FOG_SOURCE_ID, versionedTileURL(REGION_FOG_TILE_URL)],
  ] as const) {
    (map.getSource(sourceId) as { setTiles?: (tiles: string[]) => unknown } | undefined)?.setTiles?.([url]);
  }
}
