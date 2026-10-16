import { useCallback, useMemo, useRef, useState, type RefObject } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { exportFlowReducer, type ExportEvent, type ExportFlow } from './exportFlow';
import { exportFramedImage, maxExportSide, type ExportViewState } from './exportMap';
import { DEFAULT_CUSTOM_SIZE, outputSize, usableCustomSize, type CustomSizeId, type ExportChoice } from './exportViewfinder';
import type { Rect } from './exportViewfinder';
import { t } from '../i18n';

export interface ExportControls {
  flow: ExportFlow;
  error: string | null;
  maxSidePx: number;
  /** The map control: opens or closes the viewfinder. */
  toggle: () => void;
  changeChoice: (choice: ExportChoice) => void;
  moveWindow: (window: Rect) => void;
  capture: () => Promise<void>;
  cancel: () => void;
}

/** The map image export (FR-4.10): the viewfinder's stages (exportFlow.ts) and the capture of
 *  what [scene] draws into a downloaded PNG. */
export function useExportFlow(map: MapLibreMap | null, container: RefObject<HTMLDivElement | null>, scene: ExportViewState): ExportControls {
  const [flow, setFlow] = useState<ExportFlow>({ stage: 'idle' });
  const dispatch = useCallback((event: ExportEvent) => setFlow((current) => exportFlowReducer(current, event)), []);
  const [error, setError] = useState<string | null>(null);
  // The Custom size last picked, kept for the next time the viewfinder opens in this visit.
  const customSizeRef = useRef<CustomSizeId>(DEFAULT_CUSTOM_SIZE);
  const maxSidePx = useMemo(() => (map ? maxExportSide(map) : 4096), [map]);

  const toggle = useCallback(() => {
    const el = container.current;
    if (!map || !el) return;
    setError(null);
    dispatch({ type: 'toggle', customSize: usableCustomSize(customSizeRef.current, maxSidePx), container: { width: el.clientWidth, height: el.clientHeight } });
  }, [map, container, maxSidePx, dispatch]);

  const changeChoice = useCallback(
    (choice: ExportChoice) => {
      const el = container.current;
      if (!el) return;
      if (choice.preset === 'custom') customSizeRef.current = choice.customSize;
      dispatch({ type: 'choice', choice, container: { width: el.clientWidth, height: el.clientHeight } });
    },
    [container, dispatch],
  );

  const moveWindow = useCallback((window: Rect) => dispatch({ type: 'window', window }), [dispatch]);

  const capture = useCallback(async () => {
    if (flow.stage !== 'framing' || !map) return;
    const { preset, customSize, window: win } = flow;
    dispatch({ type: 'capture' });
    setError(null);
    try {
      const center = map.unproject([win.x + win.w / 2, win.y + win.h / 2]);
      const blob = await exportFramedImage(map, scene, {
        center: { lng: center.lng, lat: center.lat },
        widthPx: win.w,
        heightPx: win.h,
        target: outputSize(preset, customSize, win),
      });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `holdmytrack-${new Date().toISOString().slice(0, 10)}.png`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      dispatch({ type: 'close' });
    } catch (err) {
      // The cause (a canvas or render timeout) is for developers; the user gets a generic message.
      console.error('export failed', err);
      setError(t('export.failed'));
      dispatch({ type: 'failed' });
    }
  }, [flow, map, scene, dispatch]);

  const cancel = useCallback(() => {
    dispatch({ type: 'close' });
    setError(null);
  }, [dispatch]);

  return { flow, error, maxSidePx, toggle, changeChoice, moveWindow, capture, cancel };
}
