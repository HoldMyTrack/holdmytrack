import type { ProjectionSpecification } from 'maplibre-gl';

/** The last zoom drawn as a globe; one zoom further in the map is flat. Out there a flat map
 *  on a wide screen repeats the world, two of every country and of every heat, and a globe
 *  shows each once; from here in, the map is as it always was. */
export const GLOBE_MAX_ZOOM = 3;

/** The web maps' projection (style.ts's `globe`): a globe out to GLOBE_MAX_ZOOM, turning flat
 *  over the next zoom. */
export function globeProjection(): ProjectionSpecification {
  return {
    type: ['interpolate', ['linear'], ['zoom'], GLOBE_MAX_ZOOM, 'vertical-perspective', GLOBE_MAX_ZOOM + 1, 'mercator'],
  };
}
