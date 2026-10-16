/**
 * The export viewfinder's stages (FR-4.10) as a reducer. The state lives in `MapView` because it
 * spans `ExportViewfinder`, `ExportControl` and the map instance capture needs. Windows are in the
 * map container's CSS pixels.
 */
import type { ExportPreset } from './exportPresets';
import { fitWindow, viewfinderArea, type CustomSizeId, type ExportChoice, type Rect } from './exportViewfinder.ts';

export type ExportFlow =
  | { stage: 'idle' }
  | { stage: 'framing' | 'capturing'; preset: ExportPreset | 'custom'; customSize: CustomSizeId; window: Rect };

/** The map container's size. */
export interface ContainerSize {
  width: number;
  height: number;
}

export type ExportEvent =
  /** The map control: opens framing on Custom, or closes it. Mid-capture it does nothing. */
  | { type: 'toggle'; customSize: CustomSizeId; container: ContainerSize }
  /** Another preset or Custom size picked while framing. */
  | { type: 'choice'; choice: ExportChoice; container: ContainerSize }
  /** The window dragged while framing. */
  | { type: 'window'; window: Rect }
  | { type: 'capture' }
  /** A failed capture returns to framing, keeping the framed view. */
  | { type: 'failed' }
  | { type: 'close' };

/** A shape's window: the largest it can be in the map's free area. Custom takes the whole area. */
export function shapeWindow(preset: ExportPreset | 'custom', container: ContainerSize): Rect {
  return fitWindow(preset === 'custom' ? null : preset.widthPx / preset.heightPx, viewfinderArea(container.width, container.height));
}

export function exportFlowReducer(flow: ExportFlow, event: ExportEvent): ExportFlow {
  switch (event.type) {
    case 'toggle':
      if (flow.stage === 'framing') return { stage: 'idle' };
      if (flow.stage === 'capturing') return flow;
      return { stage: 'framing', preset: 'custom', customSize: event.customSize, window: shapeWindow('custom', event.container) };
    case 'choice':
      if (flow.stage !== 'framing') return flow;
      // Another Custom size keeps the window; any other choice fits a window of the new shape.
      if (event.choice.preset === 'custom' && flow.preset === 'custom') return { ...flow, customSize: event.choice.customSize };
      return { ...flow, ...event.choice, window: shapeWindow(event.choice.preset, event.container) };
    case 'window':
      return flow.stage === 'framing' ? { ...flow, window: event.window } : flow;
    case 'capture':
      return flow.stage === 'framing' ? { ...flow, stage: 'capturing' } : flow;
    case 'failed':
      return flow.stage === 'capturing' ? { ...flow, stage: 'framing' } : flow;
    case 'close':
      return { stage: 'idle' };
  }
}
