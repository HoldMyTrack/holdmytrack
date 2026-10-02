import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from 'react';
import { createPortal } from 'react-dom';
import { ChevronLeft, ChevronRight, ExternalLink, Maximize, X, ZoomIn, ZoomOut } from 'lucide-react';
import { API_BASE_URL, type Photo } from '../api';
import { t } from '../i18n';
import { formatStartedAt } from './format';
import { DOUBLE_CLICK_SCALE, FIT_VIEW, MAX_SCALE, ZOOM_STEP, clampView, fitSize, panBy, zoomAt, type Size, type ZoomView } from './photoZoom';

export interface PhotoViewerProps {
  /** The popup's photos: one, or a group's in route order. */
  photos: readonly Photo[];
  index: number;
  onIndex: (index: number) => void;
  onClose: () => void;
}

/**
 * A photo over the whole page (FR-16.8), opened from its popup: zoomed with the wheel, a pinch,
 * a double-click or + and −, and dragged about once zoomed in. Steps through a group as the popup
 * does, and with it — they share the index. A real `<dialog>`/`showModal()`, as ConfirmDialog.tsx
 * is, for Escape and the focus trap; portaled to the body, so nothing in it reaches the map. The
 * zoom itself is `photoZoom.ts`'s math applied as a CSS transform on the fitted picture.
 */
export function PhotoViewer({ photos, index, onIndex, onClose }: PhotoViewerProps) {
  const photo = photos[Math.min(index, photos.length - 1)]!;
  const dialogRef = useRef<HTMLDialogElement>(null);
  const stageRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [stage, setStage] = useState<Size>({ width: 0, height: 0 });
  const [view, setView] = useState<ZoomView>(FIT_VIEW);
  // Eased for a step (a button, a key, a double-click); not while following a wheel or a finger.
  const [smooth, setSmooth] = useState(false);
  const [dragging, setDragging] = useState(false);
  const pointers = useRef(new Map<number, { x: number; y: number }>());

  const fitted = fitSize({ width: photo.width, height: photo.height }, stage);
  // The wheel listener is attached once; it reads the current sizes from here.
  const geometry = useRef({ fitted, stage });
  geometry.current = { fitted, stage };

  useEffect(() => {
    const el = dialogRef.current;
    if (el && !el.open) el.showModal();
    // Not the first button, as the dialog would pick: Enter there would step to the next photo.
    closeRef.current?.focus();
  }, []);

  useLayoutEffect(() => {
    const el = stageRef.current;
    if (!el) return;
    const measure = () => setStage({ width: el.clientWidth, height: el.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // A resized window can leave an edge pulled in; another photo starts fitted.
  useEffect(() => setView((v) => clampView(v, fitted, stage)), [stage.width, stage.height]);
  useEffect(() => setView(FIT_VIEW), [photo.id]);

  // From the stage's center, where the view's x/y are measured from.
  const toStage = (clientX: number, clientY: number) => {
    const rect = stageRef.current!.getBoundingClientRect();
    return { x: clientX - rect.left - rect.width / 2, y: clientY - rect.top - rect.height / 2 };
  };

  // React's onWheel is passive, and the page mustn't scroll under the zoom.
  useEffect(() => {
    const el = stageRef.current;
    if (!el) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      const { fitted, stage } = geometry.current;
      // A trackpad pinch arrives as a ctrl+wheel with small deltas; a mouse's lines are big steps.
      const rate = event.ctrlKey ? 0.01 : event.deltaMode === 1 ? 0.05 : 0.002;
      const point = toStage(event.clientX, event.clientY);
      setSmooth(false);
      setView((v) => zoomAt(v, Math.exp(-event.deltaY * rate), point, fitted, stage));
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, []);

  const step = (next: (v: ZoomView) => ZoomView) => {
    setSmooth(true);
    setView(next);
  };
  const zoomIn = () => step((v) => zoomAt(v, ZOOM_STEP, { x: 0, y: 0 }, fitted, stage));
  const zoomOut = () => step((v) => zoomAt(v, 1 / ZOOM_STEP, { x: 0, y: 0 }, fitted, stage));
  const fit = () => step(() => FIT_VIEW);

  const onPointerDown = (event: ReactPointerEvent) => {
    if (event.pointerType === 'mouse' && event.button !== 0) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    setDragging(true);
  };

  const onPointerMove = (event: ReactPointerEvent) => {
    const all = pointers.current;
    const before = all.get(event.pointerId);
    if (!before) return;
    const after = { x: event.clientX, y: event.clientY };
    setSmooth(false);
    if (all.size === 1) {
      setView((v) => panBy(v, after.x - before.x, after.y - before.y, fitted, stage));
    } else if (all.size === 2) {
      // A pinch: scale by how far the fingers spread, about the point between them, and follow
      // that point as it moves.
      const other = [...all].find(([id]) => id !== event.pointerId)![1];
      const spreadBefore = Math.hypot(before.x - other.x, before.y - other.y);
      const spreadAfter = Math.hypot(after.x - other.x, after.y - other.y);
      const midBefore = toStage((before.x + other.x) / 2, (before.y + other.y) / 2);
      const midAfter = toStage((after.x + other.x) / 2, (after.y + other.y) / 2);
      if (spreadBefore > 0) {
        setView((v) =>
          panBy(zoomAt(v, spreadAfter / spreadBefore, midBefore, fitted, stage), midAfter.x - midBefore.x, midAfter.y - midBefore.y, fitted, stage),
        );
      }
    }
    all.set(event.pointerId, after);
  };

  const onPointerUp = (event: ReactPointerEvent) => {
    pointers.current.delete(event.pointerId);
    if (pointers.current.size === 0) setDragging(false);
  };

  const onDoubleClick = (event: ReactMouseEvent) => {
    const point = toStage(event.clientX, event.clientY);
    step((v) => (v.scale > 1 ? FIT_VIEW : zoomAt(v, DOUBLE_CLICK_SCALE, point, fitted, stage)));
  };

  const onKeyDown = (event: ReactKeyboardEvent) => {
    switch (event.key) {
      case '+':
      case '=':
        zoomIn();
        break;
      case '-':
        zoomOut();
        break;
      case '0':
        fit();
        break;
      case 'ArrowLeft':
        if (index > 0) onIndex(index - 1);
        break;
      case 'ArrowRight':
        if (index < photos.length - 1) onIndex(index + 1);
        break;
      default:
        return;
    }
    event.preventDefault();
  };

  const full = API_BASE_URL + photo.url;
  const zoomed = view.scale > 1;
  const stageClass = ['photo-viewer__stage', zoomed && 'photo-viewer__stage--zoomed', dragging && 'photo-viewer__stage--dragging'].filter(Boolean).join(' ');

  return createPortal(
    <dialog ref={dialogRef} className="photo-viewer" data-testid="photo-viewer" aria-label={photo.caption ?? t('photos.view')} onClose={onClose} onKeyDown={onKeyDown}>
      <div className="photo-viewer__bar">
        {photos.length > 1 && (
          <div className="photo-viewer__nav">
            <button type="button" className="photo-viewer__btn" aria-label={t('photos.previous')} disabled={index === 0} onClick={() => onIndex(index - 1)}>
              <ChevronLeft size={18} aria-hidden="true" />
            </button>
            <span className="photo-viewer__position">{t('photos.position', { n: index + 1, total: photos.length })}</span>
            <button
              type="button"
              className="photo-viewer__btn"
              aria-label={t('photos.next')}
              disabled={index === photos.length - 1}
              onClick={() => onIndex(index + 1)}
            >
              <ChevronRight size={18} aria-hidden="true" />
            </button>
          </div>
        )}
        <div className="photo-viewer__text">
          {photo.caption && <span className="photo-viewer__caption">{photo.caption}</span>}
          {photo.takenAt && <span className="photo-viewer__taken">{t('photos.taken', { when: formatStartedAt(photo.takenAt) })}</span>}
        </div>
        <div className="photo-viewer__tools">
          <button type="button" className="photo-viewer__btn" aria-label={t('photos.zoom_out')} title={t('photos.zoom_out')} disabled={!zoomed} onClick={zoomOut}>
            <ZoomOut size={18} aria-hidden="true" />
          </button>
          <button
            type="button"
            className="photo-viewer__btn"
            aria-label={t('photos.zoom_in')}
            title={t('photos.zoom_in')}
            disabled={view.scale >= MAX_SCALE}
            onClick={zoomIn}
            data-testid="photo-viewer-zoom-in"
          >
            <ZoomIn size={18} aria-hidden="true" />
          </button>
          <button type="button" className="photo-viewer__btn" aria-label={t('photos.zoom_fit')} title={t('photos.zoom_fit')} disabled={!zoomed} onClick={fit}>
            <Maximize size={18} aria-hidden="true" />
          </button>
          <a className="photo-viewer__btn" href={full} target="_blank" rel="noopener noreferrer" aria-label={t('photos.full_size')} title={t('photos.full_size')}>
            <ExternalLink size={18} aria-hidden="true" />
          </a>
          <button
            ref={closeRef}
            type="button"
            className="photo-viewer__btn"
            aria-label={t('photos.close_viewer')}
            title={t('photos.close_viewer')}
            onClick={() => dialogRef.current?.close()}
          >
            <X size={18} aria-hidden="true" />
          </button>
        </div>
      </div>
      <div
        ref={stageRef}
        className={stageClass}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onDoubleClick={onDoubleClick}
      >
        <img
          className={smooth ? 'photo-viewer__image photo-viewer__image--smooth' : 'photo-viewer__image'}
          src={full}
          alt={photo.caption ?? ''}
          draggable={false}
          style={{ width: fitted.width, height: fitted.height, transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}
          data-testid="photo-viewer-image"
        />
      </div>
    </dialog>,
    document.body,
  );
}
