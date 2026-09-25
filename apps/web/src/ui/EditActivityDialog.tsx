import { useEffect, useRef, useState } from 'react';
import { updateActivity, type Activity } from '../api';
import type { TypeFacet } from './activityFacets';
import { ActivityTypePicker } from './ActivityTypePicker';
import { formatStartedAt } from './format';

/** Mirrors the backend's own bounds (activities.go's maxActivityTypeLen/maxActivityNameLen/
 *  maxActivityDescriptionLen) — enforced here too so a caller sees the limit before
 *  submitting, not only after a 400 comes back. */
const MAX_ACTIVITY_TYPE_LEN = 50;
const MAX_NAME_LEN = 200;
const MAX_DESCRIPTION_LEN = 2000;

/**
 * The "rename an activity, name it, add a note" form (§4.7.4) — reached from the header
 * toolbar's Edit-selected button (ActivitiesPanel.tsx) over whatever's currently checked. A
 * real `<dialog>`/`showModal()`, the right primitive for "a small focused piece of UI over
 * the map": free Escape/backdrop/focus-trap behavior, and
 * exactly one path out (the dialog's own `close()`) regardless of whether that came from Save,
 * Cancel, Escape, or a backdrop click.
 *
 * `activities` is always at least one, and the form branches on its length: exactly one edits
 * Type, Name, and Description together, same as ever; more than one edits Type only — Name and
 * Description have nothing to set consistently across several different activities at once, so
 * they render disabled with a tooltip explaining why rather than silently doing nothing or
 * (worse) overwriting every checked activity's name/description with one shared value.
 *
 * Type is ActivityTypePicker.tsx — the same searchable picker as Settings' Country and
 * Timezone, listing this account's existing types, but open-ended: §4.7.2 already resolved
 * activity_type as free-form, not a controlled vocabulary, and this is not the place to
 * reintroduce one. Picking an existing type is a convenience (retyping "Ride" is a pick, not
 * a retype), never a constraint: typing something nobody has used before ("Solowheel",
 * "Roadtrip") offers an "Add" row that saves it exactly as typed, to every checked activity.
 *
 * Name is optional, unlike Type — an activity with none simply falls back to its start
 * datetime as the row's primary line (ActivitiesPanel.tsx). It's never parsed from a source
 * file (§4.7's revised decision is deliberately narrower than that); the only way one exists
 * is a person typing it in here, one activity at a time.
 */
export interface EditActivityDialogProps {
  /** At least one. A row click passes exactly one; the toolbar's Edit-selected button passes
   *  the whole checked group, whatever its size. */
  activities: Activity[];
  /** Every distinct activity_type already in this account's loaded activities, with counts —
   *  the same facets the header toolbar's Type dropdown is built from (activityFacets.ts),
   *  reused here as the Type picker's list rather than recomputed. */
  knownTypes: TypeFacet[];
  onClose: () => void;
  /** Called once the save actually lands, so the caller can refresh its own activity list —
   *  see ActivitiesPanel.tsx for why that's a plain reload() rather than local patching. */
  onSaved: () => void;
}

export function EditActivityDialog({ activities, knownTypes, onClose, onSaved }: EditActivityDialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const single = activities.length === 1 ? activities[0] : null;
  // Seeded from the first checked activity when several are being edited at once — there's no
  // single "current" type across a mixed group, and the field is there to set one value going
  // forward for all of them, not to summarize what they currently are.
  const [activityType, setActivityType] = useState(activities[0]!.activityType);
  const [name, setName] = useState(single?.name ?? '');
  const [description, setDescription] = useState(single?.description ?? '');
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
    const trimmedName = name.trim();
    if (trimmedName.length > MAX_NAME_LEN) {
      setError(`Name must be ${MAX_NAME_LEN} characters or fewer.`);
      return;
    }
    if (description.length > MAX_DESCRIPTION_LEN) {
      setError(`Description must be ${MAX_DESCRIPTION_LEN} characters or fewer.`);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      if (single) {
        await updateActivity(single.id, { activityType: trimmedType, name: trimmedName, description });
      } else {
        // The PATCH endpoint is a full replace (activities.go), not a per-field patch — so
        // changing only Type across a group still has to resend each activity's own current
        // name/description unchanged, or the backend would clear them. Sequential, not
        // Promise.all, matching the same reasoning ActivitiesPanel.tsx's own "Delete group"
        // already uses: a hand-checked group is a handful of rows, not bulk-import scale.
        for (const activity of activities) {
          await updateActivity(activity.id, {
            activityType: trimmedType,
            name: activity.name ?? '',
            description: activity.description ?? '',
          });
        }
      }
      onSaved();
      ref.current?.close();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'could not save activity');
    } finally {
      setSaving(false);
    }
  }

  const namedFieldsDisabledReason = single
    ? undefined
    : 'Editing multiple activities only changes their type — name and description are per-activity.';

  return (
    <dialog
      ref={ref}
      className="edit-activity-dialog"
      data-testid="edit-activity-dialog"
      onClose={onClose}
      // A backdrop click has the dialog itself as its target; a click inside it doesn't.
      onClick={(event) => {
        if (event.target === ref.current) ref.current?.close();
      }}
    >
      <h2 className="edit-activity-dialog__title">{single ? 'Edit activity' : `Edit ${activities.length} activities`}</h2>
      <p className="edit-activity-dialog__subtitle">
        {single ? formatStartedAt(single.startedAt) : 'Only Type applies to every checked activity — see below.'}
      </p>

      {/* A <div>, not a <label> like the fields below: a label wrapping the picker would
          forward every click inside its popover (search box included) to the trigger. */}
      <div className="settings-page__section">
        <span className="settings-page__label" id="edit-activity-type-label">
          Type
        </span>
        <ActivityTypePicker
          value={activityType}
          onChange={setActivityType}
          known={knownTypes}
          labelledBy="edit-activity-type-label"
          maxLength={MAX_ACTIVITY_TYPE_LEN}
        />
      </div>

      <label className="settings-page__section" title={namedFieldsDisabledReason}>
        <span className="settings-page__label">Name</span>
        <input
          className="settings-page__input"
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={MAX_NAME_LEN}
          placeholder="Optional — shown instead of the date in the Activities list"
          disabled={!single}
        />
      </label>

      <label className="settings-page__section" title={namedFieldsDisabledReason}>
        <span className="settings-page__label">Description</span>
        <textarea
          className="settings-page__input edit-activity-dialog__description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          maxLength={MAX_DESCRIPTION_LEN}
          placeholder='Add a note — e.g. "Roadtrip to California with kids"'
          rows={4}
          disabled={!single}
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
