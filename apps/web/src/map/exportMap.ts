import { Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, type ActivityQuery } from '../api';
import logoUrl from '../assets/logo.png';
import { basemapOrigin } from './config';
import { customOutputSize } from './exportPresets';
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
 * The user frames a region of the live map manually (`ExportFrame.tsx` — a frame anchored to
 * a map position, with its shape picked from its own toolbar) rather than exporting an
 * automatic fit of whatever happens to be on screen — `exportFramedImage` below is the one
 * entry point this module exports.
 */

/** How long to wait for a render to actually settle before giving up — generous, since a
 *  fresh Map instance has to fetch every tile at this new size/position from nothing, unlike
 *  the live map which usually has most of them cached already. */
const EXPORT_IDLE_TIMEOUT_MS = 20_000;

/** Attribution font size, as a fraction of the *final* canvas's shorter side — scales with
 *  output resolution (a 566px-tall Instagram landscape and a 1920px Story shouldn't read
 *  the credit line at the same absolute size) rather than a fixed px value tuned for one
 *  preset and wrong for the rest. Clamped so it never disappears on a small custom export or
 *  overwhelms one on the largest preset. */
const ATTRIBUTION_FONT_FRACTION = 0.016;
const ATTRIBUTION_MIN_FONT_PX = 12;
const ATTRIBUTION_MAX_FONT_PX = 28;
/** Distance from the canvas's bottom-right corner to the credit line's own corner, same
 *  fraction-of-shorter-side scaling as the font size above. */
const ATTRIBUTION_MARGIN_FRACTION = 0.014;

/** The HoldMyTrack watermark's wordmark font size, relative to the attribution's own font
 *  size rather than its own fraction — the two share one lower strip, so they scale together
 *  and the mark stays the same modest step larger than the credit line at every output size. */
const WATERMARK_FONT_SCALE = 1.15;
/** The logo glyph's height, as a multiple of the wordmark's font size. */
const WATERMARK_LOGO_SCALE = 1.7;
/** Drawn at partial opacity with no backing plate — a signature, not a banner competing with
 *  the map for attention. */
const WATERMARK_ALPHA = 0.7;

export interface ExportViewState {
  flavor: Flavor;
  mode: MapMode;
  activityQuery: ActivityQuery;
  hiddenIds: string[];
}

export interface ExportFrameCapture {
  /** The frame's center, as a map position — the frame is anchored to the map, not the
   *  screen, so it may be partly (or wholly) scrolled out of the live viewport by now. */
  center: { lng: number; lat: number };
  /** The frame's on-screen size in CSS pixels, at the live map's current zoom. */
  widthPx: number;
  heightPx: number;
  /** Exact output pixel dimensions for a platform preset. Omitted for Custom, which
   *  exports at the frame's own on-screen pixel size (`customOutputSize`). */
  target?: { widthPx: number; heightPx: number };
}

/**
 * Renders exactly what's inside the frame and resolves to a PNG blob. The offscreen instance
 * *is* the frame: a container of the frame's own CSS size, centred on the frame's own map
 * position, at the live camera's zoom/bearing/pitch — so it shows precisely the framed
 * extent, with labels and line widths at the same proportions as on screen — and rendered
 * at a `pixelRatio` that makes its backing canvas the output size. "What's in the frame, only
 * sharper", with no crop step, and independent of whether the frame is still inside the live
 * viewport. `liveMap` is read from directly for the camera rather than requiring the caller
 * to track it — MapView.tsx doesn't keep camera position in React state at all (FR-4.5: the
 * URL hash is the only place it's persisted).
 */
export async function exportFramedImage(
  liveMap: MapLibreMap,
  state: ExportViewState,
  capture: ExportFrameCapture,
): Promise<Blob> {
  const { center, widthPx, heightPx, target } = capture;
  const { widthPx: outWidth, heightPx: outHeight } = target ?? customOutputSize(widthPx, heightPx);
  const cssWidth = Math.max(1, Math.round(widthPx));
  const cssHeight = Math.max(1, Math.round(heightPx));

  const { canvas, cleanup } = await renderOffscreen(liveMap, state, {
    center,
    width: cssWidth,
    height: cssHeight,
    pixelRatio: outWidth / cssWidth,
  });
  try {
    // Always redrawn onto a canvas of the exact output size: the backing canvas can land a
    // pixel off after rounding (and a preset frame's ratio can differ from its target by a
    // rounding pixel too), and attribution must go onto a 2D canvas regardless.
    const finalCanvas = scaleCanvas(canvas, outWidth, outHeight);
    const stripBottom = drawAttribution(finalCanvas);
    await drawWatermark(finalCanvas, stripBottom, state.flavor);

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
 * `drawWatermark` below shares this same lower strip, mirrored into the bottom-left corner,
 * with the same `stripMetrics` sizing so the two scale together and keep the same corner margin.
 * Returns the plate's bottom edge, so the watermark can end on exactly the same line.
 */
function drawAttribution(canvas: HTMLCanvasElement): number {
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('export: could not create attribution canvas context');

  const { fontPx, margin } = stripMetrics(canvas);
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
  return baselineY + descent + padY;
}

/** The lower strip's shared sizing: the attribution font size and the corner margin, both
 *  scaled off the canvas's shorter side. */
function stripMetrics(canvas: HTMLCanvasElement): { fontPx: number; margin: number } {
  const shortSide = Math.min(canvas.width, canvas.height);
  const fontPx = Math.min(
    ATTRIBUTION_MAX_FONT_PX,
    Math.max(ATTRIBUTION_MIN_FONT_PX, shortSide * ATTRIBUTION_FONT_FRACTION),
  );
  const margin = Math.max(fontPx * 0.5, shortSide * ATTRIBUTION_MARGIN_FRACTION);
  return { fontPx, margin };
}

/**
 * Bakes the HoldMyTrack logo and "HoldMyTrack" wordmark into the bottom-left corner of
 * `canvas` in place — the opposite end of the attribution's lower strip, with the same corner
 * margin and its bottom edge on `bottomY` (the attribution plate's own bottom edge), so the
 * two never overlap and the map's middle stays clear. Always drawn, like attribution — every
 * export carries it.
 * The wordmark copies the page header's brand (services/server/internal/web/templates/
 * header.html): "HoldMy" regular, "Track" bold, in the header's
 * serif font. Unlike attribution it has no backing plate, just `WATERMARK_ALPHA` overall, so
 * its colors follow the basemap flavor instead: the header's own dark ink/amber on light
 * flavors, light counterparts on `dark`/`black`.
 *
 * The logo is the same bundled `logo.png` the header uses, so it's same-origin and drawing it
 * doesn't taint the canvas `toBlob()` reads. The serif font is awaited first because canvas
 * `fillText` doesn't wait for a web font — it just uses the fallback if the font isn't loaded
 * yet. Neither wait can fail the export: a font that never loads falls back to `serif`, and a
 * logo that fails to decode leaves the wordmark on its own.
 */
async function drawWatermark(canvas: HTMLCanvasElement, bottomY: number, flavor: Flavor): Promise<void> {
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('export: could not create watermark canvas context');

  const { fontPx: attributionFontPx, margin } = stripMetrics(canvas);
  const fontPx = attributionFontPx * WATERMARK_FONT_SCALE;
  const logoH = fontPx * WATERMARK_LOGO_SCALE;
  const gap = fontPx * 0.35;
  const lightFont = `400 ${fontPx}px Fraunces, serif`;
  const boldFont = `800 ${fontPx}px Fraunces, serif`;

  const [logo] = await Promise.all([
    loadImage(logoUrl).catch(() => null),
    document.fonts?.load(lightFont).catch(() => undefined),
    document.fonts?.load(boldFont).catch(() => undefined),
  ]);

  ctx.font = lightFont;
  const lightWidth = ctx.measureText('HoldMy').width;

  const logoW = logo ? (logo.naturalWidth / logo.naturalHeight) * logoH : 0;
  const midY = bottomY - Math.max(logoH, fontPx) / 2;
  const dark = flavor === 'dark' || flavor === 'black';

  ctx.save();
  ctx.globalAlpha = WATERMARK_ALPHA;

  let x = margin;
  if (logo) {
    ctx.drawImage(logo, x, midY - logoH / 2, logoW, logoH);
    x += logoW + gap;
  }
  ctx.textAlign = 'left';
  ctx.textBaseline = 'middle';
  ctx.fillStyle = dark ? '#e6ebe8' : '#4a5750';
  ctx.font = lightFont;
  ctx.fillText('HoldMy', x, midY);
  // The header's "Track" amber, lightened on dark flavors so it still reads there.
  ctx.fillStyle = dark ? '#e0a84a' : '#9a6b1e';
  ctx.font = boldFont;
  ctx.fillText('Track', x + lightWidth, midY);
  ctx.restore();
}

function loadImage(src: string): Promise<HTMLImageElement> {
  const img = new Image();
  img.src = src;
  return img.decode().then(() => img);
}

interface OffscreenView {
  center: { lng: number; lat: number };
  /** Container size, CSS pixels. */
  width: number;
  height: number;
  /** Backing-canvas pixels per CSS pixel — what sets the output resolution. */
  pixelRatio: number;
}

/**
 * The offscreen-instance/overlay-replay machinery — kept apart from `exportFramedImage` above
 * so that function reads as the geometry alone. Attribution and watermark bake-in
 * (`drawAttribution`/`drawWatermark` above) belongs on the *final* canvas in
 * `exportFramedImage` (post-scale — the one actually encoded to PNG), not on the canvas this
 * returns.
 */
async function renderOffscreen(
  liveMap: MapLibreMap,
  state: ExportViewState,
  view: OffscreenView,
): Promise<{ canvas: HTMLCanvasElement; cleanup: () => void }> {
  const { width, height } = view;
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
    center: [view.center.lng, view.center.lat],
    zoom: liveMap.getZoom(),
    pixelRatio: view.pixelRatio,
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
