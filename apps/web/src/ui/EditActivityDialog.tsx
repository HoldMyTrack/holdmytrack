import { useEffect, useRef, useState } from 'react';
import { updateActivity, type Activity } from '../api';
import { formatStartedAt } from './format';

/** Mirrors the backend's own bounds (activities.go's maxActivityTypeLen/
 *  maxActivityDescriptionLen) — enforced here too so a caller sees the limit before
 *  submitting, not only after a 400 comes back. */
const MAX_ACTIVITY_TYPE_LEN = 50;
const MAX_DESCRIPTION_LEN = 2000;

/**
 * The "rename an activity, add a note" form (§4.7.4) — reached from a row's pencil icon
 * (ActivitiesPanel.tsx). A real `<dialog>`/`showModal()`, the same choice PlaceholderNotice.tsx
 * already made for "a small focused piece of UI over the map": free Escape/backdrop/focus-trap
 * behavior, and exactly one path out (the dialog's own `close()`) regardless of whether that
 * came from Save, Cancel, Escape, or a backdrop click.
 *
 * Type is a plain text field, not a select — §4.7.2 already resolved activity_type as
 * free-form, not a controlled vocabulary, and this is not the place to reintroduce one. The
 * `<datalist>` of `knownTypes` is a convenience (retyping "Ride" is a pick, not a retype),
 * never a constraint: anything typed here, including something nobody has used before
 * ("Solowheel", "Roadtrip"), saves exactly as typed.
 */
export interface EditActivityDialogProps {
  activity: Activity;
  /** Every distinct activity_type already in this account's loaded activities — the same set
   *  the header toolbar's Type dropdown is built from (activityFacets.ts), reused here as
   *  `<datalist>` suggestions rather than recomputed. */
  knownTypes: string[];
  onClose: () => void;
  /** Called once the save actually lands, so the caller can refresh its own activity list —
   *  see ActivitiesPanel.tsx for why that's a plain reload() rather than local patching. */
  onSaved: () => void;
}

export function EditActivityDialog({ activity, knownTypes, onClose, onSaved }: EditActivityDialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const [activityType, setActivityType] = useState(activity.activityType);
  const [description, setDescription] = useState(activity.description ?? '');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (el && !el.open) el.showModal();
  }, []);

  async function handleSave() {
    const trimmedType = activityType.trim();
    if (!trimmedType) {
      setError('Type is required.');
      return;
    }
    if (trimmedType.length > MAX_ACTIVITY_TYPE_LEN) {
      setError(`Type must be ${MAX_ACTIVITY_TYPE_LEN} characters or fewer.`);
      return;
    }
    if (description.length > MAX_DESCRIPTION_LEN) {
      setError(`Description must be ${MAX_DESCRIPTION_LEN} characters or fewer.`);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await updateActivity(activity.id, { activityType: trimmedType, description });
      onSaved();
      ref.current?.close();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not save activity');
    } finally {
      setSaving(false);
    }
  }

  return (
    <dialog
      ref={ref}
      className="edit-activity-dialog"
      data-testid="edit-activity-dialog"
      onClose={onClose}
      // Same "backdrop click has the dialog itself as its target" trick PlaceholderNotice uses.
      onClick={(event) => {
        if (event.target === ref.current) ref.current?.close();
      }}
    >
      <h2 className="edit-activity-dialog__title">Edit activity</h2>
      <p className="edit-activity-dialog__subtitle">{formatStartedAt(activity.startedAt)}</p>

      <label className="settings-page__section">
        <span className="settings-page__label">Type</span>
        <input
          className="settings-page__input"
          type="text"
          list="edit-activity-type-suggestions"
          value={activityType}
          onChange={(e) => setActivityType(e.target.value)}
          maxLength={MAX_ACTIVITY_TYPE_LEN}
          autoFocus
        />
        <datalist id="edit-activity-type-suggestions">
          {knownTypes.map((t) => (
            <option key={t} value={t} />
          ))}
        </datalist>
      </label>

      <label className="settings-page__section">
        <span className="settings-page__label">Description</span>
        <textarea
          className="settings-page__input edit-activity-dialog__description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          maxLength={MAX_DESCRIPTION_LEN}
          placeholder='Add a note — e.g. "Roadtrip to California with kids"'
          rows={4}
        />
      </label>

      {error && <p className="settings-page__error">{error}</p>}
      <div className="settings-page__save-row">
        <button type="button" className="settings-page__submit" disabled={saving} onClick={() => void handleSave()}>
          {saving ? 'Saving…' : 'Save'}
        </button>
        <button type="button" className="settings-page__button" disabled={saving} onClick={() => ref.current?.close()}>
          Cancel
        </button>
      </div>
    </dialog>
  );
}
