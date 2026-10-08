import { useEffect, useRef, useState } from 'react';
import { sendStory, type Story } from '../api';
import { t } from '../i18n';
import { useBackdropClose } from './ConfirmDialog';

/**
 * Send a copy (`SPEC.md` FR-14.7): a Stories tab folder's send button, asking for the email
 * address to send a copy of the Story to. A modal `<dialog>` like StoryDialog.tsx, owning its busy
 * and error state. Once sent it says so and stays open on that, with the same words whether or
 * not the address has an account — the server answers the same either way, so the person
 * sending can't tell — until it's closed.
 */
export function SendCopyDialog({ story, onClose }: { story: Story; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [email, setEmail] = useState('');
  const [busy, setBusy] = useState(false);
  const [sentTo, setSentTo] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const backdrop = useBackdropClose(ref, () => {
    if (!busy) ref.current?.close();
  });

  useEffect(() => {
    const el = ref.current;
    if (el && !el.open) el.showModal();
  }, []);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    const address = email.trim();
    if (address === '' || busy) return;
    setBusy(true);
    setError(null);
    try {
      await sendStory(story.id, address);
      setSentTo(address);
      setEmail('');
    } catch (err) {
      setError(err instanceof Error ? err.message : t('common.something_wrong'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <dialog
      ref={ref}
      className="confirm-dialog story-dialog"
      data-testid="send-copy-dialog"
      aria-labelledby="send-copy-dialog-title"
      onClose={onClose}
      onCancel={(event) => {
        if (busy) event.preventDefault();
      }}
      {...backdrop}
    >
      <form onSubmit={(event) => void handleSubmit(event)}>
        <h2 className="confirm-dialog__title" id="send-copy-dialog-title">
          {t('stories.send_title', { name: story.name })}
        </h2>
        <p className="confirm-dialog__message">{t('stories.send_body')}</p>
        <label className="settings-page__section">
          <span className="settings-page__label">{t('stories.send_email')}</span>
          <input
            className="settings-page__input"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder={t('stories.send_email_placeholder')}
            autoComplete="email"
            required
            autoFocus
            disabled={busy}
          />
        </label>
        {sentTo && (
          <p className="settings-page__success" role="status">
            {t('stories.send_done', { email: sentTo })}
          </p>
        )}
        {error && <p className="settings-page__error">{error}</p>}
        <div className="settings-page__save-row">
          <button type="submit" className="settings-page__submit" disabled={busy || email.trim() === ''}>
            {busy ? t('stories.sending') : t('stories.send_confirm')}
          </button>
          <button type="button" className="settings-page__button" disabled={busy} onClick={() => ref.current?.close()}>
            {sentTo ? t('common.close') : t('common.cancel')}
          </button>
        </div>
      </form>
    </dialog>
  );
}