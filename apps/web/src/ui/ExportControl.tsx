import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import type { IControl, Map as MapLibreMap } from 'maplibre-gl';
import { Camera } from 'lucide-react';

export interface ExportControlProps {
  map: MapLibreMap;
  /** Whether the export frame is currently shown — the button reads as pressed while it is. */
  active: boolean;
  /** Shows the export frame (`ExportFrame.tsx`), or puts it away if it's already shown — this
   *  button never captures directly. Busy/error state belongs to the frame's own Capture
   *  step, since the frame/capture sequence spans components this button doesn't own. */
  onOpen: () => void;
}

/**
 * The Export trigger (`VISION.md` §4.2, "print-grade... export") — a map control in the
 * top-right stack, under zoom and locate. It was a header action until the header became the
 * server-rendered one every page shares (ADR-0012), where a map-only control has no place.
 *
 * A real MapLibre control, so it sits in MapLibre's own stack with its own look, rather than a
 * floating button positioned to line up with it: the control's container is MapLibre's
 * (`onAdd`), and React renders the button into it through a portal, so its pressed state stays
 * ordinary React state.
 *
 * A camera icon rather than the word "Export": what it makes is a picture of the map, and the
 * icon says so where "Export" read as a data export. The accessible name and the hover
 * tooltip carry the words.
 */
export function ExportControl({ map, active, onOpen }: ExportControlProps) {
  const [container, setContainer] = useState<HTMLElement | null>(null);

  useEffect(() => {
    const element = document.createElement('div');
    element.className = 'maplibregl-ctrl maplibregl-ctrl-group';
    const control: IControl = {
      onAdd: () => element,
      onRemove: () => element.remove(),
    };
    map.addControl(control, 'top-right');
    setContainer(element);
    return () => {
      map.removeControl(control);
      setContainer(null);
    };
  }, [map]);

  if (!container) return null;
  return createPortal(
    <button
      type="button"
      className="export-control"
      data-testid="export-button"
      aria-pressed={active}
      aria-label="Export map image"
      title="Export map image"
      onClick={onOpen}
    >
      <Camera size={17} aria-hidden="true" />
    </button>,
    container,
  );
}
