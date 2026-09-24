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
 *
 * A camera icon rather than the word "Export": what it makes is a picture of the map, and the
 * icon says so where "Export" read as a data export. The accessible name and the hover
 * tooltip carry the words.
 */
export function ExportButton({ map, active, onOpen }: ExportButtonProps) {
  return (
    <button
      type="button"
      className="export-button"
      data-testid="export-button"
      aria-pressed={active}
      aria-label="Export map image"
      title="Export map image"
      onClick={onOpen}
      disabled={!map}
    >
      <svg className="export-button__icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
        <path d="M3.5 8.5a2 2 0 0 1 2-2h2.3l1.4-2.2a1 1 0 0 1 .85-.46h3.9a1 1 0 0 1 .85.46l1.4 2.2h2.3a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z" />
        <circle cx="12" cy="13" r="3.6" />
      </svg>
    </button>
  );
}
