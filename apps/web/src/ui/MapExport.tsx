import type { RefObject } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import type { ExportControls } from '../map/useExportFlow';
import { ExportControl } from './ExportControl';
import { ExportViewfinder } from './ExportViewfinder';

/** The map image export (FR-4.10): its map control, and the viewfinder while it's open (useExportFlow.ts). */
export function MapExport({ map, container, exporter }: { map: MapLibreMap; container: RefObject<HTMLDivElement | null>; exporter: ExportControls }) {
  const { flow } = exporter;
  return (
    <>
      <ExportControl map={map} active={flow.stage !== 'idle'} onOpen={exporter.toggle} />
      {flow.stage !== 'idle' && (
        <ExportViewfinder
          map={map}
          containerRef={container}
          preset={flow.preset}
          customSize={flow.customSize}
          maxSidePx={exporter.maxSidePx}
          window={flow.window}
          busy={flow.stage === 'capturing'}
          error={exporter.error}
          onWindowChange={exporter.moveWindow}
          onChoiceChange={exporter.changeChoice}
          onCapture={() => void exporter.capture()}
          onCancel={exporter.cancel}
        />
      )}
    </>
  );
}
