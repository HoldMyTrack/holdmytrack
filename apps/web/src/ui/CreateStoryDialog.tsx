import { useEffect, useRef, useState } from 'react';
import { createStory, STORY_MAX_DESCRIPTION_LEN, STORY_MAX_NAME_LEN, type Story } from '../api';
import { t } from '../i18n';

/**
 * "Create story" (`SPEC.md` FR-5.16) — the Activities panel toolbar's dialog for making a Story
 * of the checked activities: a Name, required, and an optional Description, then one
 * `POST /v1/stories` carrying the activities too. A modal `<dialog>` like ConfirmDialog.tsx, and
 * like it owns its busy and error state and closes itself once the request has succeeded;
 * `onCreated` gets the new Story before `onClose` runs.
 *
 * A `<form>`, so Enter in the Name field creates; the Description is a textarea, where Enter is
 * a new line.
 */
export interface CreateStoryDialogProps {
  activityIds: string[];
  /** What's being made into a Story — "3 checked activities (58 km)". */
  summary: string;
  onCreated: (story: Story) => void;
  onClose: () => void;
}

export function CreateStoryDialog({ activityIds, summary, onCreated, onClose }: CreateStoryDialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (el && !el.open) el.showModal();
  }, []);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    if (name.trim() === '' || busy) return;
    setBusy(true);
    setError(null);
    try {
      const story = await createStory({ name: name.trim(), description: description.trim(), activityIds });
      onCreated(story);
      ref.current?.close();
    } catch (err) {
      setError(err instanceof Error ? err.message : t('common.something_wrong'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <dialog
      ref={ref}
      className="confirm-dialog create-story"
      data-testid="create-story-dialog"
      aria-labelledby="create-story-title"
      onClose={onClose}
      onClick={(event) => {
        if (event.target === ref.current && !busy) ref.current?.close();
      }}
    >
      <form onSubmit={(event) => void handleSubmit(event)}>
        <h2 className="confirm-dialog__title" id="create-story-title">
          {t('stories.create_title')}
        </h2>
        <p className="confirm-dialog__message">{t('stories.create_body', { group: summary })}</p>
        <label className="settings-page__section">
          <span className="settings-page__label">{t('stories.name')}</span>
          <input
            className="settings-page__input"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={STORY_MAX_NAME_LEN}
            placeholder={t('stories.name_placeholder')}
            required
            autoFocus
            disabled={busy}
          />
        </label>
        <label className="settings-page__section">
          <span className="settings-page__label">{t('stories.description')}</span>
          <textarea
            className="settings-page__input edit-window__description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            maxLength={STORY_MAX_DESCRIPTION_LEN}
            placeholder={t('stories.description_placeholder')}
            rows={3}
            disabled={busy}
          />
        </label>
        {error && <p className="settings-page__error">{error}</p>}
        <div className="settings-page__save-row">
          <button type="submit" className="settings-page__submit" disabled={busy || name.trim() === ''}>
            {busy ? t('stories.creating') : t('stories.create_confirm')}
          </button>
          <button type="button" className="settings-page__button" disabled={busy} onClick={() => ref.current?.close()}>
            {t('common.cancel')}
          </button>
        </div>
      </form>
    </dialog>
  );
}
