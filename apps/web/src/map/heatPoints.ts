/**
 * How the Heatmap's points are drawn (IMPLEMENTATION.md §4.2.2): MapLibre heatmap circles over
 * the server's heat-points tiles, at every zoom. Pure values, no map — heatmap.ts adds the layer.
 * Android's MapOverlays.kt holds the same numbers and must change with these.
 */

/** The last zoom the server makes heat-points tiles for (internal/fog's HeatPointsMaxZoom):
 *  its cells are the stored count tiles' own pixels, ~19 m. Past it, MapLibre enlarges it. */
export const HEAT_POINTS_MAX_ZOOM = 15;

/** A cell's width in screen pixels at its tile's own zoom: 64 cells to a 512 px tile. */
const CELL_PX = 8;

/** A circle's radius, as a multiple of the cell: wide enough that a route's cells blend into a
 *  line rather than beads, narrow enough that streets a block apart stay apart. The server's
 *  neighbour weighting assumes this radius (internal/fog's heatRadiusCells). */
const RADIUS_PER_CELL = 1.6;

/** MapLibre's circle peaks at about 0.4 of its weight; this lifts it back, so a point's circle
 *  peaks at its own weight. The server has already divided each weight by how much its
 *  neighbours' circles add (internal/fog's weighNeighbours), so a lone place, a route and a busy
 *  area all read their own counts at this one intensity. */
export const HEAT_POINTS_INTENSITY = 2.5;

/** The circles' colour by density: the ramp drawn from green, where an account went once, through
 *  yellow and orange to red at its busiest; nothing below a twentieth, so a soft edge ends. */
export const HEAT_RAMP: [number, string][] = [
  [0, 'rgba(60,200,40,0)'],
  [0.05, 'rgba(60,200,40,0)'],
  [0.1, 'rgb(90,210,40)'],
  [0.3, 'rgb(170,225,30)'],
  [0.5, 'rgb(250,220,0)'],
  [0.68, 'rgb(250,120,0)'],
  [0.8, 'rgb(230,30,30)'],
  [1, 'rgb(230,30,30)'],
];

/**
 * The circles' radius by zoom: each tile's cells are CELL_PX across at its own zoom and double
 * that one zoom in, until the next zoom's tiles, with cells half the size, take over — so the
 * radius grows with the cells across each zoom and drops back at the next. Past the last zoom
 * the cells stop shrinking, and the radius just keeps growing with them.
 */
export function heatPointsRadius(): unknown[] {
  const r = CELL_PX * RADIUS_PER_CELL;
  const stops: number[] = [];
  for (let z = 0; z < HEAT_POINTS_MAX_ZOOM; z++) stops.push(z, r, z + 0.999, 2 * r);
  stops.push(HEAT_POINTS_MAX_ZOOM, r, HEAT_POINTS_MAX_ZOOM + 9, r * 2 ** 9);
  return ['interpolate', ['exponential', 2], ['zoom'], ...stops];
}
