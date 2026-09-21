import { useEffect, useRef } from 'react';
import { EXPORT_PLATFORMS, EXPORT_PRESETS, EXPORT_PRESET_ROWS, type ExportPreset } from '../map/exportPresets';

export interface ExportPresetDialogProps {
  onPick: (preset: ExportPreset | 'custom') => void;
  onClose: () => void;
}

/**
 * The first step of the frame-and-capture export flow (`ExportButton.tsx` opens this instead
 * of exporting directly; `ExportFrame.tsx` renders once a shape is picked). Same real
 * `<dialog>`/`showModal()` shape as `ConfirmDialog.tsx` — free Escape/backdrop/focus-trap
 * behavior, one path out. Unlike `ConfirmDialog`, picking a cell *is* the action — there's
 * nothing to confirm afterward, so the dialog closes itself immediately on click rather than
 * waiting on an async `onConfirm`.
 *
 * Platform columns × resolution rows (`exportPresets.ts`'s own grid coordinates), the same
 * comparison-table shape Hootsuite's own guide uses — not every cell is filled (X has no
 * Story), which a sparse grid shows more directly than several same-looking button lists
 * would.
 */
export function ExportPresetDialog({ onPick, onClose }: ExportPresetDialogProps) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (el && !el.open) el.showModal();
  }, []);

  function pick(preset: ExportPreset | 'custom') {
    onPick(preset);
    ref.current?.close();
  }

  return (
    <dialog
      ref={ref}
      className="export-preset-dialog"
      data-testid="export-preset-dialog"
      onClose={onClose}
      onClick={(event) => {
        if (event.target === ref.current) ref.current?.close();
      }}
    >
      <h2 className="export-preset-dialog__title">Export shape</h2>
      <div className="export-preset-dialog__grid-wrap">
        <table className="export-preset-dialog__grid">
          <thead>
            <tr>
              <th scope="col" />
              {EXPORT_PLATFORMS.map((platform) => (
                <th key={platform} scope="col">
                  {platform}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {EXPORT_PRESET_ROWS.map((row) => (
              <tr key={row}>
                <th scope="row">{row}</th>
                {EXPORT_PLATFORMS.map((platform) => {
                  const preset = EXPORT_PRESETS.find((p) => p.group === platform && p.row === row);
                  return (
                    <td key={platform}>
                      {preset && (
                        <button
                          type="button"
                          className="export-preset-dialog__cell"
                          data-testid={`export-preset-${preset.id}`}
                          aria-label={`${platform} ${row}, ${preset.widthPx} by ${preset.heightPx}`}
                          onClick={() => pick(preset)}
                        >
                          {preset.widthPx}×{preset.heightPx}
                        </button>
                      )}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <button
        type="button"
        className="export-preset-dialog__custom"
        data-testid="export-preset-custom"
        onClick={() => pick('custom')}
      >
        Custom
      </button>
    </dialog>
  );
}
