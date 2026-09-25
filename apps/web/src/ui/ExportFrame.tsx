import { useCallback, useEffect, useLayoutEffect, useRef } from 'react';
import type { CSSProperties, PointerEvent as ReactPointerEvent, RefObject } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { Camera, X } from 'lucide-react';
import { customOutputSize, EXPORT_PLATFORMS, EXPORT_PRESET_ROWS, EXPORT_PRESETS, type ExportPreset } from '../map/exportPresets';

/** Where the frame is: its center as a map position (so it travels with the map when it
 *  pans), and its size in CSS pixels (so it keeps its on-screen size when the map zooms —
 *  zooming changes how much map the frame holds, not how big the frame looks). */
export interface FrameGeometry {
  center: { lng: number; lat: number };
  widthPx: number;
  heightPx: number;
}

const MIN_FRAME_PX = 80;
/** The toolbar's height plus its gap above the frame — when the frame's top edge is closer
 *  than this to the map's top, the toolbar flips inside the frame instead of going off-screen. */
const TOOLBAR_SPACE_PX = 46;

export interface ExportFrameProps {
  map: MapLibreMap;
  /** The live map's own container element — read for its size during drag math only. */
  containerRef: RefObject<HTMLDivElement | null>;
  preset: ExportPreset | 'custom';
  geometry: FrameGeometry;
  busy: boolean;
  error: string | null;
  onGeometryChange: (geometry: FrameGeometry) => void;
  onPresetChange: (preset: ExportPreset | 'custom') => void;
  onCapture: () => void;
  onCancel: () => void;
}

type Corner = 'nw' | 'ne' | 'sw' | 'se';
const CORNERS: Corner[] = ['nw', 'ne', 'sw', 'se'];
const EDGES = ['n', 'e', 's', 'w'] as const;

interface DragState {
  pointerId: number;
  /** 'move', or the corner being dragged for a resize. */
  mode: 'move' | Corner;
  startX: number;
  startY: number;
  /** The frame's center in container pixels at drag start, and its size. */
  cx: number;
  cy: number;
  w: number;
  h: number;
}

/**
 * The export frame over the live map (`FR-4.10`), rendered as a sibling of `.map-canvas`
 * inside `.map-root`. Anchored to the map, not the screen: it stores a map position for its
 * center and re-projects it on every MapLibre `move` (which also fires for zoom and rotate),
 * writing the result straight onto its own root as `--frame-cx`/`--frame-cy` — no React
 * render per animation frame while the map pans.
 *
 * It blocks nothing on the map: the wrapper and the frame box, interior included, are
 * `pointer-events: none`, so pan/zoom/click reach the map inside the frame as well as outside
 * it. The only hit targets are the four edge strips straddling the dashed border (drag to
 * move the frame), the four corner handles (drag to resize — a preset's aspect ratio stays
 * locked, since its output size is fixed), and the toolbar above the top edge (shape
 * dropdown, Close, Capture), which flips inside the frame when there's no room above it.
 *
 * Pointer-capture drag, not a drag library — this app hand-rolls every drag interaction
 * (`RangePicker.tsx`, `ActivitiesPanel.tsx`'s panel-width handle) the same way: `onPointerDown`
 * calls `setPointerCapture` and stashes start state in a ref, `onPointerMove` reads it back and
 * calls `event.preventDefault()` up front per `RangePicker.tsx`'s own documented note on why
 * (Firefox hijacks a later drag into a native ghost-image drag once a pointer path crosses text).
 */
export function ExportFrame({
  map,
  containerRef,
  preset,
  geometry,
  busy,
  error,
  onGeometryChange,
  onPresetChange,
  onCapture,
  onCancel,
}: ExportFrameProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<DragState | null>(null);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onCancel();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [onCancel]);

  const { center, widthPx, heightPx } = geometry;

  const reposition = useCallback(() => {
    const root = rootRef.current;
    if (!root) return;
    const p = map.project([center.lng, center.lat]);
    root.style.setProperty('--frame-cx', `${p.x}px`);
    root.style.setProperty('--frame-cy', `${p.y}px`);
    root.classList.toggle('export-frame--toolbar-inside', p.y - heightPx / 2 < TOOLBAR_SPACE_PX);
  }, [map, center.lng, center.lat, heightPx]);

  // Layout effect, so a changed center/size is painted in its new place on the same frame,
  // never one frame at a stale position; `move` covers the camera, `resize` the container.
  useLayoutEffect(() => {
    reposition();
    map.on('move', reposition);
    map.on('resize', reposition);
    return () => {
      map.off('move', reposition);
      map.off('resize', reposition);
    };
  }, [map, reposition]);

  const beginDrag = (mode: DragState['mode'], event: ReactPointerEvent) => {
    event.preventDefault();
    event.stopPropagation();
    event.currentTarget.setPointerCapture(event.pointerId);
    const p = map.project([center.lng, center.lat]);
    dragRef.current = { pointerId: event.pointerId, mode, startX: event.clientX, startY: event.clientY, cx: p.x, cy: p.y, w: widthPx, h: heightPx };
  };

  const onDragMove = (event: ReactPointerEvent) => {
    const drag = dragRef.current;
    const el = containerRef.current;
    if (!drag || !el || event.pointerId !== drag.pointerId) return;
    event.preventDefault();
    const W = el.clientWidth;
    const H = el.clientHeight;
    const dx = event.clientX - drag.startX;
    const dy = event.clientY - drag.startY;

    if (drag.mode === 'move') {
      // The center stays inside the map while dragging, so an edge is always left to grab —
      // panning the map can still carry the frame off-screen, and that's fine.
      const x = Math.min(Math.max(drag.cx + dx, 0), W);
      const y = Math.min(Math.max(drag.cy + dy, 0), H);
      const ll = map.unproject([x, y]);
      onGeometryChange({ center: { lng: ll.lng, lat: ll.lat }, widthPx: drag.w, heightPx: drag.h });
      return;
    }

    // Resize: the opposite corner stays put, the dragged one follows the pointer.
    const sx = drag.mode.endsWith('e') ? 1 : -1;
    const sy = drag.mode.startsWith('s') ? 1 : -1;
    const ax = drag.cx - (sx * drag.w) / 2;
    const ay = drag.cy - (sy * drag.h) / 2;
    // The dragged corner can't leave the map, so its handle is never dragged out of reach.
    const maxW = Math.max(MIN_FRAME_PX, sx > 0 ? W - ax : ax);
    const maxH = Math.max(MIN_FRAME_PX, sy > 0 ? H - ay : ay);
    let w = drag.w + sx * dx;
    let h = drag.h + sy * dy;
    if (preset !== 'custom') {
      // Locked ratio: follow whichever axis the pointer has pushed further.
      const ratio = preset.widthPx / preset.heightPx;
      if (w / ratio >= h) h = w / ratio;
      else w = h * ratio;
      const shrink = Math.min(1, maxW / w, maxH / h);
      const grow = Math.max(1, MIN_FRAME_PX / w, MIN_FRAME_PX / h);
      const k = shrink < 1 ? shrink : grow;
      w *= k;
      h *= k;
    } else {
      w = Math.min(Math.max(w, MIN_FRAME_PX), maxW);
      h = Math.min(Math.max(h, MIN_FRAME_PX), maxH);
    }
    const ll = map.unproject([ax + (sx * w) / 2, ay + (sy * h) / 2]);
    onGeometryChange({ center: { lng: ll.lng, lat: ll.lat }, widthPx: w, heightPx: h });
  };

  const endDrag = (event: ReactPointerEvent) => {
    if (dragRef.current?.pointerId === event.pointerId) dragRef.current = null;
  };

  const dragHandlers = { onPointerMove: onDragMove, onPointerUp: endDrag, onPointerCancel: endDrag };
  const customSize = customOutputSize(widthPx, heightPx);
  const style = { '--frame-w': `${widthPx}px`, '--frame-h': `${heightPx}px` } as CSSProperties;

  return (
    <div ref={rootRef} className="export-frame" style={style} data-testid="export-frame">
      <div className="export-frame__box" data-testid="export-frame-box">
        {EDGES.map((edge) => (
          <div
            key={edge}
            className={`export-frame__edge export-frame__edge--${edge}`}
            data-testid={`export-frame-edge-${edge}`}
            onPointerDown={(event) => beginDrag('move', event)}
            {...dragHandlers}
          />
        ))}
        {CORNERS.map((corner) => (
          <div
            key={corner}
            className={`export-frame__handle export-frame__handle--${corner}`}
            data-testid={`export-frame-handle-${corner}`}
            onPointerDown={(event) => beginDrag(corner, event)}
            {...dragHandlers}
          />
        ))}
      </div>

      <div className="export-frame__toolbar" data-testid="export-frame-toolbar">
        <select
          className="export-frame__shape"
          data-testid="export-frame-shape"
          aria-label="Export shape"
          value={preset === 'custom' ? 'custom' : preset.id}
          onChange={(event) => {
            const next = EXPORT_PRESETS.find((p) => p.id === event.target.value);
            onPresetChange(next ?? 'custom');
          }}
        >
          <option value="custom">
            Custom · {customSize.widthPx}×{customSize.heightPx}
          </option>
          {EXPORT_PLATFORMS.map((platform) => (
            <optgroup key={platform} label={platform}>
              {EXPORT_PRESET_ROWS.map((row) => {
                const p = EXPORT_PRESETS.find((candidate) => candidate.group === platform && candidate.row === row);
                return (
                  p && (
                    <option key={p.id} value={p.id}>
                      {row} · {p.widthPx}×{p.heightPx}
                    </option>
                  )
                );
              })}
            </optgroup>
          ))}
        </select>
        <button type="button" className="export-frame__button" data-testid="export-frame-close" aria-label="Cancel export" onClick={onCancel}>
          <X size={14} />
        </button>
        <button
          type="button"
          className="export-frame__button export-frame__button--capture"
          data-testid="export-frame-capture"
          aria-label="Capture export"
          onClick={onCapture}
          disabled={busy}
        >
          {busy ? <span className="export-frame__spinner" aria-hidden="true" /> : <Camera size={16} />}
        </button>
      </div>

      {error && (
        <p className="export-frame__error" role="alert" data-testid="export-frame-error">
          {error}
        </p>
      )}
    </div>
  );
}
