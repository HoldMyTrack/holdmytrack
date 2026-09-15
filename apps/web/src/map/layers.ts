import type { LayerSpecification, Map as MapLibreMap } from 'maplibre-gl';

/**
 * Layer-ordering helpers for the overlay stack that later phases add:
 * basemap fills and lines → fog raster → track lines → basemap labels.
 *
 * Nothing uses these yet. They exist now because the ordering rule is the part
 * that is awkward to retrofit: fog painted over labels buries every place name
 * and reads as a rendering bug, so both the fog raster and the track tiles have
 * to be inserted *beneath* the first symbol layer rather than appended.
 */

/**
 * The id of the first symbol (label) layer, which is the `beforeId` that fog and
 * track layers must be inserted against.
 *
 * Returns undefined for a style with no labels at all (e.g. `labelsOnly: false`
 * flavors or a style built before `layers()` ran), in which case callers should
 * append — there is nothing to stay beneath.
 */
export function firstSymbolLayerId(layers: readonly LayerSpecification[]): string | undefined {
  return layers.find((layer) => layer.type === 'symbol')?.id;
}

/**
 * Same question asked of a live map rather than a style object. Use this after
 * `styledata`, since a `setStyle` swap replaces the layer list wholesale.
 */
export function labelInsertionPoint(map: MapLibreMap): string | undefined {
  return firstSymbolLayerId(map.getStyle().layers);
}
