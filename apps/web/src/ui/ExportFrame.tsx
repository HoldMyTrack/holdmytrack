import { useCallback, useEffect, useRef } from 'react';
import type { CSSProperties, RefObject } from 'react';
import type { ExportPreset } from '../map/exportPresets';

/** Fractions (0-1) of the map container's own client rect, not raw pixels — keeps the frame
 *  correctly positioned/proportioned across a window resize with no recompute wiring, the same
 *  reasoning `ActivitiesPanel.tsx`'s `--panel-width` custom property already applies to a
 *  single dynamic value. Exact pixels are only needed once, at capture time. */
export interface FrameRect {
  xFrac: number;
  yFrac: number;
  wFrac: number;
  hFrac: number;
}

const MIN_FRAME_FRACTION = 0.08;

export interface ExportFrameProps {
  /** The live map's own container element — the same ref `MapView.tsx` uses for `.map-canvas`
   *  — read here only for its `getBoundingClientRect()` during drag math, never written to. */
  containerRef: RefObject<HTMLDivElement | null>;
  preset: ExportPreset | 'custom';
  rect: FrameRect;
  busy: boolean;
  error: string | null;
  onRectChange: (rect: FrameRect) => void;
  onCapture: () => void;
  onCancel: () => void;
}

type DragMode = 'move' | 'resize';
interface DragState {
  pointerId: number;
  mode: DragMode;
  startX: number;
  startY: number;
  startRect: FrameRect;
}

/**
 * The framing overlay shown between picking a shape (`ExportPresetDialog.tsx`) and capturing
 * (`exportFramedImage`, `map/exportMap.ts`). Rendered as a sibling of `.map-canvas` inside
 * `.map-root`, the same absolutely-positioned-overlay pattern as `.map-mode-toggle`/
 * `CoverageNotice`. No darkening: the live map stays fully visible and interactive everywhere
 * outside the frame — the outer `.export-frame` wrapper is `pointer-events: none`, so
 * MapLibre's own drag/scroll/click handling reaches the map everywhere except the frame box
 * itself, its corner handle, and its two buttons, which opt back into `pointer-events: auto`.
 * A dashed border is the only visual marker of the frame's own bounds.
 *
 * The frame box's own interior is the move-drag target (not just its edges) — dragging
 * anywhere inside it repositions the frame; dragging the live map means grabbing anywhere
 * *outside* the box, same as normal map panning.
 *
 * Pointer-capture drag, not a drag library — this app hand-rolls every drag interaction
 * (`RangePicker.tsx`, `ActivitiesPanel.tsx`'s panel-width handle) the same way: `onPointerDown`
 * calls `setPointerCapture` and stashes start state in a ref (not React state, so a drag in
 * progress doesn't re-render on every pixel), `onPointerMove` reads it back and calls
 * `event.preventDefault()` up front per `RangePicker.tsx`'s own documented note on why (Firefox
 * hijacks a later drag into a native ghost-image drag once a pointer path crosses text).
 */
export function ExportFrame({ containerRef, preset, rect, busy, error, onRectChange, onCapture, onCancel }: ExportFrameProps) {
  const dragRef = useRef<DragState | null>(null);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onCancel();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [onCancel]);

  const beginDrag = useCallback((mode: DragMode, event: React.PointerEvent) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = { pointerId: event.pointerId, mode, startX: event.clientX, startY: event.clientY, startRect: rect };
  }, [rect]);

  const onDragMove = useCallback(
    (event: React.PointerEvent) => {
      const drag = dragRef.current;
      const el = containerRef.current;
      if (!drag || !el || event.pointerId !== drag.pointerId) return;
      const box = el.getBoundingClientRect();
      if (box.width <= 0 || box.height <= 0) return;
      const dxFrac = (event.clientX - drag.startX) / box.width;
      const dyFrac = (event.clientY - drag.startY) / box.height;
      if (drag.mode === 'move') {
        const xFrac = Math.min(Math.max(drag.startRect.xFrac + dxFrac, 0), 1 - drag.startRect.wFrac);
        const yFrac = Math.min(Math.max(drag.startRect.yFrac + dyFrac, 0), 1 - drag.startRect.hFrac);
        onRectChange({ ...drag.startRect, xFrac, yFrac });
      } else {
        const wFrac = Math.min(Math.max(drag.startRect.wFrac + dxFrac, MIN_FRAME_FRACTION), 1 - drag.startRect.xFrac);
        const hFrac = Math.min(Math.max(drag.startRect.hFrac + dyFrac, MIN_FRAME_FRACTION), 1 - drag.startRect.yFrac);
        onRectChange({ ...drag.startRect, wFrac, hFrac });
      }
    },
    [containerRef, onRectChange],
  );

  const endDrag = useCallback((event: React.PointerEvent) => {
    if (dragRef.current?.pointerId === event.pointerId) dragRef.current = null;
  }, []);

  const style = {
    '--frame-x': `${rect.xFrac * 100}%`,
    '--frame-y': `${rect.yFrac * 100}%`,
    '--frame-w': `${rect.wFrac * 100}%`,
    '--frame-h': `${rect.hFrac * 100}%`,
  } as CSSProperties;

  return (
    <div className="export-frame" style={style} data-testid="export-frame">
      <div
        className="export-frame__box"
        data-testid="export-frame-box"
        onPointerDown={(event) => beginDrag('move', event)}
        onPointerMove={onDragMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
      />
      {preset === 'custom' && (
        <div
          className="export-frame__handle"
          data-testid="export-frame-handle"
          onPointerDown={(event) => beginDrag('resize', event)}
          onPointerMove={onDragMove}
          onPointerUp={endDrag}
          onPointerCancel={endDrag}
        />
      )}
      <button
        type="button"
        className="export-frame__close"
        data-testid="export-frame-close"
        aria-label="Cancel export"
        onClick={onCancel}
      >
        <CloseIcon />
      </button>
      <button
        type="button"
        className="export-frame__capture"
        data-testid="export-frame-capture"
        aria-label="Capture export"
        onClick={onCapture}
        disabled={busy}
      >
        {busy ? <span className="export-frame__spinner" aria-hidden="true" /> : <CameraIcon />}
      </button>
      {error && (
        <p className="export-frame__error" role="alert" data-testid="export-frame-error">
          {error}
        </p>
      )}
    </div>
  );
}

function CameraIcon() {
  return (
    <svg viewBox="0 0 16 16" width="15" height="15">
      <path
        d="M2 5.5 A1 1 0 0 1 3 4.5 H5.2 L6 3 H10 L10.8 4.5 H13 A1 1 0 0 1 14 5.5 V12 A1 1 0 0 1 13 13 H3 A1 1 0 0 1 2 12 Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.3"
        strokeLinejoin="round"
      />
      <circle cx="8" cy="8.5" r="2.6" fill="none" stroke="currentColor" strokeWidth="1.3" />
    </svg>
  );
}

function CloseIcon() {
  return (
    <svg viewBox="0 0 16 16" width="13" height="13">
      <line x1="3" y1="3" x2="13" y2="13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <line x1="13" y1="3" x2="3" y2="13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}
