import type { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, TILES_V1 } from '../api';
import { CITY_MIN_ZOOM, COUNTRY_MAX_ZOOM, REGION_MAX_ZOOM, REGION_MIN_ZOOM } from './zoomTiers';

/**
 * The Fog of War raster layer (IMPLEMENTATION.md §4.2). Unlike tracks, Fog of War is never
 * scoped by the date range/TYPE/DISTANCE/hidden-track state that drives Normal mode — it
 * shows true all-time coverage, unconditionally (a place once cleared stays cleared, which is
 * the whole point of the mechanic). There is therefore no query to build or refresh: one
 * fixed tile URL, set once and never changed. See heatmap.ts, which has its own fixed URL for
 * the same reason, just a different fixed rolling window computed server-side.
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

// Same dark veil colour/opacity as the raster tier's own fog_colour/fog_opacity
// (internal/fog/raster.go's RenderFogPNG: #202b25 @ 0.82) — kept identical so nothing shifts
// hue when a pan/zoom crosses the Region/City boundary.
const FOG_FILL_COLOR = '#202b25';
const FOG_FILL_OPACITY = 0.82;

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
 */
export function ensureFogLayer(map: MapLibreMap, beforeId: string | undefined): void {
  if (!map.getSource(FOG_SOURCE_ID)) {
    map.addSource(FOG_SOURCE_ID, {
      type: 'raster',
      tiles: [FOG_TILE_URL],
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
      },
      beforeId,
    );
  }

  if (!map.getSource(COUNTRY_FOG_SOURCE_ID)) {
    map.addSource(COUNTRY_FOG_SOURCE_ID, {
      type: 'vector',
      tiles: [COUNTRY_FOG_TILE_URL],
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
        paint: { 'fill-color': FOG_FILL_COLOR, 'fill-opacity': FOG_FILL_OPACITY },
        layout: { visibility: 'none' },
      },
      beforeId,
    );
  }

  if (!map.getSource(REGION_FOG_SOURCE_ID)) {
    map.addSource(REGION_FOG_SOURCE_ID, {
      type: 'vector',
      tiles: [REGION_FOG_TILE_URL],
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
        paint: { 'fill-color': FOG_FILL_COLOR, 'fill-opacity': FOG_FILL_OPACITY },
        layout: { visibility: 'none' },
      },
      beforeId,
    );
  }
}
