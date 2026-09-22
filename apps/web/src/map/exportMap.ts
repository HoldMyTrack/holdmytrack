import { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, type ActivityQuery } from '../api';
import { basemapOrigin } from './config';
import { ensureFogLayer } from './fog';
import { ensureHeatmapLayer } from './heatmap';
import { labelInsertionPoint } from './layers';
import { setMapMode, type MapMode } from './mapMode';
import { ATTRIBUTION_TEXT, buildStyle, type Flavor } from './style';
import { ensureTrackLayer, setHiddenTracks } from './tracks';

/**
 * `VISION.md` §4.2's "print-grade... export" — the client-side slice
 * (`IMPLEMENTATION.md` §5.5's "Revised" note explains why this isn't the
 * headless server-side pipeline that section originally specified). The map is already fully
 * client-rendered, so this reuses every overlay module exactly as `MapView.tsx`'s own
 * `reattachOverlays` does, just against a second, temporary, hidden `Map` instead of the live
 * one — the live map is never resized, never flickers, and never pays `preserveDrawingBuffer`'s
 * cost for a feature used rarely.
 *
 * The user frames a region of the live map manually (`ExportPresetDialog.tsx` picks a target
 * shape, `ExportFrame.tsx` positions/sizes it) rather than exporting an automatic fit of
 * whatever happens to be on screen — `exportFramedImage` below is the one entry point this
 * module exports.
 */

/** The output's longer side, for a Custom frame (no declared target dimensions to aim for
 *  instead) — "print-grade" is scoped here as "generously larger than a screenshot," not a
 *  calibrated DPI against an unspecified physical size. Not tied to "width" specifically:
 *  a portrait Custom frame's longer side is its height, just as validly. */
const EXPORT_LONG_SIDE_PX = 2400;

/** How much bigger than the live container's own on-screen size to render the offscreen
 *  instance — the crop in `exportFramedImage` below then pulls just the framed region out of
 *  that, so this needs to comfortably cover the largest preset (1920px) even when the frame
 *  covers only a modest fraction of the container. */
const EXPORT_SUPERSAMPLE = 3;

/** How long to wait for a render to actually settle before giving up — generous, since a
 *  fresh Map instance has to fetch every tile at this new size/position from nothing, unlike
 *  the live map which usually has most of them cached already. */
const EXPORT_IDLE_TIMEOUT_MS = 20_000;

/** Attribution font size, as a fraction of the *final* canvas's shorter side — scales with
 *  output resolution (a 566px-tall Instagram landscape crop and a 1920px Story shouldn't read
 *  the credit line at the same absolute size) rather than a fixed px value tuned for one
 *  preset and wrong for the rest. Clamped so it never disappears on a tiny custom crop or
 *  overwhelms one on the largest preset. */
const ATTRIBUTION_FONT_FRACTION = 0.016;
const ATTRIBUTION_MIN_FONT_PX = 12;
const ATTRIBUTION_MAX_FONT_PX = 28;
/** Distance from the canvas's bottom-right corner to the credit line's own corner, same
 *  fraction-of-shorter-side scaling as the font size above. */
const ATTRIBUTION_MARGIN_FRACTION = 0.014;

export interface ExportViewState {
  flavor: Flavor;
  mode: MapMode;
  activityQuery: ActivityQuery;
  hiddenIds: string[];
}

export interface ExportFrameCapture {
  /** The live map container's own on-screen CSS-pixel rect — `getBoundingClientRect()`, read
   *  once by the caller right before calling this. The live map must not move or resize while
   *  a capture is in flight, the same invariant this module already relied on before framing
   *  existed. */
  containerRect: DOMRect;
  /** The frame's own on-screen rect, in the same coordinate space as `containerRect`. */
  frameRect: DOMRect;
  /** Exact output pixel dimensions for a platform preset. Omitted for Custom, where the
   *  crop's own aspect ratio is kept and only scaled so its longer side hits
   *  `EXPORT_LONG_SIDE_PX`. */
  target?: { widthPx: number; heightPx: number };
}

/**
 * Renders the region inside `capture.frameRect`, at `capture.target`'s exact pixel dimensions
 * (or, for Custom, proportionally scaled the same way this module always has), and resolves to
 * a PNG blob. `liveMap` is read from directly (center/zoom/bearing/pitch) rather than requiring
 * the caller to track those separately — MapView.tsx doesn't keep camera position in React
 * state at all (FR-4.5: the URL hash is the only place it's persisted), so "the current view"
 * only really exists on the live instance itself.
 *
 * True WYSIWYG relative to the frame: the offscreen instance is rendered at
 * `EXPORT_SUPERSAMPLE`× the live container's own size, at the live camera's exact
 * center/zoom/bearing/pitch, so it's pixel-registered to the live view and the frame's rect
 * can be cropped out of it by simple proportional math — no unproject/re-centering needed.
 */
export async function exportFramedImage(
  liveMap: MapLibreMap,
  state: ExportViewState,
  capture: ExportFrameCapture,
): Promise<Blob> {
  const { containerRect, frameRect, target } = capture;
  const tempWidth = Math.round(containerRect.width * EXPORT_SUPERSAMPLE);
  const tempHeight = Math.round(containerRect.height * EXPORT_SUPERSAMPLE);

  const { canvas, cleanup } = await renderOffscreen(liveMap, state, tempWidth, tempHeight);
  try {
    // The offscreen container was sized to tempWidth/tempHeight CSS pixels, but MapLibre backs
    // its canvas at devicePixelRatio× that (same as the live map) — read the actual ratio back
    // from the canvas itself rather than trusting `window.devicePixelRatio` at call time, which
    // sidesteps any drift between the two (e.g. browser zoom changing DPR mid-session).
    const backingRatio = canvas.width / tempWidth;
    const pxPerContainerPx = EXPORT_SUPERSAMPLE * backingRatio;
    const cropX = (frameRect.left - containerRect.left) * pxPerContainerPx;
    const cropY = (frameRect.top - containerRect.top) * pxPerContainerPx;
    const cropW = frameRect.width * pxPerContainerPx;
    const cropH = frameRect.height * pxPerContainerPx;

    const cropCanvas = document.createElement('canvas');
    cropCanvas.width = Math.max(1, Math.round(cropW));
    cropCanvas.height = Math.max(1, Math.round(cropH));
    const cropCtx = cropCanvas.getContext('2d');
    if (!cropCtx) throw new Error('export: could not create crop canvas context');
    cropCtx.drawImage(canvas, cropX, cropY, cropW, cropH, 0, 0, cropCanvas.width, cropCanvas.height);

    const finalCanvas = target
      ? scaleCanvas(cropCanvas, target.widthPx, target.heightPx)
      : scaleCanvasToLongSide(cropCanvas, EXPORT_LONG_SIDE_PX);
    // On the final canvas, not cropCanvas or the offscreen instance's own — this module's own
    // doc comment on renderOffscreen explains why: frame position and preset scaling both
    // happen after those, and attribution needs to land correctly positioned/sized in the
    // actual output regardless of either.
    drawAttribution(finalCanvas);

    const blob = await new Promise<Blob | null>((resolve) => {
      finalCanvas.toBlob(resolve, 'image/png');
    });
    if (!blob) throw new Error('export: canvas produced no image data');
    return blob;
  } finally {
    cleanup();
  }
}

function scaleCanvas(source: HTMLCanvasElement, widthPx: number, heightPx: number): HTMLCanvasElement {
  const out = document.createElement('canvas');
  out.width = widthPx;
  out.height = heightPx;
  const ctx = out.getContext('2d');
  if (!ctx) throw new Error('export: could not create output canvas context');
  ctx.drawImage(source, 0, 0, source.width, source.height, 0, 0, widthPx, heightPx);
  return out;
}

function scaleCanvasToLongSide(source: HTMLCanvasElement, longSidePx: number): HTMLCanvasElement {
  const scale = longSidePx / Math.max(source.width, source.height);
  return scaleCanvas(source, Math.round(source.width * scale), Math.round(source.height * scale));
}

/**
 * Bakes `style.ts`'s own `ATTRIBUTION_TEXT` into the bottom-right corner of `canvas` in place
 * — not optional or togglable, since the basemap is an ODbL "Produced Work" and OSM/Protomaps
 * credit is a license requirement, not a preference (`style.ts`'s own comment on
 * `ATTRIBUTION`). The live map shows the same text via MapLibre's DOM-based
 * `AttributionControl`, which an exported PNG's `canvas.toBlob()` never captures — this is
 * what stands in for that control on the one output path that bypasses the DOM entirely.
 *
 * A translucent plate behind the text, not text alone, since the basemap flavor and whatever
 * fog/heatmap/track content sits beneath this corner varies per export — plain white or black
 * text can disappear against a same-toned background, the same "needs to read regardless of
 * what's underneath" reasoning `IMPLEMENTATION.md` §4.2 already used for fog's own veil.
 *
 * `docs/ROADMAP.md`'s FitMap-logo item shares this same lower-right corner and this same draw
 * call site by design — landing it later just means a second small draw call here, not a new
 * pass over the canvas.
 */
function drawAttribution(canvas: HTMLCanvasElement): void {
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('export: could not create attribution canvas context');

  const shortSide = Math.min(canvas.width, canvas.height);
  const fontPx = Math.min(
    ATTRIBUTION_MAX_FONT_PX,
    Math.max(ATTRIBUTION_MIN_FONT_PX, shortSide * ATTRIBUTION_FONT_FRACTION),
  );
  const margin = Math.max(fontPx * 0.5, shortSide * ATTRIBUTION_MARGIN_FRACTION);
  const padX = fontPx * 0.5;
  const padY = fontPx * 0.3;

  ctx.font = `${fontPx}px sans-serif`;
  ctx.textAlign = 'right';
  ctx.textBaseline = 'alphabetic';

  const metrics = ctx.measureText(ATTRIBUTION_TEXT);
  // Falls back to font-relative approximations of ascent/descent — actualBoundingBox* is
  // widely supported (every browser this app otherwise targets), but degrading gracefully
  // here costs nothing and keeps the plate from vanishing entirely if it's ever missing.
  const ascent = metrics.actualBoundingBoxAscent || fontPx * 0.8;
  const descent = metrics.actualBoundingBoxDescent || fontPx * 0.2;

  const baselineX = canvas.width - margin;
  const baselineY = canvas.height - margin;

  ctx.fillStyle = 'rgba(0, 0, 0, 0.55)';
  ctx.fillRect(
    baselineX - metrics.width - padX * 2,
    baselineY - ascent - padY,
    metrics.width + padX * 2,
    ascent + descent + padY * 2,
  );

  ctx.fillStyle = '#ffffff';
  ctx.fillText(ATTRIBUTION_TEXT, baselineX - padX, baselineY);
}

/**
 * The offscreen-instance/overlay-replay machinery shared by every capture — factored out so
 * `exportFramedImage` above doesn't duplicate it. Attribution bake-in (`drawAttribution`
 * above; the still-unbuilt FitMap-logo item tracked in `docs/ROADMAP.md` will join it) belongs
 * on the *final* canvas in `exportFramedImage` (post-crop, post-scale — the one actually
 * encoded to PNG), not on the canvas this returns, since frame position and preset scaling
 * both happen after this step.
 */
async function renderOffscreen(
  liveMap: MapLibreMap,
  state: ExportViewState,
  width: number,
  height: number,
): Promise<{ canvas: HTMLCanvasElement; cleanup: () => void }> {
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
    bearing: liveMap.getBearing(),
    pitch: liveMap.getPitch(),
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

  const cleanup = () => {
    instance.remove();
    container.remove();
  };

  try {
    await new Promise<void>((resolve, reject) => {
      instance.once('load', () => resolve());
      instance.once('error', (e) => reject(e.error ?? new Error('export map failed to load')));
    });

    const beforeId = labelInsertionPoint(instance);
    ensureFogLayer(instance, beforeId);
    ensureHeatmapLayer(instance, beforeId);
    ensureTrackLayer(instance, beforeId, state.activityQuery);
    setMapMode(instance, state.mode);
    setHiddenTracks(instance, state.hiddenIds);

    await waitForIdle(instance);

    return { canvas: instance.getCanvas(), cleanup };
  } catch (err) {
    cleanup();
    throw err;
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
