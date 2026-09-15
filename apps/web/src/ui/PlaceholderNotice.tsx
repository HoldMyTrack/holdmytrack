import { useEffect, useRef } from 'react';

/**
 * The "this isn't built yet" popup, shared by everything in the header nav bar that is a
 * placeholder — Donate and every item in the user menu (Header.tsx).
 *
 * One component rather than one per control, because the honest message in each case has the
 * same shape: here is what this will do, here is why it does nothing today. Saying so out
 * loud is the point — an inert control the user can click and get no response from reads as
 * a bug, and a disabled one doesn't explain itself either.
 *
 * A real `<dialog>` with `showModal()`, not a hand-rolled overlay: Escape-to-dismiss, focus
 * trapping, inertness of the page behind it and the `::backdrop` pseudo-element all come
 * with it, and none of those are worth reimplementing for a message box. Dismissal always
 * goes through the element's own `close()` so there is exactly one path out — the `close`
 * event — whether it came from Escape, the backdrop or the button.
 */
export interface NoticeContent {
  title: string;
  message: string;
}

export interface PlaceholderNoticeProps {
  notice: NoticeContent;
  onDismiss: () => void;
}

export function PlaceholderNotice({ notice, onDismiss }: PlaceholderNoticeProps) {
  const ref = useRef<HTMLDialogElement>(null);

  // showModal() rather than the `open` attribute — the attribute renders a non-modal dialog,
  // which is the one variant that gets none of the behaviour this is here for.
  useEffect(() => {
    const el = ref.current;
    if (el && !el.open) el.showModal();
  }, []);

  return (
    <dialog
      ref={ref}
      className="placeholder-notice"
      data-testid="placeholder-notice"
      onClose={onDismiss}
      // A click on the backdrop has the dialog itself as its target — the children are what
      // register as targets for clicks on the box.
      onClick={(event) => {
        if (event.target === ref.current) ref.current?.close();
      }}
    >
      <h2 className="placeholder-notice__title">{notice.title}</h2>
      <p className="placeholder-notice__message">{notice.message}</p>
      <button type="button" className="placeholder-notice__dismiss" onClick={() => ref.current?.close()}>
        Close
      </button>
    </dialog>
  );
}
