/**
 * The photo viewer's zoom and pan (PhotoViewer.tsx, FR-16.8), as plain math over a view: the
 * picture fitted to the stage and centered is scale 1 at (0, 0); `scale` multiplies that and
 * `x`/`y` move its center, in CSS pixels from the stage's center. Kept apart from the component
 * so it can be tested without a browser.
 */
export interface ZoomView {
  scale: number;
  x: number;
  y: number;
}

export interface Size {
  width: number;
  height: number;
}

export const FIT_VIEW: ZoomView = { scale: 1, x: 0, y: 0 };
/** Past this the 2048 px stored copy is only blur on a typical screen. */
export const MAX_SCALE = 8;
/** One press of + or −. */
export const ZOOM_STEP = 1.5;
/** Where a double-click on the fitted picture zooms to. */
export const DOUBLE_CLICK_SCALE = 3;

/** The picture's size at scale 1: as large as fits the stage, never larger than it is. */
export function fitSize(image: Size, stage: Size): Size {
  if (image.width <= 0 || image.height <= 0) return { width: 0, height: 0 };
  const ratio = Math.min(stage.width / image.width, stage.height / image.height, 1);
  return { width: image.width * ratio, height: image.height * ratio };
}

/**
 * Holds the picture where it can be: no smaller than fitted, no larger than MAX_SCALE, and an
 * edge never pulled in past the stage's edge — a picture narrower than the stage stays centered.
 */
export function clampView(view: ZoomView, fitted: Size, stage: Size): ZoomView {
  const scale = Math.min(Math.max(view.scale, 1), MAX_SCALE);
  const maxX = Math.max(0, (fitted.width * scale - stage.width) / 2);
  const maxY = Math.max(0, (fitted.height * scale - stage.height) / 2);
  return { scale, x: clamp(view.x, maxX), y: clamp(view.y, maxY) };
}

/**
 * Scales by `factor` about `point` (from the stage's center), so what's under the pointer — or
 * between the fingers — stays there.
 */
export function zoomAt(view: ZoomView, factor: number, point: { x: number; y: number }, fitted: Size, stage: Size): ZoomView {
  const scale = Math.min(Math.max(view.scale * factor, 1), MAX_SCALE);
  const k = scale / view.scale;
  return clampView({ scale, x: point.x - (point.x - view.x) * k, y: point.y - (point.y - view.y) * k }, fitted, stage);
}

export function panBy(view: ZoomView, dx: number, dy: number, fitted: Size, stage: Size): ZoomView {
  return clampView({ ...view, x: view.x + dx, y: view.y + dy }, fitted, stage);
}

function clamp(value: number, limit: number): number {
  // + 0 turns a -0 into 0, so a view that hasn't moved compares equal to FIT_VIEW.
  return Math.min(Math.max(value, -limit), limit) + 0;
}
