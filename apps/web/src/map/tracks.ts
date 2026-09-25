import type { FilterSpecification, Map as MapLibreMap, VectorTileSource } from 'maplibre-gl';
import { API_BASE_URL, TILES_V1, type ActivityQuery } from '../api';

/**
 * The live tracks MVT layer (IMPLEMENTATION.md §4.3). Unlike the basemap —
 * and unlike fog, once that exists — this is never precomputed: the backend runs a live
 * PostGIS query per tile, so the source points straight at the API rather than a pmtiles
 * archive.
 */
export const TRACKS_SOURCE_ID = 'tracks';
export const TRACKS_LAYER_ID = 'tracks-line';
// The halo drawn under a selected track (checked or focused) — see ensureTrackLayer.
export const TRACKS_CASING_LAYER_ID = 'tracks-casing';
const TRACKS_SOURCE_LAYER = 'tracks'; // must match ST_AsMVT(t, 'tracks', ...) in the backend query

// Tracks draw from z4 — a few states on screen — down to street level. Below that a track is a
// few pixels long; above it the only cost is tile payload, and ST_AsMVTGeom's 4096-unit grid
// already bounds that: measured with the demo's 611 activities and no date filter, the tile
// over Cleveland is 40KB at z4 against 44KB at z8 (IMPLEMENTATION.md §5.3). Not Fog/Heatmap's
// CITY_MIN_ZOOM (z8): those switch to Country/Region fills below it, but Normal mode has no
// such fallback, so tracks hidden there left an empty map — and a focused road trip too long
// to fit at z8 flew to a view with nothing drawn on it.
const TRACKS_MIN_ZOOM = 4;

const NORMAL_WIDTH = 2.5;
const EMPHASIS_WIDTH = 4.5; // selected: checked or focused, plus the halo below
const HOVER_WIDTH = 5; // hovered: the widest, in HOVER_COLOR, with no halo of its own
// 1.5px of halo showing on each side of an EMPHASIS_WIDTH line.
const CASING_WIDTH = EMPHASIS_WIDTH + 3;
const TRACK_COLOR = '#b07e2e'; // --fm-accent
const HOVER_COLOR = '#93691f'; // --fm-accent-strong

// Half-width, in screen pixels, of the box a click's hit-test is queried against — see the
// click handler below for why this can't just be the bare click pixel.
const CLICK_TOLERANCE_PX = 4;

/** Only `from`/`to` — `types` stays a client-side-only filter (activityFacets.ts,
 *  setHiddenTracks below), matching DISTANCE, which has no server-side equivalent at all;
 *  keeping both narrowed the same way is simpler than half server-side, half client-side. */
type TrackDateRange = Pick<ActivityQuery, 'from' | 'to'>;

/**
 * `GET /tiles/v1/tracks/{z}/{x}/{y}.mvt?from=&to=` — the backend already applies this
 * filter (§4.3 shares `parseActivityFilter` with §4.7's list endpoints), but until this
 * function actually sent the params, the map drew every track ever uploaded regardless of
 * the selected date range: an omitted filter means "no restriction," so a bare tile request
 * silently asked for everything. Confirmed live — the Activities list and the map disagreed
 * about what was in view until this was wired up.
 */
function trackTileURL(range: TrackDateRange): string {
  const params = new URLSearchParams();
  if (range.from) params.set('from', range.from);
  if (range.to) params.set('to', range.to);
  const qs = params.toString();
  return `${API_BASE_URL}${TILES_V1}/tracks/{z}/{x}/{y}.mvt${qs ? `?${qs}` : ''}`;
}

// Map instances that already have hover/click handlers attached. A plain module-level
// WeakSet, not a per-call guard inside ensureTrackLayer: setStyle discards and re-adds the
// layer on every theme swap (styledata fires, ensureTrackLayer re-runs), but the *map
// instance* survives across that, and map.on('click', LAYER_ID, ...) is a layer-id-scoped
// delegated listener that keeps matching a layer re-added under the same id — re-attaching
// here on every styledata would silently stack duplicate handlers, firing a click N times
// after N theme toggles.
const interactiveMaps = new WeakSet<MapLibreMap>();

/**
 * Adds the tracks source and layer if they aren't already on the map. Idempotent on
 * purpose: `styledata` fires on every `setStyle` (theme swap), which discards custom
 * layers, so this runs every time and has to be a safe no-op when they're already there.
 *
 * `beforeId` keeps the line layer beneath basemap labels — see layers.ts for why painting
 * over labels reads as a rendering bug rather than a design choice.
 *
 * `range` seeds the *initial* tiles URL for a source created here — the common case is a
 * `styledata`-triggered re-creation after a theme swap discards the old one, so this has to
 * reflect whatever range is current at that moment, not always the unfiltered default.
 * Changing the range on an already-existing source is `refreshTrackLayer`'s job instead.
 */
export function ensureTrackLayer(map: MapLibreMap, beforeId: string | undefined, range: TrackDateRange): void {
  if (!map.getSource(TRACKS_SOURCE_ID)) {
    map.addSource(TRACKS_SOURCE_ID, {
      type: 'vector',
      tiles: [trackTileURL(range)],
      // Matches the basemap archive's own max zoom (IMPLEMENTATION.md
      // §5.4); MapLibre overzooms a vector source past its declared maxzoom the same way
      // it already does for the basemap.
      minzoom: 0,
      maxzoom: 14,
      // Feature-state (hover/selected below) is keyed by whatever id setFeatureState is
      // called with; promoteId is what tells MapLibre to use the tile's own `id` property
      // (the real activity UUID — see the tracks SQL's `id` column) as that key, instead
      // of the tile-internal numeric feature id that resets per tile and means nothing
      // across tiles the same activity spans.
      promoteId: 'id',
    });
  }
  // Hovered and selected (checked or focused, MapView's boldedActivityIds) tracks are both
  // widened, but differently: a hovered one is the widest and a darker gold, a selected one
  // keeps the track color and gets this dark halo underneath, so a checked group still reads
  // as checked while the pointer moves over other tracks (SPEC.md FR-4.1's state table).
  // Hovering a selected track shows both: the hover line over its halo. Feature-state can't drive line-sort-key (a
  // layout property), so a selected track isn't raised above its neighbours; the halo is what
  // separates it where tracks overlap. Added before the line layer so it always sits beneath
  // it, including when only the line layer already exists.
  if (!map.getLayer(TRACKS_CASING_LAYER_ID)) {
    map.addLayer(
      {
        id: TRACKS_CASING_LAYER_ID,
        type: 'line',
        source: TRACKS_SOURCE_ID,
        'source-layer': TRACKS_SOURCE_LAYER,
        minzoom: TRACKS_MIN_ZOOM,
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: {
          // Ink, not white: tracks mostly run along the basemap's white streets, where a white
          // halo disappears into the road it sits on.
          'line-color': '#202b25',
          'line-width': CASING_WIDTH,
          'line-opacity': ['case', ['boolean', ['feature-state', 'selected'], false], 0.95, 0],
        },
      },
      map.getLayer(TRACKS_LAYER_ID) ? TRACKS_LAYER_ID : beforeId,
    );
  }
  if (!map.getLayer(TRACKS_LAYER_ID)) {
    map.addLayer(
      {
        id: TRACKS_LAYER_ID,
        type: 'line',
        source: TRACKS_SOURCE_ID,
        'source-layer': TRACKS_SOURCE_LAYER,
        minzoom: TRACKS_MIN_ZOOM,
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: {
          'line-color': ['case', ['boolean', ['feature-state', 'hover'], false], HOVER_COLOR, TRACK_COLOR],
          'line-width': [
            'case',
            ['boolean', ['feature-state', 'hover'], false],
            HOVER_WIDTH,
            ['boolean', ['feature-state', 'selected'], false],
            EMPHASIS_WIDTH,
            NORMAL_WIDTH,
          ],
          'line-opacity': ['case', ['boolean', ['feature-state', 'selected'], false], 1, 0.9],
        },
      },
      beforeId,
    );
  }
  attachTrackInteractivity(map);
}

export interface TrackInteractivityHandlers {
  /** A track was clicked — the caller decides what "selected" means (see MapView). */
  onSelect: (activityId: string) => void;
  /** The pointer entered or left a track — `null` on leave. Reported up rather than applied
   *  directly (as this used to) so ActivitiesPanel's own row hover can drive the exact same
   *  feature-state through one place (setHoveredTrack) instead of the two fighting over
   *  which one a mouseleave should clear. */
  onHover: (activityId: string | null) => void;
  /** A click landed on the map somewhere with no track underneath it — reported live as the
   *  missing counterpart to onSelect: a focused activity had no way to lose that focus by
   *  clicking away from it on the map itself (only by focusing a different one, or via the
   *  Activities panel). MapView clears its own row-click focus on this; it does not touch
   *  the checkbox group, matching that the two are independent everywhere else. */
  onClickAway: () => void;
}

let handlers: TrackInteractivityHandlers | null = null;

/** Set once from MapView; attachTrackInteractivity reads it indirectly so it isn't a
 *  parameter that would need re-passing (and re-checking against the WeakSet) on every
 *  ensureTrackLayer call. */
export function setTrackInteractivityHandlers(next: TrackInteractivityHandlers): void {
  handlers = next;
}

/**
 * Wires hover (reported up via onHover — see setHoveredTrack for where the feature-state
 * actually gets applied) and click (calls back into MapView, which owns what "selected"
 * persists as) on the tracks layer. Attached once per map instance — see the interactiveMaps
 * comment above for why not once per ensureTrackLayer call.
 */
function attachTrackInteractivity(map: MapLibreMap): void {
  if (interactiveMaps.has(map)) return;
  interactiveMaps.add(map);

  let hoveredId: string | number | null = null;

  map.on('mousemove', TRACKS_LAYER_ID, (e) => {
    map.getCanvas().style.cursor = 'pointer';
    const id = e.features?.[0]?.id;
    if (id === undefined || id === hoveredId) return;
    hoveredId = id;
    handlers?.onHover(String(id));
  });

  map.on('mouseleave', TRACKS_LAYER_ID, () => {
    map.getCanvas().style.cursor = '';
    if (hoveredId !== null) {
      hoveredId = null;
      handlers?.onHover(null);
    }
  });

  // One plain, map-wide click handler rather than a layer-scoped one — onSelect and
  // onClickAway are two outcomes of the exact same hit-test ("did this click land on a
  // track, or not"), so they share one query instead of running it twice and risking the
  // two disagreeing. Queries TRACKS_LAYER_ID specifically rather than the band overlay:
  // every visible band already sits directly on top of that same activity's own
  // tracks-layer feature, so checking the tracks layer alone is sufficient to tell "on a
  // track" from "not."
  //
  // Queries a small box around the click, not the bare pixel: NORMAL_WIDTH is 2.5px, which
  // leaves almost no room for error — a click landing a couple of pixels off the rendered
  // line (easy to do aiming at something this thin) used to just do nothing (harmless), but
  // once a miss started clearing the current focus (onClickAway), the same near-miss became
  // destructive instead. Found live: clicking within 3px of a second activity's track, while
  // a first one was already focused, cleared the first activity's focus and never applied a
  // new one — the exact-pixel query saw neither track under the cursor. CLICK_TOLERANCE_PX
  // is deliberately larger than half of HOVER_WIDTH so a hover-widened line is still
  // comfortably inside its own tolerance box, not just barely.
  map.on('click', (e) => {
    // Same "layer might not exist yet" guard setSelectedTracks/setHiddenTracks already have —
    // queryRenderedFeatures throws for a layer id the current style doesn't have, which is
    // real during the brief window between a styledata-triggered setStyle and reattachOverlays
    // re-adding this layer.
    if (!map.getLayer(TRACKS_LAYER_ID)) return;
    const { x, y } = e.point;
    const box: [[number, number], [number, number]] = [
      [x - CLICK_TOLERANCE_PX, y - CLICK_TOLERANCE_PX],
      [x + CLICK_TOLERANCE_PX, y + CLICK_TOLERANCE_PX],
    ];
    const hits = map.queryRenderedFeatures(box, { layers: [TRACKS_LAYER_ID] });
    const id = hits[0]?.properties?.id as string | undefined;
    if (id) handlers?.onSelect(id);
    else handlers?.onClickAway();
  });
}

// The previously-hovered id, so setHoveredTrack can clear exactly that feature's state —
// same reasoning and the same MapLibre constraint (no id-less removeFeatureState) as
// selectedIds/setSelectedTracks below, just for a single id instead of a set.
let hoveredTrackId: string | null = null;

/**
 * Applies the transient "hover" feature-state to at most one track — the single source of
 * truth for it, fed by both directions: the map's own mousemove/mouseleave above (via
 * onHover) and ActivitiesPanel's row onMouseEnter/onMouseLeave (via MapView's
 * hoveredActivityId state). Centralizing it here is what lets a row hover and a map hover
 * share one piece of state without either clobbering state the other one owns.
 */
export function setHoveredTrack(map: MapLibreMap, activityId: string | null): void {
  if (!map.getSource(TRACKS_SOURCE_ID)) return; // see setSelectedTracks' identical guard
  if (activityId === hoveredTrackId) return;
  if (hoveredTrackId !== null) {
    map.setFeatureState(
      { source: TRACKS_SOURCE_ID, sourceLayer: TRACKS_SOURCE_LAYER, id: hoveredTrackId },
      { hover: false },
    );
  }
  if (activityId !== null) {
    map.setFeatureState({ source: TRACKS_SOURCE_ID, sourceLayer: TRACKS_SOURCE_LAYER, id: activityId }, { hover: true });
  }
  hoveredTrackId = activityId;
}

// The previously-selected ids, so setSelectedTracks can clear exactly those features' state
// without touching ones that are still selected. Tried removeFeatureState({source,
// sourceLayer}, 'selected') — a key with no id — first, on the assumption it clears that key
// across every feature in one call; actually tested against the installed maplibre-gl 6.9.0,
// it throws "A feature id is required to remove its specific state property" instead.
// Tracking ids explicitly, the same way the hover handler above already has to, is the API
// MapLibre actually has.
let selectedIds = new Set<string>();

/**
 * Applies the persistent "selected" feature-state to every id in `activityIds`, clearing it
 * from whatever was selected before but no longer is. Plural because the Activities panel's
 * row click toggles membership in a multi-select set (footer summary, debounced fly-to-fit),
 * not a single "focused" row — every checked row's track bolds, not just the last one clicked.
 */
export function setSelectedTracks(map: MapLibreMap, activityIds: readonly string[]): void {
  // MapView's effect calling this can fire before ensureTrackLayer has added the source —
  // React commits effects in declaration order, and this one is declared ahead of the
  // effect that calls reattachOverlays on the same `map`-becomes-non-null commit. Confirmed
  // directly: without this guard, mounting throws "source 'tracks' does not exist" every
  // time, not occasionally. A missing source has nothing selected on it by definition, so a
  // no-op is the correct behavior here, not a workaround.
  if (!map.getSource(TRACKS_SOURCE_ID)) return;
  const next = new Set(activityIds);
  for (const id of selectedIds) {
    if (!next.has(id)) {
      map.removeFeatureState({ source: TRACKS_SOURCE_ID, sourceLayer: TRACKS_SOURCE_LAYER, id }, 'selected');
    }
  }
  for (const id of next) {
    if (!selectedIds.has(id)) {
      map.setFeatureState({ source: TRACKS_SOURCE_ID, sourceLayer: TRACKS_SOURCE_LAYER, id }, { selected: true });
    }
  }
  selectedIds = next;
}

/**
 * Forces MapLibre to refetch every currently-loaded tracks tile — either because a track
 * that didn't exist yet when a tile was first fetched should now show up (a finished
 * upload), or because `range` itself changed and the previously-loaded tiles were built
 * from the old `from`/`to` and would otherwise keep showing activities outside it.
 *
 * `setTiles` works as a refresh primitive (same URL or not) because it calls the source's
 * internal `load(true)` unconditionally — verified directly against the installed
 * maplibre-gl 6.9.0 bundle, not assumed from the type declarations alone, since a naive
 * implementation could plausibly skip reloading when the value hasn't changed.
 */
export function refreshTrackLayer(map: MapLibreMap, range: TrackDateRange): void {
  const source = map.getSource(TRACKS_SOURCE_ID) as VectorTileSource | undefined;
  source?.setTiles([trackTileURL(range)]);
}

/**
 * Per-activity show/hide (the list's eye icon), client-side only — no backend filter, no
 * refetch. The tracks endpoint already returns everything in range; hiding a track is a
 * question of which already-fetched features get painted, which is exactly what a layer
 * filter is for. A dozen or so hidden ids inlined into `in`/`literal` is not something worth
 * a round trip over.
 *
 * Same "layer might not exist yet" guard as setSelectedTracks, for the same reason: the
 * effect calling this can commit before ensureTrackLayer has run.
 */
export function setHiddenTracks(map: MapLibreMap, hiddenIds: string[]): void {
  if (!map.getLayer(TRACKS_LAYER_ID)) return;
  const next: FilterSpecification | null = hiddenIds.length
    ? ['!', ['in', ['get', 'id'], ['literal', hiddenIds]]]
    : null;
  // map.setFilter (unlike the style-internal one) calls _update(true) unconditionally —
  // marking sources dirty and forcing the live tracks vector-tile source to be re-checked
  // on the next render frame — even when the filter is unchanged. reattachOverlays calls
  // this on every 'styledata' event, so a bare pass-through self-sustains forever: each
  // re-render re-fires 'styledata', which calls this again, which dirties again, which
  // never lets isStyleLoaded() settle (confirmed: this is what hung verify:map's second
  // isStyleLoaded() check). Comparing against the layer's current filter first breaks it.
  // The halo layer takes the same filter, or a hidden-but-checked track would leave its halo
  // drawn with no line on it.
  for (const layerId of [TRACKS_LAYER_ID, TRACKS_CASING_LAYER_ID]) {
    if (!map.getLayer(layerId)) continue;
    const current = map.getFilter(layerId) ?? null;
    if (JSON.stringify(current) === JSON.stringify(next)) continue;
    map.setFilter(layerId, next);
  }
}
