import { useRef, useState } from 'react';
import { Pencil, Play, Trash2 } from 'lucide-react';
import { deleteStory, type Activity, type Story, type StoryTotals } from '../api';
import { ActivityRow, useScrollFocusedRow } from './ActivityRow';
import { ConfirmDialog } from './ConfirmDialog';
import { StoryDialog } from './StoryDialog';
import { formatActivityType, formatDuration, formatTotalDistance } from './format';
import { useUnitSystem, type UnitSystem } from './units';
import { t, tn } from '../i18n';

/** A Story's numbers, or one type's share of them, in one line — "3 activities · 58 km · 3h 26m
 *  moving". */
function storyStatsLine(totals: StoryTotals, system: UnitSystem): string {
  return t('stories.stats', {
    activities: tn('activities.count', totals.count),
    distance: formatTotalDistance(totals.distanceMeters, system),
    moving: formatDuration(totals.movingSeconds),
  });
}

export interface StoriesTabProps {
  readOnly: boolean;
  /** Newest first — useStories. */
  stories: Story[];
  storiesReady: boolean;
  storiesError: string | null;
  /** The open Story's id — MapView's storyId. At most one is open, and once the list has
   *  loaded, one always is (MapView opens the newest). */
  openId: string | null;
  /** The open Story as `GET /v1/stories/{id}` last returned it (useStory) — its whole-Story
   *  statistics for the footer. */
  openStory: Story | null;
  /** Why the open Story couldn't be read — a `?story=` that doesn't exist or isn't this account's. */
  openError: string | null;
  /** The open Story's activities, following the timeline's selection — MapView's list. */
  activities: Activity[];
  activitiesLoading: boolean;
  activitiesError: string | null;
  focusedId: string | null;
  hoveredId: string | null;
  onOpen: (id: string) => void;
  onFocus: (id: string) => void;
  onClearFocus: () => void;
  onHoverActivity: (id: string | null) => void;
  /** Renamed or re-described with a folder's pencil — the Story as the server returned it. */
  onEdited: (story: Story) => void;
  /** Deleted with a folder's trash, once the server has deleted it. */
  onDeleted: (id: string) => void;
}

/**
 * The Activities panel's Stories tab (`SPEC.md` FR-14.6): every Story as a folder, newest first,
 * exactly one of them open. Opening one is viewing it — MapView narrows the drawn tracks, the
 * timeline and the list to it, and this tab shows that list inside the folder and the whole
 * Story's statistics in the footer. The rows are for looking only (hover preview, click to
 * focus); what's in a Story changes from the Edit window's Stories tab. A folder's pencil and
 * trash rename and delete that Story.
 */
export function StoriesTab({
  readOnly,
  stories,
  storiesReady,
  storiesError,
  openId,
  openStory,
  openError,
  activities,
  activitiesLoading,
  activitiesError,
  focusedId,
  hoveredId,
  onOpen,
  onFocus,
  onClearFocus,
  onHoverActivity,
  onEdited,
  onDeleted,
}: StoriesTabProps) {
  const system = useUnitSystem();
  const [editing, setEditing] = useState<Story | null>(null);
  const [deleting, setDeleting] = useState<Story | null>(null);
  const listRef = useRef<HTMLUListElement>(null);
  useScrollFocusedRow(listRef, focusedId);

  const editTitle = readOnly ? t('stories.demo_edit') : t('stories.edit');
  const deleteTitle = readOnly ? t('stories.demo_delete') : t('stories.delete');
  const openListed = openId !== null && stories.some((s) => s.id === openId);

  return (
    <>
      <ul className="stories-tab" data-testid="stories-list" ref={listRef}>
        {openError && !openListed && <li className="activities-panel__note activities-panel__note--error">{openError}</li>}
        {stories.map((listed) => {
          const open = listed.id === openId;
          // The open Story's own copy is the fresher one — its numbers follow edits made since.
          const story = open && openStory?.id === listed.id ? openStory : listed;
          return (
            <li key={story.id} className={`stories-tab__story${open ? ' stories-tab__story--open' : ''}`}>
              <div className="stories-tab__folder">
                <button
                  type="button"
                  className="stories-tab__toggle"
                  aria-expanded={open}
                  data-testid="stories-tab-toggle"
                  onClick={() => {
                    if (!open) onOpen(story.id);
                  }}
                >
                  <Play size={12} className="stories-tab__chevron" aria-hidden="true" />
                  <span className="stories-tab__name">{story.name}</span>
                </button>
                <button
                  type="button"
                  className="activities-panel__edit stories-tab__action"
                  disabled={readOnly}
                  onClick={() => setEditing(story)}
                  aria-label={`${editTitle}: ${story.name}`}
                  title={editTitle}
                >
                  <Pencil size={14} />
                </button>
                <button
                  type="button"
                  className="activities-panel__delete stories-tab__action"
                  disabled={readOnly}
                  onClick={() => setDeleting(story)}
                  aria-label={`${deleteTitle}: ${story.name}`}
                  title={deleteTitle}
                >
                  <Trash2 size={14} />
                </button>
              </div>
              {open && (
                // A click on the body's own empty space clears the selection, as on the
                // Activities tab's list.
                <div
                  className="stories-tab__body"
                  onClick={(e) => {
                    if (e.target === e.currentTarget) onClearFocus();
                  }}
                >
                  {story.description !== '' && <p className="stories-tab__description">{story.description}</p>}
                  <ul className="stories-tab__rows">
                    {activities.map((activity) => (
                      <ActivityRow
                        key={activity.id}
                        activity={activity}
                        system={system}
                        focused={focusedId === activity.id}
                        hovered={hoveredId === activity.id}
                        hidden={false}
                        openStoryId={story.id}
                        onFocus={() => onFocus(activity.id)}
                        onHover={onHoverActivity}
                      />
                    ))}
                    {activitiesLoading && <li className="activities-panel__note">{t('common.loading')}</li>}
                    {activitiesError && !activitiesLoading && (
                      <li className="activities-panel__note activities-panel__note--error">{activitiesError}</li>
                    )}
                    {!activitiesLoading && !activitiesError && activities.length === 0 && (
                      <li className="activities-panel__note">
                        {story.stats.count === 0 ? t('stories.no_activities') : t('activities.none_match')}
                      </li>
                    )}
                  </ul>
                </div>
              )}
            </li>
          );
        })}
        {!storiesReady && !storiesError && stories.length === 0 && <li className="activities-panel__note">{t('common.loading')}</li>}
        {storiesError && <li className="activities-panel__note activities-panel__note--error">{storiesError}</li>}
        {storiesReady && stories.length === 0 && <li className="activities-panel__note">{t('stories.empty')}</li>}
      </ul>

      {/* The whole Story, whatever the timeline selects (FR-14.1). */}
      {openStory && openStory.id === openId && (
        <div className="activities-panel__footer stories-tab__stats" data-testid="stories-tab-stats">
          {openStory.stats.count > 0 ? (
            <>
              <p className="stories-tab__stats-line">{storyStatsLine(openStory.stats, system)}</p>
              <table className="stories-tab__types" aria-label={t('stories.by_type')}>
                <tbody>
                  {openStory.stats.byType.map((type) => (
                    <tr key={type.activityType}>
                      <th scope="row">{formatActivityType(type.activityType)}</th>
                      <td>{storyStatsLine(type, system)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          ) : (
            <p className="stories-tab__stats-line">{t('stories.no_activities')}</p>
          )}
        </div>
      )}

      {editing && (
        <StoryDialog mode="edit" story={editing} onSaved={onEdited} onClose={() => setEditing(null)} />
      )}

      {deleting && (
        <ConfirmDialog
          title={t('stories.delete_title')}
          message={t('stories.delete_confirm', { name: deleting.name })}
          confirmLabel={t('stories.delete_button')}
          busyLabel={t('common.deleting')}
          onConfirm={async () => {
            await deleteStory(deleting.id);
            onDeleted(deleting.id);
          }}
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  );
}