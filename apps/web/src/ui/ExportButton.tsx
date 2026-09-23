import type { Map as MapLibreMap } from 'maplibre-gl';

export interface ExportButtonProps {
  /** `null` before the live map has finished loading — the button stays disabled until
   *  then, same as every other map-dependent control in this app. */
  map: MapLibreMap | null;
  /** Whether the export frame is currently shown — the button reads as pressed while it is. */
  active: boolean;
  /** Shows the export frame (`ExportFrame.tsx`), or puts it away if it's already shown — this
   *  button never captures directly. Busy/error state belongs to the frame's own Capture
   *  step, since the frame/capture sequence spans components this button doesn't own. */
  onOpen: () => void;
}

/**
 * The header's Export trigger (`VISION.md` §4.2, "print-grade... export"). A header
 * action rather than a floating map-corner control on purpose — every corner is already
 * spoken for, and this is a deliberate one-shot action, not a live map mode/state toggle.
 * `map` is passed in rather than read here, the same "callback wiring belongs where the map
 * instance is" reasoning Header.tsx's own doc comment already gives for why `importControl`
 * is a prop.
 */
export function ExportButton({ map, active, onOpen }: ExportButtonProps) {
  return (
    <button
      type="button"
      className="export-button"
      data-testid="export-button"
      aria-pressed={active}
      onClick={onOpen}
      disabled={!map}
    >
      Export
    </button>
  );
}
