import { useEffect, useRef } from 'react';
import type { Story } from '../api';
import { t, tn } from '../i18n';

/** How much of the Edit window's target a Story holds: all of it, some of it, or none. */
export type StoryMembership = 'all' | 'some' | 'none';

export function membershipOf(story: Story, activityIds: readonly string[]): StoryMembership {
  const held = activityIds.filter((id) => story.activityIds.includes(id)).length;
  return held === 0 ? 'none' : held === activityIds.length ? 'all' : 'some';
}

/**
 * The Edit window's Stories tab (`SPEC.md` FR-5.17): the account's Stories as checkboxes for the
 * window's target, one activity or a group. A box is ticked when its Story holds every one of
 * them, indeterminate when it holds some — the one state a click can't choose, only leave — and
 * empty when it holds none. Nothing is written here: `changes` holds what the user picked, by
 * Story id, and the window's shared Save commits it.
 */
export interface EditStoriesTabProps {
  /** null while loading. */
  stories: Story[] | null;
  error: string | null;
  activityIds: readonly string[];
  /** The ticked (`all`) or cleared (`none`) state picked for a Story, where it differs from
   *  what the Story holds now. */
  changes: ReadonlyMap<string, 'all' | 'none'>;
  onChange: (storyId: string, next: 'all' | 'none' | null) => void;
  busy: boolean;
}

export function EditStoriesTab({ stories, error, activityIds, changes, onChange, busy }: EditStoriesTabProps) {
  if (error) return <p className="edit-track__error">{error}</p>;
  if (stories === null) return <p className="edit-track__note">{t('common.loading')}</p>;
  if (stories.length === 0) return <p className="edit-track__note">{t('edit.stories_empty')}</p>;
  return (
    <>
      <p className="edit-track__note">{tn('edit.stories_hint', activityIds.length)}</p>
      <ul className="edit-stories">
        {stories.map((story) => {
          const initial = membershipOf(story, activityIds);
          const shown = changes.get(story.id) ?? initial;
          return (
            <li key={story.id}>
              <label className="edit-stories__row" title={shown === 'some' ? t('edit.stories_some') : undefined}>
                <StoryCheckbox
                  state={shown}
                  disabled={busy}
                  onToggle={() => {
                    const next = shown === 'all' ? 'none' : 'all';
                    onChange(story.id, next === initial ? null : next);
                  }}
                />
                <span className="edit-stories__name">{story.name}</span>
                <span className="edit-stories__count">{tn('activities.count', story.activityIds.length)}</span>
              </label>
            </li>
          );
        })}
      </ul>
    </>
  );
}

/** A checkbox that can show `some` — `indeterminate` is a DOM property, not an attribute. */
function StoryCheckbox({ state, disabled, onToggle }: { state: StoryMembership; disabled: boolean; onToggle: () => void }) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = state === 'some';
  }, [state]);
  return (
    <input
      ref={ref}
      type="checkbox"
      className="activities-panel__checkbox"
      checked={state === 'all'}
      disabled={disabled}
      onChange={onToggle}
    />
  );
}
