import type { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL } from '../api';
import { readOverlays, viewFor, type CoverageOverlays, type OverlaySource } from './coverageStyle';
import { versionedTileURL } from './coverageVersion';
import type { MapMode } from './mapMode';
import { SATELLITE_LAYER_ID } from './style';

/**
 * Fog of War and Heatmap on a live map, from the overlays the style carries (coverageStyle.ts):
 * this module adds what the style describes and switches its views, and decides nothing about
 * how either mode looks. Every call is idempotent, since `styledata` fires on every theme
 * `setStyle`, which discards custom layers.
 */

type LayoutProperty = Parameters<MapLibreMap['getLayoutProperty']>[1];
type PaintProperty = Parameters<MapLibreMap['getPaintProperty']>[1];

/** The overlays read from each loaded style. `getStyle()` deep-copies the whole document and this
 *  runs several times on every `styledata`, so they're read once per style. Keyed by MapLibre's
 *  style object, which `setStyle` replaces: the app swaps styles only with `diff: false`
 *  (useMapLayers.ts), which never edits one in place. */
const overlaysByStyle = new WeakMap<object, CoverageOverlays>();

/** The overlays of the style `map` has, or null before one with them has loaded. */
export function coverageOverlays(map: MapLibreMap): CoverageOverlays | null {
  const style = map.style as object | undefined;
  if (!style) return null;
  const cached = overlaysByStyle.get(style);
  if (cached) return cached;
  const overlays = readOverlays(map.getStyle()?.metadata);
  // Not cached while missing: a style still loading has none yet.
  if (overlays) overlaysByStyle.set(style, overlays);
  return overlays;
}

function tileURL(source: OverlaySource): string {
  return versionedTileURL(`${API_BASE_URL}${source.tiles}`);
}

/**
 * Adds the overlay sources and layers not yet on the map, in the style's order, beneath
 * `beforeId` (layers.ts), so labels stay over the veil. Added before the paths and the tracks,
 * which then stack over them.
 */
export function ensureCoverageLayers(map: MapLibreMap, beforeId: string | undefined): void {
  const overlays = coverageOverlays(map);
  if (!overlays) return;
  for (const [id, source] of Object.entries(overlays.sources)) {
    if (map.getSource(id)) continue;
    map.addSource(id, {
      type: 'vector',
      tiles: [tileURL(source)],
      minzoom: source.minzoom,
      maxzoom: source.maxzoom,
      ...(source.attribution && { attribution: source.attribution }),
    });
  }
  for (const layer of overlays.layers) {
    if (!map.getLayer(layer.id)) map.addLayer(layer, beforeId);
  }
}

/**
 * Applies `mode`'s view, over imagery when `satellite` is on and the style has it. Diffs before
 * each set: `setLayoutProperty` and `setPaintProperty` re-render unconditionally, and this runs
 * on every `styledata` (docs/DEVELOPMENT.md, the setFilter/setLayoutProperty gotcha).
 */
export function setCoverageView(map: MapLibreMap, mode: MapMode, satellite: boolean): void {
  const overlays = coverageOverlays(map);
  if (!overlays) return;
  const view = viewFor(overlays, mode, satellite && map.getLayer(SATELLITE_LAYER_ID) !== undefined);
  for (const [id, props] of Object.entries(view.layout)) {
    if (!map.getLayer(id)) continue;
    for (const [prop, value] of Object.entries(props) as [LayoutProperty, unknown][]) {
      if (map.getLayoutProperty(id, prop) !== value) map.setLayoutProperty(id, prop, value as never);
    }
  }
  for (const [id, props] of Object.entries(view.paint)) {
    if (!map.getLayer(id)) continue;
    for (const [prop, value] of Object.entries(props) as [PaintProperty, unknown][]) {
      if (map.getPaintProperty(id, prop) !== value) map.setPaintProperty(id, prop, value as never);
    }
  }
}

/** The lowest overlay layer on the map, which paths.ts lifts the trails above. */
export function firstCoverageLayerId(map: MapLibreMap): string | undefined {
  return coverageOverlays(map)?.layers[0]?.id;
}

/** Re-points every overlay source at the current coverage version (coverageVersion.ts), so
 *  MapLibre refetches every tile; the caller sets the new version first. A source not yet on the
 *  map is created at the current version by ensureCoverageLayers. */
export function refreshCoverageLayers(map: MapLibreMap): void {
  const overlays = coverageOverlays(map);
  if (!overlays) return;
  for (const [id, source] of Object.entries(overlays.sources)) {
    (map.getSource(id) as { setTiles?: (tiles: string[]) => unknown } | undefined)?.setTiles?.([tileURL(source)]);
  }
}
