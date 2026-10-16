import { useEffect, useRef, useState } from 'react';
import type { CSSProperties, PointerEvent as ReactPointerEvent, RefObject } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { Camera, ChevronDown, X } from 'lucide-react';
import { EXPORT_PLATFORMS, EXPORT_PRESET_ROWS, EXPORT_PRESETS, type ExportPreset } from '../map/exportPresets';
import {
  choiceValue,
  CUSTOM_SIZES,
  customSizeAvailable,
  fitWindow,
  outputSize,
  parseChoice,
  resizeWindow,
  settleWindow,
  viewfinderArea,
  windowMoved,
  type Corner,
  type CustomSizeId,
  type ExportChoice,
  type Rect,
} from '../map/exportViewfinder';
import { t } from '../i18n';

/** EXPORT_PRESET_ROWS' names as the toolbar shows them — the ids stay English. */
const ROW_LABELS = {
  Square: 'export.row_square',
  Portrait: 'export.row_portrait',
  Landscape: 'export.row_landscape',
  Story: 'export.row_story',
} as const;

const CORNERS: Corner[] = ['nw', 'ne', 'sw', 'se'];

/** How long a settle takes: the window's CSS transition and the map's ease share it. */
const SETTLE_MS = 300;

export interface ExportViewfinderProps {
  map: MapLibreMap;
  /** The live map's own container element — its size is the viewfinder's. */
  containerRef: RefObject<HTMLDivElement | null>;
  preset: ExportPreset | 'custom';
  customSize: CustomSizeId;
  /** The longest side this browser can draw an image at (`exportMap.ts`'s `maxExportSide`). */
  maxSidePx: number;
  /** The window, in the container's CSS pixels. */
  window: Rect;
  busy: boolean;
  error: string | null;
  onWindowChange: (window: Rect) => void;
  onChoiceChange: (choice: ExportChoice) => void;
  onCapture: () => void;
  onCancel: () => void;
}

interface DragState {
  pointerId: number;
  corner: Corner;
  startX: number;
  startY: number;
  start: Rect;
}

/**
 * The export viewfinder (FR-4.10, `IMPLEMENTATION.md` §4.3.3): the map dimmed around a window
 * fixed in its middle, which is what the picture shows. The map moves under the window, and only
 * there: four dark panels cover the rest and take its pointer events, so the map's own controls
 * and the map outside the window don't respond. Its geometry is `map/exportViewfinder.ts`.
 *
 * A corner drag crops the window, the opposite corner staying put; on release the window
 * settles back to full size and the map eases by the same zoom, so the cropped part fills it.
 *
 * A pinch that starts inside the window keeps working when a finger lands on a dark panel: from
 * the first touch inside until the last finger lifts, the panels let touches through to the map.
 * MapLibre only counts touches whose target is inside its own container, so without this a
 * pinch begun near the window's edge would lose its second finger.
 */
export function ExportViewfinder({
  map,
  containerRef,
  preset,
  customSize,
  maxSidePx,
  window: win,
  busy,
  error,
  onWindowChange,
  onChoiceChange,
  onCapture,
  onCancel,
}: ExportViewfinderProps) {
  const dragRef = useRef<DragState | null>(null);
  const [settling, setSettling] = useState(false);
  const [gesture, setGesture] = useState(false);
  const aspect = preset === 'custom' ? null : preset.widthPx / preset.heightPx;

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onCancel();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [onCancel]);

  // A resized map (a turned phone, a resized window) refits the window in its own shape.
  const latest = useRef({ win, onWindowChange });
  latest.current = { win, onWindowChange };
  useEffect(() => {
    const refit = () => {
      const el = containerRef.current;
      if (!el) return;
      const { win: current, onWindowChange: change } = latest.current;
      change(fitWindow(current.w / current.h, viewfinderArea(el.clientWidth, el.clientHeight)));
    };
    map.on('resize', refit);
    return () => {
      map.off('resize', refit);
    };
  }, [map, containerRef]);

  // Touches that start on the map, inside the window, open the panels to the rest of the gesture.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const onStart = () => setGesture(true);
    const onEnd = (event: TouchEvent) => {
      if (event.touches.length === 0) setGesture(false);
    };
    el.addEventListener('touchstart', onStart, { capture: true, passive: true });
    window.addEventListener('touchend', onEnd, { passive: true });
    window.addEventListener('touchcancel', onEnd, { passive: true });
    return () => {
      el.removeEventListener('touchstart', onStart, { capture: true });
      window.removeEventListener('touchend', onEnd);
      window.removeEventListener('touchcancel', onEnd);
    };
  }, [containerRef]);

  const beginDrag = (corner: Corner, event: ReactPointerEvent) => {
    if (busy) return;
    event.preventDefault();
    event.stopPropagation();
    event.currentTarget.setPointerCapture(event.pointerId);
    setSettling(false);
    dragRef.current = { pointerId: event.pointerId, corner, startX: event.clientX, startY: event.clientY, start: win };
  };

  const onDragMove = (event: ReactPointerEvent) => {
    const drag = dragRef.current;
    const el = containerRef.current;
    if (!drag || !el || event.pointerId !== drag.pointerId) return;
    event.preventDefault();
    const area = viewfinderArea(el.clientWidth, el.clientHeight);
    onWindowChange(resizeWindow(drag.start, drag.corner, event.clientX - drag.startX, event.clientY - drag.startY, aspect, area));
  };

  const endDrag = (event: ReactPointerEvent) => {
    const drag = dragRef.current;
    const el = containerRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    dragRef.current = null;
    if (!el || !windowMoved(drag.start, win)) return;
    const { window: settled, zoomDelta, focus } = settleWindow(win, viewfinderArea(el.clientWidth, el.clientHeight));
    const center = map.unproject([focus.x, focus.y]);
    setSettling(true);
    map.easeTo({
      center,
      zoom: map.getZoom() + zoomDelta,
      // The window's middle, not the map's, is where the crop's middle has to land.
      offset: [settled.x + settled.w / 2 - el.clientWidth / 2, settled.y + settled.h / 2 - el.clientHeight / 2],
      duration: SETTLE_MS,
    });
    onWindowChange(settled);
  };

  const dragHandlers = { onPointerMove: onDragMove, onPointerUp: endDrag, onPointerCancel: endDrag };
  const size = outputSize(preset, customSize, win);
  const currentSize = CUSTOM_SIZES.find((option) => option.id === customSize) ?? CUSTOM_SIZES[0];
  const style = {
    '--win-x': `${win.x}px`,
    '--win-y': `${win.y}px`,
    '--win-w': `${win.w}px`,
    '--win-h': `${win.h}px`,
    '--settle-ms': `${SETTLE_MS}ms`,
  } as CSSProperties;
  const classes = ['export-viewfinder', settling && 'export-viewfinder--settling', gesture && 'export-viewfinder--gesture']
    .filter(Boolean)
    .join(' ');

  return (
    <div className={classes} style={style} data-testid="export-viewfinder" onTransitionEnd={() => setSettling(false)}>
      <div className="export-viewfinder__shade export-viewfinder__shade--top" />
      <div className="export-viewfinder__shade export-viewfinder__shade--bottom" />
      <div className="export-viewfinder__shade export-viewfinder__shade--left" />
      <div className="export-viewfinder__shade export-viewfinder__shade--right" />

      <div className="export-viewfinder__window" data-testid="export-viewfinder-window">
        {CORNERS.map((corner) => (
          <div
            key={corner}
            className={`export-viewfinder__handle export-viewfinder__handle--${corner}`}
            data-testid={`export-viewfinder-handle-${corner}`}
            onPointerDown={(event) => beginDrag(corner, event)}
            {...dragHandlers}
          />
        ))}
      </div>

      <div className="export-viewfinder__toolbar" data-testid="export-viewfinder-toolbar">
        {/* One menu of shape-and-size pairs: Custom at each size, then each platform's presets.
            Its list says what a Custom size is ("Custom · HD (1920 px long side)"); closed, it
            shows only "Custom · HD", which fits a phone's toolbar. The native select sits
            invisibly over that short label, so it still opens, takes focus and is read out as
            itself. */}
        <span className="export-viewfinder__select export-viewfinder__picker">
          <span aria-hidden="true">
            {preset === 'custom' ? t('export.custom_size', { name: currentSize.label }) : `${preset.group} · ${t(ROW_LABELS[preset.row as keyof typeof ROW_LABELS])}`}
          </span>
          <ChevronDown size={14} aria-hidden="true" />
          <select
            data-testid="export-viewfinder-choice"
            aria-label={t('export.shape')}
            value={choiceValue(preset, customSize)}
            disabled={busy}
            onChange={(event) => {
              const choice = parseChoice(event.target.value, EXPORT_PRESETS);
              if (choice) onChoiceChange(choice);
            }}
          >
            <optgroup label={t('export.custom')}>
              {CUSTOM_SIZES.map((option) => (
                <option key={option.id} value={`custom-${option.id}`} disabled={!customSizeAvailable(option.id, maxSidePx)}>
                  {t('export.custom_size_full', { name: option.label, px: String(option.longSidePx) })}
                </option>
              ))}
            </optgroup>
            {EXPORT_PLATFORMS.map((platform) => (
              <optgroup key={platform} label={platform}>
                {EXPORT_PRESET_ROWS.map((row) => {
                  const p = EXPORT_PRESETS.find((candidate) => candidate.group === platform && candidate.row === row);
                  return (
                    p && (
                      <option key={p.id} value={p.id}>
                        {platform} · {t(ROW_LABELS[row])}
                      </option>
                    )
                  );
                })}
              </optgroup>
            ))}
          </select>
        </span>
        <button type="button" className="export-viewfinder__button" data-testid="export-viewfinder-close" aria-label={t('export.cancel')} onClick={onCancel}>
          <X size={16} />
        </button>
        <button
          type="button"
          className="export-viewfinder__button export-viewfinder__button--capture"
          data-testid="export-viewfinder-capture"
          aria-label={t('export.capture')}
          title={t('export.capture')}
          onClick={onCapture}
          disabled={busy}
        >
          {busy ? <span className="export-viewfinder__spinner" aria-hidden="true" /> : <Camera size={16} />}
        </button>
      </div>

      {error ? (
        <p className="export-viewfinder__note export-viewfinder__note--error" role="alert" data-testid="export-viewfinder-error">
          {error}
        </p>
      ) : (
        <p className="export-viewfinder__note" data-testid="export-viewfinder-dimensions">
          {size.widthPx} × {size.heightPx}
        </p>
      )}
    </div>
  );
}
