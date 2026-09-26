import { useCallback, useEffect, useRef, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { saveActivityTrackEdit, updateActivity, type Activity, type TrackEdit } from '../api';
import type { TypeFacet } from './activityFacets';
import { ActivityTypePicker } from './ActivityTypePicker';
import { formatStartedAt } from './format';
import { TrackEditor } from './TrackEditor';
import { t, tn } from '../i18n';

/** Mirrors the backend's own bounds (activities.go's maxActivityTypeLen/maxActivityNameLen/
 *  maxActivityDescriptionLen) — enforced here too so a caller sees the limit before
 *  submitting, not only after a 400 comes back. */
const MAX_ACTIVITY_TYPE_LEN = 50;
const MAX_NAME_LEN = 200;
const MAX_DESCRIPTION_LEN = 2000;

type Tab = 'activity' | 'track';

/**
 * The Edit window (§4.7.4, §4.7.7) — reached from the header toolbar's Edit button
 * (ActivitiesPanel.tsx) over whatever's currently checked, floating over the map. Two tabs
 * behind one shared Save and Cancel:
 *
 *  - **Activity** — Type, Name, and Description. `activities` is always at least one, and the
 *    form branches on its length: exactly one edits all three; more than one edits Type only —
 *    Name and Description have nothing to set consistently across several different activities
 *    at once, so they render disabled with a tooltip explaining why rather than silently doing
 *    nothing or (worse) overwriting every checked activity's name/description with one value.
 *  - **Track** — TrackEditor.tsx, over exactly one activity with a finished track;
 *    `trackUnavailable` names why not otherwise, as the disabled tab's tooltip. Opening it the
 *    first time starts MapView's track session (`onStartTrack`: the other tracks hidden, the
 *    camera on this one), which then lasts until the window closes; the editor stays mounted
 *    behind the Activity tab, so switching back and forth loses nothing.
 *
 * Save writes what changed — the fields first (skipped when they're as they were), then the
 * track edit (skipped when the Track tab did nothing) — and closes. If the track edit fails
 * after the fields saved, the window stays open with the error, and `onClose` still reports
 * the save so the list picks it up. Cancel (or Escape) discards both.
 *
 * Floating, not a modal `<dialog>` as the Activity form alone once was: the Track tab edits on
 * the map, and a modal would make the map inert. MapView makes the Activities panel and the
 * timeline inert instead while the window is open — the window edits the checked group, which
 * must not change underneath it.
 *
 * Type is ActivityTypePicker.tsx — the same searchable picker as Settings' Country and
 * Timezone, listing this account's existing types, but open-ended: §4.7.2 already resolved
 * activity_type as free-form, not a controlled vocabulary. Picking an existing type is a
 * convenience, never a constraint: typing something nobody has used before offers an "Add" row
 * that saves it exactly as typed, to every checked activity. Name is optional — an activity
 * with none falls back to its start datetime as the row's primary line; it's never parsed from
 * a source file, only typed in here, one activity at a time.
 */
export interface EditActivityWindowProps {
  map: MapLibreMap;
  /** At least one — the checked group, whatever its size. */
  activities: Activity[];
  /** Every distinct activity_type already in this account's loaded activities, with counts —
   *  the same facets the Type filter is built from (activityFacets.ts), reused as the Type
   *  picker's list rather than recomputed. */
  knownTypes: TypeFacet[];
  /** Why the Track tab is disabled, or null when `activities` is one editable track. */
  trackUnavailable: string | null;
  onStartTrack: (activity: Activity) => void;
  /** `saved`: something was written, so the list reloads. `trackApplied`: a track edit was
   *  sent, so the row now reads Pending until its reprocess lands. */
  onClose: (result: { saved: boolean; trackApplied: boolean }) => void;
}

export function EditActivityWindow({ map, activities, knownTypes, trackUnavailable, onStartTrack, onClose }: EditActivityWindowProps) {
  const single = activities.length === 1 ? activities[0]! : null;
  const [tab, setTab] = useState<Tab>('activity');
  const [trackStarted, setTrackStarted] = useState(false);
  // Seeded from the first checked activity when several are being edited at once — there's no
  // single "current" type across a mixed group, and the field is there to set one value going
  // forward for all of them, not to summarize what they currently are.
  const [activityType, setActivityType] = useState(activities[0]!.activityType);
  const [name, setName] = useState(single?.name ?? '');
  const [description, setDescription] = useState(single?.description ?? '');
  const [trackPending, setTrackPending] = useState<{ edit: TrackEdit | null } | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Fields already written by an earlier Save whose track edit then failed — a later Cancel
  // still has to report them, and a retry needn't write them again.
  const fieldsSaved = useRef(false);

  const onTrackChange = useCallback((pending: { edit: TrackEdit | null } | null) => {
    setTrackPending(pending);
    setError(null);
  }, []);

  const cancel = useCallback(() => {
    onClose({ saved: fieldsSaved.current, trackApplied: false });
  }, [onClose]);

  useEffect(() => {
    if (saving) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented) cancel();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [saving, cancel]);

  function openTab(next: Tab) {
    setTab(next);
    if (next === 'track' && !trackStarted && single) {
      setTrackStarted(true);
      onStartTrack(single);
    }
  }

  async function save() {
    const trimmedType = activityType.trim();
    const trimmedName = name.trim();
    const invalid = !trimmedType
      ? t('edit.type_required')
      : trimmedType.length > MAX_ACTIVITY_TYPE_LEN
        ? t('edit.type_too_long', { max: MAX_ACTIVITY_TYPE_LEN })
        : trimmedName.length > MAX_NAME_LEN
          ? t('edit.name_too_long', { max: MAX_NAME_LEN })
          : description.length > MAX_DESCRIPTION_LEN
            ? t('edit.description_too_long', { max: MAX_DESCRIPTION_LEN })
            : null;
    if (invalid) {
      setError(invalid);
      setTab('activity');
      return;
    }
    const fieldsChanged = single
      ? trimmedType !== single.activityType || trimmedName !== (single.name ?? '') || description !== (single.description ?? '')
      : activities.some((a) => a.activityType !== trimmedType);

    setSaving(true);
    setError(null);
    try {
      if (fieldsChanged && !fieldsSaved.current) {
        if (single) {
          await updateActivity(single.id, { activityType: trimmedType, name: trimmedName, description });
        } else {
          // The PATCH endpoint is a full replace (activities.go), not a per-field patch — so
          // changing only Type across a group still has to resend each activity's own current
          // name/description unchanged, or the backend would clear them. Sequential, not
          // Promise.all, matching ActivitiesPanel.tsx's own group delete: a hand-checked group
          // is a handful of rows, not bulk-import scale.
          for (const activity of activities) {
            await updateActivity(activity.id, {
              activityType: trimmedType,
              name: activity.name ?? '',
              description: activity.description ?? '',
            });
          }
        }
        fieldsSaved.current = true;
      }
      if (single && trackPending) {
        await saveActivityTrackEdit(single.id, trackPending.edit);
        onClose({ saved: true, trackApplied: true });
        return;
      }
      onClose({ saved: fieldsSaved.current, trackApplied: false });
    } catch (err) {
      setError(err instanceof Error ? err.message : t('edit.save_failed'));
      setSaving(false);
    }
  }

  const namedFieldsDisabledReason = single ? undefined : t('edit.multi_reason');

  return (
    <section className="edit-track edit-window" aria-label={t('edit.title_one')} data-testid="edit-activity-window">
      <header className="edit-track__head">
        <span className="edit-track__title">{single ? t('edit.title_one') : tn('edit.title_many', activities.length)}</span>
        <span className="edit-track__subtitle">
          {single ? single.name?.trim() || formatStartedAt(single.startedAt) : t('edit.multi_subtitle')}
        </span>
      </header>

      <div className="edit-window__tabs" role="tablist">
        <button
          type="button"
          role="tab"
          className="edit-window__tab"
          aria-selected={tab === 'activity'}
          onClick={() => openTab('activity')}
          data-testid="edit-tab-activity"
        >
          {t('edit.tab_activity')}
        </button>
        <button
          type="button"
          role="tab"
          className="edit-window__tab"
          aria-selected={tab === 'track'}
          disabled={trackUnavailable !== null}
          title={trackUnavailable ?? undefined}
          onClick={() => openTab('track')}
          data-testid="edit-tab-track"
        >
          {t('edit.tab_track')}
          {trackPending && <span className="edit-window__tab-dot" aria-hidden="true" />}
        </button>
      </div>

      <div className="edit-window__panel" role="tabpanel" hidden={tab !== 'activity'}>
        {/* A <div>, not a <label> like the fields below: a label wrapping the picker would
            forward every click inside its popover (search box included) to the trigger. */}
        <div className="settings-page__section">
          <span className="settings-page__label" id="edit-activity-type-label">
            {t('activities.type')}
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
          <span className="settings-page__label">{t('edit.name')}</span>
          <input
            className="settings-page__input"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={MAX_NAME_LEN}
            placeholder={t('edit.name_placeholder')}
            disabled={!single}
          />
        </label>

        <label className="settings-page__section" title={namedFieldsDisabledReason}>
          <span className="settings-page__label">{t('edit.description')}</span>
          <textarea
            className="settings-page__input edit-window__description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            maxLength={MAX_DESCRIPTION_LEN}
            placeholder={t('edit.description_placeholder')}
            rows={4}
            disabled={!single}
          />
        </label>
      </div>

      {trackStarted && single && (
        <div className="edit-window__panel" role="tabpanel" hidden={tab !== 'track'}>
          <TrackEditor map={map} activity={single} active={tab === 'track'} busy={saving} onChange={onTrackChange} />
        </div>
      )}

      {error && <p className="edit-track__error">{error}</p>}
      <div className="edit-track__row edit-track__row--footer">
        <span className="edit-track__spacer" aria-hidden="true" />
        <button type="button" className="edit-track__btn" disabled={saving} onClick={cancel}>
          {t('common.cancel')}
        </button>
        <button
          type="button"
          className="edit-track__btn edit-track__btn--primary"
          disabled={saving}
          onClick={() => void save()}
          data-testid="edit-save"
        >
          {saving ? t('common.saving') : t('common.save')}
        </button>
      </div>
    </section>
  );
}
