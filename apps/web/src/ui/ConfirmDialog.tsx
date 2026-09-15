import { useEffect, useRef, useState } from 'react';

/**
 * A generic "are you sure" confirm/cancel dialog — first used by the Activities panel's
 * Delete button (§4.7.5), but deliberately not delete-specific: any future destructive action
 * that needs a confirm step before committing can reuse this rather than growing its own.
 *
 * Same real `<dialog>`/`showModal()` shape as `PlaceholderNotice.tsx`/`EditActivityDialog.tsx`
 * — free Escape/backdrop/focus-trap behavior, and exactly one path out (the dialog's own
 * `close()`) regardless of whether that came from Confirm, Cancel, Escape, or a backdrop
 * click. `onConfirm` is async and owned entirely here, the same self-contained shape
 * `EditActivityDialog.tsx`'s own Save button already uses, rather than pushing busy/error
 * state onto whichever caller happens to render this — the dialog closes itself once
 * `onConfirm` actually resolves, not before.
 */
export interface ConfirmDialogProps {
  title: string;
  message: string;
  /** Defaults to "Confirm"/"Cancel" — callers name the action explicitly ("Delete") when a
   *  generic label would read oddly for what's being confirmed. */
  confirmLabel?: string;
  cancelLabel?: string;
  busyLabel?: string;
  onConfirm: () => Promise<void>;
  onClose: () => void;
}

export function ConfirmDialog({
  title,
  message,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  busyLabel = 'Working…',
  onConfirm,
  onClose,
}: ConfirmDialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (el && !el.open) el.showModal();
  }, []);

  async function handleConfirm() {
    setBusy(true);
    setError(null);
    try {
      await onConfirm();
      ref.current?.close();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'something went wrong');
    } finally {
      setBusy(false);
    }
  }

  return (
    <dialog
      ref={ref}
      className="confirm-dialog"
      data-testid="confirm-dialog"
      onClose={onClose}
      onClick={(event) => {
        if (event.target === ref.current) ref.current?.close();
      }}
    >
      <h2 className="confirm-dialog__title">{title}</h2>
      <p className="confirm-dialog__message">{message}</p>
      {error && <p className="settings-page__error">{error}</p>}
      <div className="settings-page__save-row">
        <button type="button" className="confirm-dialog__confirm" disabled={busy} onClick={() => void handleConfirm()}>
          {busy ? busyLabel : confirmLabel}
        </button>
        <button type="button" className="settings-page__button" disabled={busy} onClick={() => ref.current?.close()}>
          {cancelLabel}
        </button>
      </div>
    </dialog>
  );
}
