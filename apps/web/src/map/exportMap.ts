import { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, type ActivityQuery } from '../api';
import { basemapOrigin } from './config';
import { ensureFogLayer, type MaskQuery } from './fog';
import { ensureHeatmapLayer } from './heatmap';
import { labelInsertionPoint } from './layers';
import { setMapMode, type MapMode } from './mapMode';
import { buildStyle, type Flavor } from './style';
import { ensureTrackLayer, setHiddenTracks } from './tracks';

/**
 * `VISION.md` §4.2's "print-grade... export" — the client-side slice
 * (`IMPLEMENTATION.md` §5.5's "Revised" note explains why this isn't the
 * headless server-side pipeline that section originally specified). The map is already fully
 * client-rendered, so this reuses every overlay module exactly as `MapView.tsx`'s own
 * `reattachOverlays` does, just against a second, temporary, hidden `Map` instead of the live
 * one — the live map is never resized, never flickers, and never pays `preserveDrawingBuffer`'s
 * cost for a feature used rarely.
 */

/** Wider than the on-screen canvas on purpose — "print-grade" is scoped here as "generously
 *  larger than a screenshot," not a calibrated DPI against an unspecified physical size (see
 *  the plan this shipped from). Height is derived from the live map's own aspect ratio, so
 *  the export frames the same view, just bigger. */
const EXPORT_WIDTH_PX = 2400;

/** How long to wait for a render to actually settle before giving up — generous, since a
 *  fresh Map instance has to fetch every tile at this new size/position from nothing, unlike
 *  the live map which usually has most of them cached already. */
const EXPORT_IDLE_TIMEOUT_MS = 20_000;

export interface ExportViewState {
  flavor: Flavor;
  mode: MapMode;
  activityQuery: ActivityQuery;
  maskQuery: MaskQuery;
  hiddenIds: string[];
}

/**
 * Renders the current view at export resolution and resolves to a PNG blob. `liveMap` is
 * read from directly (center/zoom/container aspect ratio) rather than requiring the caller to
 * track those separately — MapView.tsx doesn't keep camera position in React state at all
 * (FR-4.5: the URL hash is the only place it's persisted), so "the current view" only really
 * exists on the live instance itself.
 */
export async function exportMapImage(liveMap: MapLibreMap, state: ExportViewState): Promise<Blob> {
  const canvas = liveMap.getCanvas();
  const aspect = canvas.clientHeight / canvas.clientWidth || 0.75;
  const width = EXPORT_WIDTH_PX;
  const height = Math.round(width * aspect);

  const container = document.createElement('div');
  // Off-screen via position, not display:none — a display:none element has no real layout
  // box, and MapLibre needs one to size its canvas and WebGL context correctly.
  container.style.position = 'fixed';
  container.style.top = '0';
  container.style.left = '-99999px';
  container.style.width = `${width}px`;
  container.style.height = `${height}px`;
  document.body.appendChild(container);

  const instance = new MapLibreMap({
    container,
    style: buildStyle({ flavor: state.flavor, origin: basemapOrigin() }),
    center: liveMap.getCenter(),
    zoom: liveMap.getZoom(),
    attributionControl: false,
    maxPitch: 0,
    // Same credentialed-tile wiring useMapInstance.ts's live map already has — tracks/fog/
    // heatmap tiles need the session cookie, which MapLibre's own fetches don't carry by
    // default (they bypass api.ts entirely).
    transformRequest: (url) => (url.startsWith(API_BASE_URL) ? { url, credentials: 'include' } : { url }),
    // The one thing the live map deliberately doesn't set — see this module's own doc
    // comment for why it's confined to this temporary instance instead. Nested under
    // canvasContextAttributes, not a top-level MapOptions field, in this maplibre-gl version
    // (confirmed against the installed 6.9.0 type declarations, not assumed).
    canvasContextAttributes: { preserveDrawingBuffer: true },
  });

  try {
    await new Promise<void>((resolve, reject) => {
      instance.once('load', () => resolve());
      instance.once('error', (e) => reject(e.error ?? new Error('export map failed to load')));
    });

    const beforeId = labelInsertionPoint(instance);
    ensureFogLayer(instance, beforeId, state.maskQuery);
    ensureHeatmapLayer(instance, beforeId, state.maskQuery);
    ensureTrackLayer(instance, beforeId, state.activityQuery);
    setMapMode(instance, state.mode);
    setHiddenTracks(instance, state.hiddenIds);

    await waitForIdle(instance);

    const blob = await new Promise<Blob | null>((resolve) => {
      instance.getCanvas().toBlob(resolve, 'image/png');
    });
    if (!blob) throw new Error('export: canvas produced no image data');
    return blob;
  } finally {
    instance.remove();
    container.remove();
  }
}

/**
 * Waits for `'idle'`, then a fixed 150 ms settle buffer before trusting it — the same margin
 * docs/DEVELOPMENT.md's own `isStyleLoaded()` gotcha already established for this class of
 * problem (a just-added source/layer can report "done" a tick before it actually is). A plain `'idle'`
 * listener rather than a polled boolean, since that's the API MapLibre actually offers here.
 */
function waitForIdle(map: MapLibreMap): Promise<void> {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(
      () => reject(new Error('export: timed out waiting for the map to finish rendering')),
      EXPORT_IDLE_TIMEOUT_MS,
    );
    map.once('idle', () => {
      setTimeout(() => {
        clearTimeout(timeout);
        resolve();
      }, 150);
    });
  });
}
