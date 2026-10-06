import { useEffect, useRef, useState, type RefObject } from 'react';
import { Lock, X } from 'lucide-react';
import type { Activity } from '../api';
import { formatActivityType, formatDistance, formatDuration, formatStartedAt } from './format';
import type { UnitSystem } from './units';
import { t, tn } from '../i18n';

/** A row's primary line: its user-entered name (§4.7's revised decision) when it has one, else
 *  its start date/time — also how the toolbar names a single selected activity. */
export function rowLabel(activity: Activity): string {
  return activity.name?.trim() || formatStartedAt(activity.startedAt);
}

export interface ActivityRowProps {
  activity: Activity;
  system: UnitSystem;
  focused: boolean;
  hovered: boolean;
  hidden: boolean;
  /** The Story open on the Stories tab: every row there is in it, so only its other Stories
   *  are worth a badge. */
  openStoryId?: string;
  /** The Activities tab's checkbox; the Stories tab's rows have none. */
  checkbox?: { checked: boolean; onToggle: () => void };
  /** The Stories tab's Remove from story — an × at the row's end, shown on hover or focus (always
   *  on a phone). */
  remove?: { label: string; disabled: boolean; onRemove: () => void };
  /** A Story badge clicked — opens that Story on the Stories tab (MapView's enterStory). */
  onOpenStory?: (id: string) => void;
  onFocus: () => void;
  onHover: (id: string | null) => void;
}

/** One activity in a panel list — the Activities tab's (ActivitiesPanel.tsx, which documents the
 *  four row interactions) and an open Story's on the Stories tab (StoriesTab.tsx). */
export function ActivityRow({
  activity,
  system,
  focused,
  hovered,
  hidden,
  openStoryId,
  checkbox,
  remove,
  onOpenStory,
  onFocus,
  onHover,
}: ActivityRowProps) {
  const isPending = activity.pending;
  const otherStories = activity.stories.filter((s) => s.id !== openStoryId);
  // A user-entered name (§4.7's revised decision) leads; started_at is the fallback for a row
  // that has none — never the reverse, so an activity's date doesn't disappear from the list
  // just because it also has a name (shown in the meta line below instead). Sorting itself is
  // untouched either way: the list's order comes entirely from the server's own
  // `ORDER BY started_at DESC`, never from this label.
  const displayName = activity.name?.trim() || null;
  const label = rowLabel(activity);
  const classes = ['activities-panel__row'];
  // Only the selected row is bold on the map, so only it gets the row highlight — a checked
  // row's ticked box is its only mark.
  if (focused) classes.push('activities-panel__row--selected');
  if (hidden) classes.push('activities-panel__row--hidden');
  if (isPending) classes.push('activities-panel__row--pending');
  return (
    <li
      data-activity-id={activity.id}
      className={classes.join(' ')}
      // The description (§4.7.4) shows as a hover tooltip only — no second visible line, and
      // undefined (not an empty string) when there is none, so a row with nothing written there
      // gets no title attribute at all rather than an empty one a browser might still render as
      // an inert tooltip.
      title={activity.description ?? undefined}
      onMouseEnter={() => onHover(activity.id)}
      onMouseLeave={() => onHover(null)}
    >
      {checkbox && (
        <input
          type="checkbox"
          className="activities-panel__checkbox"
          checked={checkbox.checked}
          // A pending row (§4.7.7) is disabled until its reprocess lands, except that an
          // already-checked one can still be unchecked.
          disabled={isPending && !checkbox.checked}
          aria-label={checkbox.checked ? t('activities.row_uncheck', { label }) : t('activities.row_check', { label })}
          onChange={checkbox.onToggle}
        />
      )}
      <button
        type="button"
        className="activities-panel__text"
        disabled={isPending}
        aria-pressed={focused}
        aria-label={t('activities.fly_to', { label })}
        title={activity.private ? t('activities.private_title') : activity.bbox === null ? t('activities.no_track') : label}
        onClick={onFocus}
      >
        <span className={`activities-panel__title${hovered ? ' activities-panel__title--hovered' : ''}`}>{label}</span>
        <span className="activities-panel__meta">
          {/* The date moves down here, ahead of distance/duration, once a name has taken its
              place as the title above — otherwise it's already the title and repeating it here
              would be redundant. Type trails the line. */}
          {displayName && `${formatStartedAt(activity.startedAt)} · `}
          {/* A private activity has no track, so no distance or duration — "0.0 mi · 0m" would
              read as broken data rather than as hidden on purpose. Its badge says why. */}
          {!activity.private && (
            <>
              {formatDistance(activity.distanceMeters, system)} · {formatDuration(activity.durationSeconds)} ·{' '}
            </>
          )}
          {formatActivityType(activity.activityType)}
        </span>
      </button>
      {(isPending || hidden || activity.private || otherStories.length > 0) && (
        // One right-aligned group, so the badges share one right edge whatever the text beside
        // them does, and a row that's both stacks them there together.
        <span className="activities-panel__badges">
          {otherStories.length > 0 && <StoryBadge stories={otherStories} inOpenStory={openStoryId !== undefined} onOpen={onOpenStory} />}
          {isPending && (
            <span className="activities-panel__hidden-badge" title={t('activities.pending_title')}>
              {t('activities.pending')}
            </span>
          )}
          {activity.private && !isPending && (
            <span className="activities-panel__hidden-badge activities-panel__private-badge" title={t('activities.private_title')}>
              <Lock size={10} aria-hidden="true" />
              {t('activities.private')}
            </span>
          )}
          {hidden && <span className="activities-panel__hidden-badge">{t('activities.hidden')}</span>}
        </span>
      )}
      {remove && (
        <button
          type="button"
          className="activities-panel__row-remove"
          disabled={remove.disabled}
          aria-label={`${remove.label}: ${label}`}
          title={remove.label}
          onClick={remove.onRemove}
        >
          <X size={14} />
        </button>
      )}
    </li>
  );
}

/**
 * A row's Story badge (`SPEC.md` FR-5.1): the Stories this activity is in — besides the open one,
 * on the Stories tab. A button that opens a Story: with one, that Story; with more, a small menu
 * of their names to pick from. Without `onOpen` it's a plain label.
 */
function StoryBadge({
  stories,
  inOpenStory,
  onOpen,
}: {
  stories: Activity['stories'];
  inOpenStory: boolean;
  onOpen: ((id: string) => void) | undefined;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const ref = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    if (!menuOpen) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!ref.current?.contains(event.target as Node)) setMenuOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMenuOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [menuOpen]);

  const label = tn('activities.story_badge', stories.length);
  const names = tn(inOpenStory ? 'activities.in_other_stories' : 'activities.in_stories', stories.length, {
    names: stories.map((s) => s.name).join(', '),
  });
  if (!onOpen) {
    return (
      <span className="activities-panel__hidden-badge activities-panel__story-badge" title={names}>
        {label}
      </span>
    );
  }
  const only = stories.length === 1 ? stories[0]! : null;
  const title = only ? t('activities.open_story', { name: only.name }) : names;
  return (
    <span className="activities-panel__story-badge-wrap" ref={ref}>
      <button
        type="button"
        className="activities-panel__hidden-badge activities-panel__story-badge"
        aria-label={title}
        title={title}
        aria-haspopup={only ? undefined : 'menu'}
        aria-expanded={only ? undefined : menuOpen}
        onClick={() => {
          if (only) onOpen(only.id);
          else setMenuOpen((o) => !o);
        }}
      >
        {label}
      </button>
      {menuOpen && (
        <span className="story-menu__panel activities-panel__story-badge-menu" role="menu">
          {stories.map((story) => (
            <button
              key={story.id}
              type="button"
              role="menuitem"
              className="story-menu__item"
              onClick={() => {
                setMenuOpen(false);
                onOpen(story.id);
              }}
            >
              <span className="story-menu__name">{story.name}</span>
            </button>
          ))}
        </span>
      )}
    </span>
  );
}

/**
 * Scrolls the newly row-click-focused activity into view, centered — reported live as having
 * to hunt for the now-bolded row by eye after clicking a track on the map, since a long list
 * scrolled it out of view as often as not. Keyed on focusedId alone (not the checked group): a
 * row click always has exactly one target to center on, while a checkbox spree building up a
 * multi-row group has no single row to scroll to, and would otherwise jerk the list around
 * after every click. `data-activity-id` on the row is what this looks up.
 *
 * Scrolls the list alone, not `row.scrollIntoView()`: that scrolls every ancestor too, and on a
 * phone the collapsed sheet (`overflow: hidden`, its tab row tall) is one — a tap on a track scrolled
 * the whole sheet up by its own content, pushing the header and the expand strip out of its
 * visible box, where no gesture could bring them back.
 */
export function useScrollFocusedRow(listRef: RefObject<HTMLElement | null>, focusedId: string | null) {
  useEffect(() => {
    if (focusedId === null) return;
    const list = listRef.current;
    const row = list?.querySelector(`[data-activity-id="${CSS.escape(focusedId)}"]`);
    if (!list || !row) return;
    const rowRect = row.getBoundingClientRect();
    const offset = rowRect.top - list.getBoundingClientRect().top;
    list.scrollTo({
      top: list.scrollTop + offset - (list.clientHeight - rowRect.height) / 2,
      behavior: 'smooth',
    });
  }, [listRef, focusedId]);
}