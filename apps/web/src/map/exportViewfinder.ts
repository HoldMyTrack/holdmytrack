/**
 * The export viewfinder's geometry and output sizes (FR-4.10), as plain values so they're
 * unit-tested apart from the component that draws them (`ExportViewfinder.tsx`). Every rect is
 * in the map container's CSS pixels.
 *
 * The window sits fixed in the middle of the map and the map moves under it. Dragging a corner
 * crops; on release the window settles back to the largest size the area holds and the map
 * zooms by the same factor, so the cropped area fills it again (`settleWindow`). The window
 * only sets what's in the picture and its shape; how many pixels the picture has is chosen
 * apart from it, so a phone's small screen makes as big an image as a desktop's.
 */

import type { ExportPreset } from './exportPresets';

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export type Corner = 'nw' | 'ne' | 'sw' | 'se';

/** Room kept clear around the window: the toolbar's row above it, the output size's label
 *  below it, and a margin at the sides. */
export const VIEWFINDER_TOP_PX = 64;
export const VIEWFINDER_BOTTOM_PX = 44;
export const VIEWFINDER_SIDE_PX = 16;
/** The smallest a corner drag can crop the window to. */
export const MIN_WINDOW_PX = 64;

/** The part of a `width` × `height` map the window may take. */
export function viewfinderArea(width: number, height: number): Rect {
  return {
    x: VIEWFINDER_SIDE_PX,
    y: VIEWFINDER_TOP_PX,
    w: Math.max(MIN_WINDOW_PX, width - 2 * VIEWFINDER_SIDE_PX),
    h: Math.max(MIN_WINDOW_PX, height - VIEWFINDER_TOP_PX - VIEWFINDER_BOTTOM_PX),
  };
}

/** The largest window of `aspect` (width ÷ height) that fits `area`, centred in it; with no
 *  aspect, the whole area. */
export function fitWindow(aspect: number | null, area: Rect): Rect {
  if (aspect === null) return { ...area };
  const w = area.w / area.h >= aspect ? area.h * aspect : area.w;
  const h = w / aspect;
  return { x: area.x + (area.w - w) / 2, y: area.y + (area.h - h) / 2, w, h };
}

/**
 * The window while `corner` is dragged by (`dx`, `dy`) from `start`: the opposite corner stays
 * put, the size stays within `area` and at least MIN_WINDOW_PX, and with an `aspect` the shape
 * holds, scaled by whichever axis the pointer has moved further.
 */
export function resizeWindow(start: Rect, corner: Corner, dx: number, dy: number, aspect: number | null, area: Rect): Rect {
  const sx = corner.endsWith('e') ? 1 : -1;
  const sy = corner.startsWith('s') ? 1 : -1;
  const ax = sx > 0 ? start.x : start.x + start.w;
  const ay = sy > 0 ? start.y : start.y + start.h;
  const maxW = sx > 0 ? area.x + area.w - ax : ax - area.x;
  const maxH = sy > 0 ? area.y + area.h - ay : ay - area.y;
  let w = start.w + sx * dx;
  let h = start.h + sy * dy;
  if (aspect === null) {
    w = clamp(w, MIN_WINDOW_PX, maxW);
    h = clamp(h, MIN_WINDOW_PX, maxH);
  } else {
    const kx = w / start.w;
    const ky = h / start.h;
    const scale = Math.abs(kx - 1) >= Math.abs(ky - 1) ? kx : ky;
    h = (start.w * scale) / aspect;
    w = start.w * scale;
    const k = Math.min(maxW / w, maxH / h, 1);
    w *= k;
    h *= k;
    const grow = Math.max(MIN_WINDOW_PX / w, MIN_WINDOW_PX / h, 1);
    w *= grow;
    h *= grow;
  }
  return { x: sx > 0 ? ax : ax - w, y: sy > 0 ? ay : ay - h, w, h };
}

/**
 * Where a cropped window goes on release: back to the largest window of its new shape, with
 * the zoom change that makes the cropped part of the map fill it, and the point of the map
 * that has to end up in its middle (the crop's own middle, in today's screen pixels).
 */
export function settleWindow(crop: Rect, area: Rect): { window: Rect; zoomDelta: number; focus: { x: number; y: number } } {
  const window = fitWindow(crop.w / crop.h, area);
  return {
    window,
    zoomDelta: Math.log2(window.w / crop.w),
    focus: { x: crop.x + crop.w / 2, y: crop.y + crop.h / 2 },
  };
}

/** Whether a settle would change anything visible: a drag that ended where it began, or
 *  within a pixel of it, leaves the map where it is. */
export function windowMoved(a: Rect, b: Rect): boolean {
  return Math.abs(a.x - b.x) > 0.5 || Math.abs(a.y - b.y) > 0.5 || Math.abs(a.w - b.w) > 0.5 || Math.abs(a.h - b.h) > 0.5;
}

/** A Custom image's sizes, by its long side; the short side follows the window's shape. */
export const CUSTOM_SIZES = [
  { id: 'hd', label: 'HD', longSidePx: 1920 },
  { id: '2k', label: '2K', longSidePx: 2560 },
  { id: '4k', label: '4K', longSidePx: 3840 },
] as const;

export type CustomSizeId = (typeof CUSTOM_SIZES)[number]['id'];

export const DEFAULT_CUSTOM_SIZE: CustomSizeId = 'hd';

/** The pixel size of a Custom image of `aspect` with its long side at `longSidePx`. */
export function customOutputSize(aspect: number, longSidePx: number): { widthPx: number; heightPx: number } {
  return aspect >= 1
    ? { widthPx: longSidePx, heightPx: Math.max(1, Math.round(longSidePx / aspect)) }
    : { widthPx: Math.max(1, Math.round(longSidePx * aspect)), heightPx: longSidePx };
}

/** The image's pixel size: a preset's own, or a Custom size in the window's shape. */
export function outputSize(preset: ExportPreset | 'custom', customSize: CustomSizeId, window: Rect): { widthPx: number; heightPx: number } {
  if (preset !== 'custom') return { widthPx: preset.widthPx, heightPx: preset.heightPx };
  const longSide = CUSTOM_SIZES.find((size) => size.id === customSize)?.longSidePx ?? CUSTOM_SIZES[0].longSidePx;
  return customOutputSize(window.w / window.h, longSide);
}

/** Whether this device can draw a Custom size: its long side within `maxSidePx`, the most
 *  the browser's WebGL can draw in one image. */
export function customSizeAvailable(id: CustomSizeId, maxSidePx: number): boolean {
  return (CUSTOM_SIZES.find((size) => size.id === id)?.longSidePx ?? Infinity) <= maxSidePx;
}

/** The size picked, or the largest the device can draw when it can't draw that one. */
export function usableCustomSize(id: CustomSizeId, maxSidePx: number): CustomSizeId {
  if (customSizeAvailable(id, maxSidePx)) return id;
  const fits = CUSTOM_SIZES.filter((size) => size.longSidePx <= maxSidePx);
  return fits.at(-1)?.id ?? CUSTOM_SIZES[0].id;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), Math.max(min, max));
}
