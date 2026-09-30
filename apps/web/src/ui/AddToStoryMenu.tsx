import { useEffect, useRef, useState } from 'react';
import { BookPlus, Check, Plus } from 'lucide-react';
import { addStoryActivities, type Activity, type Story } from '../api';
import { StoryDialog } from './StoryDialog';
import { useStories } from './useStories';
import { t, tn } from '../i18n';

export interface AddToStoryMenuProps {
  readOnly: boolean;
  /** The toolbar's target — the checked group, else the selected row (MapView's toolbarTargetIds). */
  target: Activity[];
  /** The target as the toolbar's tooltips name it ("3 checked activities"), null with none. */
  targetName: string | null;
  /** "3 checked activities (58 km)" — Create story's description of what it's made of. */
  summary: string;
  /** A Story just made of the target — MapView opens it on the Stories tab. */
  onCreated: (story: Story) => void;
  /** An existing Story the target was just added to, as the server returned it. */
  onAdded: (story: Story) => void;
}

/**
 * The Activities toolbar's Add to story (`SPEC.md` FR-5.16): the one place activities go into a
 * Story — where they're picked. A menu over the toolbar's target: "New story…" (StoryDialog.tsx
 * in its create mode, a Story made of the target), then every Story, newest first, fetched each
 * time the menu opens (useStories). Clicking a Story adds the target to it with one
 * `POST /v1/stories/{id}/activities` and closes the menu; a Story already holding all of the
 * target is ticked and can't be picked. Taking an activity out of a Story is the Stories tab's
 * (StoriesTab.tsx), where the Story is open.
 */
export function AddToStoryMenu({ readOnly, target, targetName, summary, onCreated, onAdded }: AddToStoryMenuProps) {
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [adding, setAdding] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const stories = useStories(open);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!ref.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  // A new target is a new question: close the menu rather than leave it answering the old one.
  const targetKey = target.map((a) => a.id).join(',');
  useEffect(() => {
    setOpen(false);
    setError(null);
  }, [targetKey]);

  const ids = target.map((a) => a.id);
  const title = readOnly
    ? t('stories.demo_add')
    : targetName === null
      ? t('activities.no_target')
      : t('stories.add_target', { target: targetName });

  async function add(story: Story) {
    if (adding !== null) return;
    setAdding(story.id);
    setError(null);
    try {
      const updated = await addStoryActivities(story.id, ids);
      setOpen(false);
      onAdded(updated);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('common.something_wrong'));
    } finally {
      setAdding(null);
    }
  }

  return (
    <div className="story-menu" ref={ref}>
      <button
        type="button"
        className="activities-panel__add-story"
        data-testid="add-to-story"
        disabled={readOnly || targetName === null}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={title}
        title={title}
        onClick={() => {
          setError(null);
          setOpen((o) => !o);
        }}
      >
        <BookPlus size={16} />
      </button>
      {open && (
        <div className="story-menu__panel" role="menu" aria-label={title} data-testid="add-to-story-menu">
          <button
            type="button"
            role="menuitem"
            className="story-menu__item story-menu__item--new"
            onClick={() => {
              setOpen(false);
              setCreating(true);
            }}
          >
            <Plus size={14} aria-hidden="true" />
            <span className="story-menu__name">{t('stories.new')}</span>
          </button>
          {(stories.stories.length > 0 || !stories.ready) && <div className="story-menu__divider" aria-hidden="true" />}
          {!stories.ready && !stories.error && <p className="story-menu__note">{t('common.loading')}</p>}
          {stories.error && <p className="story-menu__note story-menu__note--error">{stories.error}</p>}
          {stories.ready && (
            <div className="story-menu__list">
              {stories.stories.map((story) => {
                const holdsAll = ids.every((id) => story.activityIds.includes(id));
                return (
                  <button
                    key={story.id}
                    type="button"
                    role="menuitem"
                    className="story-menu__item"
                    disabled={holdsAll || adding !== null}
                    title={holdsAll ? t('stories.holds_all') : undefined}
                    onClick={() => void add(story)}
                  >
                    <span className="story-menu__check" aria-hidden="true">
                      {holdsAll && <Check size={14} />}
                    </span>
                    <span className="story-menu__name">{story.name}</span>
                    <span className="story-menu__count">
                      {adding === story.id ? t('stories.adding') : tn('activities.count', story.activityIds.length)}
                    </span>
                  </button>
                );
              })}
            </div>
          )}
          {error && <p className="story-menu__note story-menu__note--error">{error}</p>}
        </div>
      )}
      {creating && (
        <StoryDialog mode="create" activityIds={ids} summary={summary} onSaved={onCreated} onClose={() => setCreating(false)} />
      )}
    </div>
  );
}
