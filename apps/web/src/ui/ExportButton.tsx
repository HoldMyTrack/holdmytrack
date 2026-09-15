import { useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { exportMapImage, type ExportViewState } from '../map/exportMap';

export interface ExportButtonProps {
  /** `null` before the live map has finished loading — the button stays disabled until
   *  then, same as every other map-dependent control in this app. */
  map: MapLibreMap | null;
  viewState: ExportViewState;
}

/**
 * The header's Export trigger (`VISION.md` §4.2, "print-grade... export"). A header
 * action rather than a floating map-corner control on purpose — every corner is already
 * spoken for, and this is a deliberate one-shot action, not a live map mode/state toggle.
 * `map`/`viewState` are passed in rather than read here, the same "callback wiring belongs
 * where the map instance is" reasoning Header.tsx's own doc comment already gives for why
 * `uploadControl` is a prop.
 */
export function ExportButton({ map, viewState }: ExportButtonProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleClick = async () => {
    if (!map || busy) return;
    setBusy(true);
    setError(null);
    try {
      const blob = await exportMapImage(map, viewState);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `fitmap-${new Date().toISOString().slice(0, 10)}.png`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="export-button-wrap">
      <button
        type="button"
        className="export-button"
        data-testid="export-button"
        onClick={handleClick}
        disabled={!map || busy}
      >
        {busy ? 'Exporting…' : 'Export'}
      </button>
      {error && (
        <p className="export-button__error" role="alert" data-testid="export-button-error">
          {error}
        </p>
      )}
    </div>
  );
}
