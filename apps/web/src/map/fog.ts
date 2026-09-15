import type { Map as MapLibreMap, RasterTileSource } from 'maplibre-gl';
import { API_BASE_URL, TILES_V1, type ActivityQuery } from '../api';

/**
 * The Fog of War raster layer (IMPLEMENTATION.md §4.2 / its Takeout-adjacent
 * per-activity-mask revision). Unlike tracks, the *unfiltered* case here is still
 * precomputed — the source points at the stored-tile endpoint, not a live query — but the
 * endpoint now accepts the same `from`/`to` tracks already sends, plus `exclude` for the
 * eye-icon/TYPE/DISTANCE hidden set, and composites on the fly when either is present. See
 * heatmap.ts (shares `MaskQuery`) and MapView.tsx (`maskQuery`, computed alongside
 * `activityQuery`).
 */
export const FOG_SOURCE_ID = 'fog';
export const FOG_LAYER_ID = 'fog-raster';

/** `from`/`to` mirror tracks.ts's own date-range filter; `exclude` has no tracks.ts
 *  equivalent — tracks hide client-side (setHiddenTracks, a MapLibre layer filter), but a
 *  raster tile has no per-feature identity to hide, so hidden activities have to be excluded
 *  server-side instead. */
export interface MaskQuery extends Pick<ActivityQuery, 'from' | 'to'> {
  exclude?: readonly string[];
}

/** Shared by fog.ts and heatmap.ts's own tile-URL builders — same query shape, different
 *  endpoint path. `base` already ends in a trailing slash (e.g. `.../tiles/v1/fog/`). */
export function maskTileURL(base: string, query: MaskQuery): string {
  const params = new URLSearchParams();
  if (query.from) params.set('from', query.from);
  if (query.to) params.set('to', query.to);
  if (query.exclude && query.exclude.length > 0) params.set('exclude', query.exclude.join(','));
  const qs = params.toString();
  return `${base}{z}/{x}/{y}.png${qs ? `?${qs}` : ''}`;
}

function fogTileURL(query: MaskQuery): string {
  return maskTileURL(`${API_BASE_URL}${TILES_V1}/fog/`, query);
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
 * `query` seeds the *initial* tiles URL, same convention as ensureTrackLayer's `range` —
 * changing it on an already-existing source is refreshFogLayer's job instead.
 */
export function ensureFogLayer(map: MapLibreMap, beforeId: string | undefined, query: MaskQuery): void {
  if (!map.getSource(FOG_SOURCE_ID)) {
    map.addSource(FOG_SOURCE_ID, {
      type: 'raster',
      tiles: [fogTileURL(query)],
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
        layout: { visibility: 'none' }, // starts hidden — Normal is the default mode
      },
      beforeId,
    );
  }
}

/** Forces MapLibre to refetch every currently-loaded fog tile against the current filter —
 *  the same `setTiles` refresh primitive refreshTrackLayer uses (tracks.ts), for the same
 *  reason: a changed `query` means previously-loaded tiles were composited against a stale
 *  filter and would otherwise keep showing coverage that should now be excluded (or vice
 *  versa). */
export function refreshFogLayer(map: MapLibreMap, query: MaskQuery): void {
  const source = map.getSource(FOG_SOURCE_ID) as RasterTileSource | undefined;
  source?.setTiles([fogTileURL(query)]);
}
