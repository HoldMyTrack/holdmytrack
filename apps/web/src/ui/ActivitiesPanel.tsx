import { useCallback, useEffect, useRef, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { ChevronDown, ChevronUp, Eye, EyeOff, Focus, Pencil, Trash2 } from 'lucide-react';
import { deleteActivity, type Activity, type ActivityTotals, type DuplicateActivity, type Story } from '../api';
import { ActivityRow, rowLabel, useScrollFocusedRow } from './ActivityRow';
import { AddToStoryMenu } from './AddToStoryMenu';
import { ConfirmDialog } from './ConfirmDialog';
import { DateRangeSlider, type DateRangeSliderProps } from './DateRangeSlider';
import { DistanceFilter } from './DistanceFilter';
import { PrivateLocationsPanel } from './PrivateLocationsPanel';
import { StoriesTab } from './StoriesTab';
import { SyncTab } from './SyncTab';
import type { DistanceRange, TypeFacet } from './activityFacets';
import { formatActivityType, formatDistance, formatIngestSource, formatStartedAt, formatTotalDistance } from './format';
import { useUnitSystem } from './units';
import type { ImportsState } from './useImports';
import { lang, t, tn } from '../i18n';

/**
 * The left sidebar, with four tabs: **Activities** (the list, below), **Stories** (StoriesTab.tsx
 * — every Story as a folder, the open one's activities inside it, FR-14.6), **Sync** (SyncTab.tsx
 * — file upload and the import history) and **Privacy** (PrivateLocationsPanel.tsx — FR-8.1's
 * list, whose editor floats over the map like Edit track's). The tab is MapView's state, so
 * `/?private-locations` can open onto Privacy, `/?story=` onto Stories, and the tab survives
 * this panel unmounting for Fog/Heatmap; the upload queue behind Sync is MapView's too
 * (useImports), so it keeps running while another tab is showing.
 *
 * The activity list — a permanent left sidebar (not a collapsible
 * dropdown, nor a paginated one: §4.7's list
 * endpoint no longer pages, so this renders every row the current date range matched, filtered
 * further by TYPE/DISTANCE). MapView owns the fetch, the range, and both filters' state; this
 * component is purely presentational plus its own dropdown-open/panel-resize local UI state.
 *
 * The date range is the list's first filter: DateRangeSlider.tsx, under the subtext line, on
 * this tab alone — the other tabs aren't about a stretch of time, so nothing there is filtered
 * by one. On a phone index.css pins it to the bottom of the screen under the sheet instead, so
 * the range can still be changed with the sheet collapsed and the map in view.
 *
 * The other filters share one row between the slider and the toolbar below: the Type dropdown
 * (TYPE checkboxes plus an "All types" convenience row that clears every exclusion), then
 * DistanceFilter.tsx, always visible — no longer hidden behind the old "Filter" toggle button,
 * which is gone.
 *
 * A header toolbar sits directly above the row list. Every single-item action (edit, delete,
 * hide) lives here, not on the row — a row has only its checkbox and its text; there are no
 * per-row icon buttons at all. The toolbar acts on its *target* (MapView's toolbarTargetIds):
 * the checked group whenever anything is checked, else the selected row alone. A master
 * checkbox mirrors the checked group's state, then icon actions over the target — Show/hide,
 * Edit (the Edit window, EditActivityWindow.tsx: its Activity tab edits Type/Name/Description
 * for exactly one activity and Type only for more than one, its Track tab one activity's
 * track), Add to story (AddToStoryMenu.tsx), Delete, and, set off by a divider, Focus on map
 * (fly-to-fit). Each one's tooltip
 * names the target ("3 checked activities", or the selected row's own label), so which of the
 * two applies is never a guess. The footer keeps only the target's "N selected · X km" summary.
 *
 * Four interactions per row, all living in MapView (which holds the map instance they need;
 * this component owns only rendering and its own local UI state). Selection and the checkbox
 * group are two fully independent mechanisms — reported live as wrongly coupled once, when a
 * row click also silently checked/unchecked boxes:
 *  - Hovering the row (anywhere on it) previews that activity's track — bold on the map,
 *    nothing else — for as long as the pointer stays there. Purely a preview: the camera
 *    never moves for it, and it clears the moment the pointer leaves.
 *  - Clicking the row's text *selects* it (the row-click focus) — bolds its track, highlights
 *    the row, flies there, and replaces whichever row was selected before. Clicking empty
 *    space in the list (or on the map) clears it. Never touches any checkbox.
 *  - The checkbox only *adds or removes* this one row from the checked group the toolbar acts
 *    on — no bold track, no row highlight, no fly. Never touches the selection.
 *  - The toolbar's Show/hide button toggles whether the target's tracks paint on the map at
 *    all, independent of all three of the above; a hidden activity's row dims in place.
 */
export interface ActivitiesPanelProps {
  /** A demo account (`docs/SPEC.md` FR-2.1–FR-2.3) — the
   *  backend already rejects every mutation a demo session attempts (requireNotDemo), so this
   *  disables the controls that would otherwise error, with a `title` explaining why, rather
   *  than either hiding them (which would hide the feature existing at all, undercutting the
   *  demo's whole point of letting someone feel the app) or leaving them enabled to fail.
   *  Group visible stays enabled either way — purely local UI state, never sent to the
   *  backend, so there's nothing for a demo account to be blocked from there. */
  readOnly?: boolean;
  /** Rows already narrowed by TYPE/DISTANCE — what actually renders. */
  activities: Activity[];
  loading: boolean;
  error: string | null;
  /** §4.7's range summary — unfiltered by TYPE/DISTANCE, so "km loaded" always describes the
   *  whole date range regardless of how the two filters below narrow what's shown. */
  totals: ActivityTotals | null;
  facets: TypeFacet[];
  excludedTypes: ReadonlySet<string>;
  onToggleType: (type: string) => void;
  distanceBounds: DistanceRange | null;
  distanceFilter: DistanceRange | null;
  onChangeDistance: (next: DistanceRange | null) => void;
  onResetFilters: () => void;
  /** The checkbox group — additive/subtractive, independent of `focusedId` below. */
  checked: Set<string>;
  /** What the header toolbar acts on — `checked` when it's non-empty, else `focusedId` alone,
   *  else nothing (MapView's toolbarTargetIds). */
  targetIds: Set<string>;
  /** The selected row (row-click focus) — at most one id, independent of `checked` above. */
  focusedId: string | null;
  /** The shared hover-preview id (MapView's `hoveredActivityId`) — fed by both this panel's
   *  own row `onMouseEnter`/`onMouseLeave` below *and* the map's own track hover
   *  (`tracks.ts`'s `onHover`), so hovering a track on the map underlines the matching row's
   *  title here, the same way hovering a row already bolds its track on the map. */
  hoveredId: string | null;
  /** The checkbox: adds/removes this one row from `checked`. */
  onToggle: (id: string) => void;
  /** The row's text: replaces `focusedId` with this one row. */
  onFocus: (id: string) => void;
  /** A click on empty space in the list: clears `focusedId`, leaving `checked` alone. */
  onClearFocus: () => void;
  /** The row's own hover preview, `null` on leave — see the component doc comment above. */
  onHoverActivity: (id: string | null) => void;
  /** Empties the checked group — the header checkbox's "uncheck all" state. */
  onClear: () => void;
  /** Checks every currently-listed row — the header checkbox's "check all" state. */
  onSelectAll: () => void;
  /** Checks every unchecked listed row and unchecks every checked one — the toolbar's
   *  invert-selection icon beside the header checkbox. */
  onInvertSelection: () => void;
  /** Flies to fit the toolbar's target without changing it. */
  onShowSelected: () => void;
  /** Activities currently hidden from the map — dims the row (there's no per-row eye icon any
   *  more), and what the header toolbar's "Group visible" button toggles in bulk over
   *  `targetIds`. */
  hiddenIds: Set<string>;
  /** The header toolbar's "Group visible": if any of the target is currently hidden, show all
   *  of it; otherwise hide all of it. See MapView's
   *  toggleGroupVisibility for the exact rule. */
  onToggleGroupVisibility: () => void;
  /** §4.7.5's toolbar Delete — the only delete entry point (over the toolbar's target: the
   *  checked group, or the selected row alone), so always an array even for one id. Unlike onActivityUpdated, this also has to drop every deleted id from
   *  `checked`/`hiddenIds`/`focusedId` and refresh the map's track layer and totals/histogram
   *  (deleting changes distance/duration, editing never does) — MapView's own
   *  handleActivitiesDeleted does more than a plain reload. */
  onActivitiesDeleted: (ids: string[]) => void;
  /** The toolbar's Edit button, over the toolbar's target — MapView opens the Edit window
   *  (EditActivityWindow.tsx: Activity and Track tabs) over the map. */
  onEdit: (activities: Activity[]) => void;
  /** A Story just made of the toolbar's target with Add to story's "New story…"
   *  (AddToStoryMenu.tsx) — MapView opens it on the Stories tab. */
  onStoryCreated: (story: Story) => void;
  /** An existing Story the toolbar's target was just added to with Add to story, as the server
   *  returned it — MapView reloads the list so the rows' Story badges follow. */
  onStoryAdded: (story: Story) => void;
  /** The Stories tab's list and its open Story — MapView's. */
  stories: StoriesPanel;
  /** FR-3.7's "Not yet built" gap, closed: activities cross-source dedup took out of
   *  circulation, each alongside the richer copy that superseded it — mirrors the Android
   *  app's own duplicates section (`SyncStatusActivity`). Never filtered by the date range or
   *  TYPE/DISTANCE facets above; a duplicate answers "where did my activity go", which isn't
   *  a question scoped to whatever's currently selected. */
  duplicates: DuplicateActivity[];
  duplicatesError: string | null;
  /** The Sync tab's upload queue and history — MapView's useImports. */
  imports: ImportsState;
  /** A Sync-tab row's "View on map" — MapView's viewActivityOnMap. */
  onViewOnMap: (activityId: string, startedAt: string) => void;
  tab: PanelTab;
  onTabChange: (tab: PanelTab) => void;
  /** The Privacy tab's map, for its overlay — null until MapView's map has loaded. */
  map: MapLibreMap | null;
  /** A saved or deleted Private location — MapView's handlePrivateLocationsChanged. */
  onPrivateLocationsChanged: () => void;
  /** The Activities tab's date slider — MapView's selected range and useActivityDays' window. */
  dateRange: DateRangeSliderProps;
}

export type PanelTab = 'activities' | 'stories' | 'sync' | 'private';

/** What the Stories tab needs from MapView beyond the activity list the other tabs share —
 *  see StoriesTab.tsx's props of the same names. */
export interface StoriesPanel {
  stories: Story[];
  ready: boolean;
  error: string | null;
  openId: string | null;
  openStory: Story | null;
  openError: string | null;
  onOpen: (id: string) => void;
  onEdited: (story: Story) => void;
  onDeleted: (id: string) => void;
  onActivityRemoved: (story: Story, activityId: string) => void;
}

export function ActivitiesPanel({
  readOnly = false,
  activities,
  loading,
  error,
  totals,
  facets,
  excludedTypes,
  onToggleType,
  distanceBounds,
  distanceFilter,
  onChangeDistance,
  onResetFilters,
  checked,
  targetIds,
  focusedId,
  hoveredId,
  onToggle,
  onFocus,
  onClearFocus,
  onHoverActivity,
  onClear,
  onSelectAll,
  onInvertSelection,
  onShowSelected,
  hiddenIds,
  onToggleGroupVisibility,
  onActivitiesDeleted,
  onEdit,
  onStoryCreated,
  onStoryAdded,
  stories,
  duplicates,
  duplicatesError,
  imports,
  onViewOnMap,
  tab,
  onTabChange: setTab,
  map,
  onPrivateLocationsChanged,
  dateRange,
}: ActivitiesPanelProps) {
  const hasActiveFilters = excludedTypes.size > 0 || distanceFilter !== null;

  // The Type dropdown, in the filter row beside Distance — closed by
  // default, same "most sessions don't start by narrowing filters" reasoning the old "Filter"
  // toggle button had. Dismiss on outside click or Escape, the same hand-wired pattern
  // the Duplicates disclosure below uses too (not shared into a hook for two call sites).
  const [typeFilterOpen, setTypeFilterOpen] = useState(false);
  const typeFilterRef = useRef<HTMLDivElement>(null);

  // The Duplicates disclosure, same closed-by-default/dismiss-on-outside-click pattern as the
  // Type dropdown above — a small footer link, not part of the main row list, since a
  // duplicate isn't one of "my activities" in the sense the rest of this panel means.
  const [duplicatesOpen, setDuplicatesOpen] = useState(false);
  const duplicatesRef = useRef<HTMLDivElement>(null);

  // The row list, for useScrollFocusedRow (ActivityRow.tsx): a newly focused row scrolls into view.
  const listRef = useRef<HTMLUListElement>(null);
  useScrollFocusedRow(listRef, focusedId);

  // The tab strip scrolls sideways when the tabs don't fit (index.css); the selected tab is
  // brought into view, scrolling the strip alone, for the reason useScrollFocusedRow gives.
  const tabsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const strip = tabsRef.current;
    const selected = strip?.querySelector<HTMLElement>('[aria-selected="true"]');
    if (!strip || !selected) return;
    const left = selected.getBoundingClientRect().left - strip.getBoundingClientRect().left + strip.scrollLeft;
    if (left < strip.scrollLeft || left + selected.offsetWidth > strip.scrollLeft + strip.clientWidth) {
      strip.scrollTo({ left: left - (strip.clientWidth - selected.offsetWidth) / 2, behavior: 'smooth' });
    }
  }, [tab]);

  useEffect(() => {
    if (!typeFilterOpen) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!typeFilterRef.current?.contains(event.target as Node)) setTypeFilterOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setTypeFilterOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [typeFilterOpen]);

  useEffect(() => {
    if (!duplicatesOpen) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!duplicatesRef.current?.contains(event.target as Node)) setDuplicatesOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setDuplicatesOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [duplicatesOpen]);


  // §4.7.5's delete confirm dialog — the toolbar's Delete is the only delete entry point
  // (there's no per-row delete button), so this covers both the one-activity and many-activity
  // case uniformly. The confirm title and message below branch on targetActivities.length.
  const [deletingGroup, setDeletingGroup] = useState(false);

  // Mobile-only bottom sheet (index.css's `@media (max-width: 768px)` layer) — collapsed by
  // default, same reasoning as typeFilterOpen above. Desktop CSS never reacts to the
  // `--sheet-expanded` modifier class this drives, so toggling it there is an inert no-op,
  // not a behavior change; see the subtext button below for why that's safe to leave wired
  // unconditionally rather than gated behind a JS media-query check.
  const [sheetExpanded, setSheetExpanded] = useState(false);

  // Wider than the old fixed 320px, and now draggable — a full "Sep 11, 2026, 09:00 AM"
  // title plus its TYPE label was clipping at 320px. Local state, not lifted to MapView:
  // nothing outside this component's own layout needs to know the panel's width, the same
  // reasoning as `typeFilterOpen` above. Not persisted across reloads on purpose — no other UI
  // preference in this app is, and adding the one-off localStorage-plus-SSR-mismatch
  // handling for just this value isn't worth it before anything else here does the same.
  const [panelWidth, setPanelWidth] = useState(380);
  const resizeRef = useRef<{ pointerId: number; startX: number; startWidth: number } | null>(null);
  const MIN_PANEL_WIDTH = 260;
  const MAX_PANEL_WIDTH = 560;

  const onResizePointerDown = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      event.currentTarget.setPointerCapture(event.pointerId);
      resizeRef.current = { pointerId: event.pointerId, startX: event.clientX, startWidth: panelWidth };
    },
    [panelWidth],
  );
  const onResizePointerMove = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    const drag = resizeRef.current;
    if (!drag || event.pointerId !== drag.pointerId) return;
    const next = drag.startWidth + (event.clientX - drag.startX);
    setPanelWidth(Math.min(Math.max(next, MIN_PANEL_WIDTH), MAX_PANEL_WIDTH));
  }, []);
  const onResizePointerUp = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (resizeRef.current?.pointerId === event.pointerId) resizeRef.current = null;
  }, []);

  const checkedCount = activities.filter((a) => checked.has(a.id)).length;
  // The toolbar's target (see the component doc comment) — the footer summary describes it
  // too, so the "N selected · X km" line always matches what the toolbar would act on.
  const targetActivities = activities.filter((a) => targetIds.has(a.id));
  const targetMeters = targetActivities.reduce((sum, a) => sum + (a.distanceMeters ?? 0), 0);
  const system = useUnitSystem();
  // The toolbar tooltips' name for the target. Keyed on `checked`, not on the count alone: one
  // checked activity is still "1 checked activity", so it's clear the group wins over a
  // selected row, rather than reading like the selection itself.
  // Null when none of the target is listed (e.g. every checked row filtered out by TYPE), which
  // disables the toolbar the same as having no target at all.
  const firstTarget = targetActivities[0];
  const targetName =
    firstTarget === undefined
      ? null
      : checked.size > 0
        ? tn('activities.checked_count', targetActivities.length)
        : t('activities.target_one', { label: rowLabel(firstTarget) });
  const hasTarget = targetName !== null;

  // The header toolbar's master checkbox — checked once every currently-listed row is in
  // `checked`, indeterminate for a partial selection, unchecked for none. `indeterminate`
  // has no JSX prop (it's a DOM property, not an HTML attribute), hence the ref + effect.
  const selectAllRef = useRef<HTMLInputElement>(null);
  const allChecked = activities.length > 0 && checkedCount === activities.length;
  const someChecked = checkedCount > 0 && !allChecked;
  useEffect(() => {
    if (selectAllRef.current) selectAllRef.current.indeterminate = someChecked;
  }, [someChecked]);

  // §4.7.5's ConfirmDialog message needs the target's own summary: the count and total
  // distance for a group, the same two numbers the footer summary already shows, or the
  // activity's own label for a single selected row.
  const groupSummary = t('activities.group_summary', {
    activities: targetName ?? '',
    distance: formatTotalDistance(targetMeters, system),
  });

  // Group visible's own icon mirrors the row-level eye icon's open/closed convention: closed
  // (about to reveal) once any of the target is currently hidden, open otherwise — matching
  // MapView's toggleGroupVisibility rule (show all of it the moment any of it is hidden).
  const groupHasHidden = targetActivities.some((a) => hiddenIds.has(a.id));

  // Pending rows can't be edited or deleted until their reprocess lands — the job would
  // otherwise race the edit or delete for the same row.
  const groupHasPending = targetActivities.some((a) => a.pending);

  // Each toolbar action's tooltip (and accessible name) says what it would act on; with no
  // target at all, the disabled buttons say how to get one instead.
  const noTarget = t('activities.no_target');
  const visibilityTitle =
    targetName === null
      ? noTarget
      : t(groupHasHidden ? 'activities.show_target' : 'activities.hide_target', { target: targetName });
  const editTitle =
    targetName === null
      ? noTarget
      : t(targetActivities.length === 1 ? 'activities.edit_one' : 'activities.edit_many', { target: targetName });
  const deleteTitle = targetName === null ? noTarget : t('activities.delete_target', { target: targetName });

  const focusTitle = targetName === null ? noTarget : t('activities.focus_target', { target: targetName });

  return (
    <div
      className={`activities-panel${sheetExpanded ? ' activities-panel--sheet-expanded' : ''}${tab === 'activities' ? ' activities-panel--dated' : ''}`}
      data-testid="activities-panel"
      // A CSS custom property, not a direct `width`, specifically so the mobile media query
      // can override it with a plain `width: 100%` rule — an inline style always beats an
      // external stylesheet rule short of `!important` (which this codebase otherwise never
      // needs), but a custom property consumed by `width: var(--panel-width)` in the base
      // rule is just a normal declaration the media query's own later, equal-specificity
      // `width` rule can cleanly win over.
      style={{ '--panel-width': `${panelWidth}px` } as React.CSSProperties}
    >
      <div
        className="activities-panel__resize-handle"
        data-testid="activities-panel-resize"
        role="separator"
        aria-orientation="vertical"
        aria-label={t('activities.resize')}
        onPointerDown={onResizePointerDown}
        onPointerMove={onResizePointerMove}
        onPointerUp={onResizePointerUp}
        onPointerCancel={onResizePointerUp}
      />
      <div className="activities-panel__head" role="tablist" aria-label={t('activities.panel')} ref={tabsRef}>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'activities'}
          className="activities-panel__tab"
          data-testid="activities-panel-tab-activities"
          onClick={() => setTab('activities')}
        >
          <span className="activities-panel__heading-text">{t('activities.tab')}</span>
          {/* On the Stories tab the list is the open Story's, not the date range's. */}
          {tab !== 'stories' && <span className="activities-panel__badge">{activities.length.toLocaleString(lang)}</span>}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'stories'}
          className="activities-panel__tab"
          data-testid="activities-panel-tab-stories"
          onClick={() => setTab('stories')}
        >
          <span className="activities-panel__heading-text">{t('stories.tab')}</span>
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'sync'}
          className="activities-panel__tab"
          data-testid="activities-panel-tab-sync"
          onClick={() => setTab('sync')}
        >
          <span className="activities-panel__heading-text">{t('sync.tab')}</span>
          {/* Visible from the Activities tab too, so an upload's progress doesn't disappear
              the moment you switch away from it. */}
          {imports.badgeCount > 0 && <span className="activities-panel__sync-badge">{imports.badgeCount}</span>}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'private'}
          className="activities-panel__tab"
          data-testid="activities-panel-tab-private"
          onClick={() => setTab('private')}
        >
          <span className="activities-panel__heading-text">{t('private.tab')}</span>
        </button>
      </div>
      {/* A <button>, not a <p> — on mobile this is the bottom sheet's own peek-strip tap
          target (expand/collapse), styled identically to the old plain text on desktop
          (index.css keeps `cursor: default` there) so clicking it is a harmless, invisible
          no-op at desktop width rather than a behavior change. */}
      <button
        type="button"
        className="activities-panel__subtext"
        data-testid="activities-panel-sheet-toggle"
        aria-expanded={sheetExpanded}
        onClick={() => setSheetExpanded((expanded) => !expanded)}
      >
        {tab === 'sync'
          ? t('sync.subtext')
          : tab === 'private'
            ? t('private.subtitle')
            : tab === 'stories'
              ? t('stories.subtext')
              : totals !== null
                ? t('activities.loaded', { distance: formatTotalDistance(totals.distanceMeters, system) })
                : t('common.loading')}
        <span className="activities-panel__sheet-chevron" aria-hidden="true">
          {sheetExpanded ? <ChevronDown size={20} /> : <ChevronUp size={20} />}
        </span>
      </button>

      {tab === 'private' ? (
        map ? (
          <PrivateLocationsPanel
            map={map}
            readOnly={readOnly}
            onChanged={onPrivateLocationsChanged}
            // On a phone the expanded sheet would cover the editor window and the circle it edits.
            // No visible effect at desktop width.
            onEditorOpen={() => setSheetExpanded(false)}
          />
        ) : (
          <p className="edit-track__note">{t('common.loading')}</p>
        )
      ) : tab === 'stories' ? (
        <StoriesTab
          readOnly={readOnly}
          stories={stories.stories}
          storiesReady={stories.ready}
          storiesError={stories.error}
          openId={stories.openId}
          openStory={stories.openStory}
          openError={stories.openError}
          activities={activities}
          activitiesLoading={loading}
          activitiesError={error}
          focusedId={focusedId}
          hoveredId={hoveredId}
          onOpen={(id) => {
            // On a phone the expanded sheet would cover the Story's tracks, as after View on map.
            setSheetExpanded(false);
            stories.onOpen(id);
          }}
          onFocus={onFocus}
          onClearFocus={onClearFocus}
          onHoverActivity={onHoverActivity}
          onEdited={stories.onEdited}
          onDeleted={stories.onDeleted}
          onActivityRemoved={stories.onActivityRemoved}
        />
      ) : tab === 'sync' ? (
        <SyncTab
          imports={imports}
          readOnly={readOnly}
          onViewOnMap={(activityId, startedAt) => {
            setTab('activities');
            // On a phone the sheet is expanded to show this tab, and would stay drawn over the
            // very map the link is meant to show. No visible effect at desktop width.
            setSheetExpanded(false);
            onViewOnMap(activityId, startedAt);
          }}
        />
      ) : (
        <>
          <div className="activities-panel__dates">
            <DateRangeSlider {...dateRange} />
          </div>
          {/* The two filters side by side: Type's dropdown, then Distance's slider taking the
              rest of the row. */}
          <div className="activities-panel__filters">
            <div className="activities-panel__type-dropdown" ref={typeFilterRef}>
              <button
                type="button"
                className="activities-panel__type-trigger"
                aria-haspopup="true"
                aria-expanded={typeFilterOpen}
                onClick={() => setTypeFilterOpen((open) => !open)}
              >
                {t('activities.type')}
                {excludedTypes.size > 0 && <span className="activities-panel__type-trigger-dot" aria-hidden="true" />}
                <span className="activities-panel__type-trigger-caret" aria-hidden="true">
                  <ChevronDown size={14} />
                </span>
              </button>
              {typeFilterOpen && (
                <div className="activities-panel__type-panel" role="dialog" aria-label={t('activities.filter_by_type')}>
                  {facets.length === 0 ? (
                    <p className="activities-panel__type-panel-empty">{t('activities.type_empty')}</p>
                  ) : (
                    <>
                      {/* A select-all convenience, not a real toggle — it only ever clears every
                          exclusion (looping onToggleType over the current excluded set re-includes
                          each one; there's no separate prop for "clear all"), matching how "Reset
                          filters" elsewhere in this panel also only ever clears forward. Clicking
                          it while every type is already shown is a no-op. */}
                      <label htmlFor="activities-toolbar-type-all" className="activity-filters__type-item activity-filters__type-item--all">
                        <input
                          id="activities-toolbar-type-all"
                          type="checkbox"
                          checked={excludedTypes.size === 0}
                          onChange={() => {
                            for (const type of excludedTypes) onToggleType(type);
                          }}
                        />
                        <span className="activity-filters__type-label">{t('activities.all_types')}</span>
                      </label>
                      <div className="activities-panel__type-panel-divider" aria-hidden="true" />
                      <div className="activity-filters__type-list">
                        {facets.map((facet) => {
                          const included = !excludedTypes.has(facet.type);
                          const inputId = `activities-toolbar-type-${facet.type}`;
                          return (
                            <label key={facet.type} htmlFor={inputId} className="activity-filters__type-item">
                              <input id={inputId} type="checkbox" checked={included} onChange={() => onToggleType(facet.type)} />
                              <span className="activity-filters__type-label">{formatActivityType(facet.type)}</span>
                              <span className="activity-filters__type-count">{facet.count}</span>
                            </label>
                          );
                        })}
                      </div>
                    </>
                  )}
                </div>
              )}
            </div>
            <DistanceFilter
              bounds={distanceBounds}
              value={distanceFilter}
              onChangeDistance={onChangeDistance}
              onReset={onResetFilters}
              hasActiveFilters={hasActiveFilters}
            />
          </div>

          {/* The header toolbar — right above the row list. There are no per-row action icons
              to stay column-aligned with (Visible/Edit/Delete all live here, operating on the
              toolbar's target), so this is a plain compact strip: select-all checkbox, the
              invert-selection icon, a spacer, the action chips, a divider, then the one
              accent-tinted "focus the map on the target" action. */}
          <div className="activities-panel__toolbar">
            <input
              ref={selectAllRef}
              type="checkbox"
              className="activities-panel__checkbox"
              checked={allChecked}
              disabled={activities.length === 0}
              aria-label={allChecked ? t('activities.uncheck_all_label') : t('activities.check_all_label')}
              title={allChecked ? t('activities.uncheck_all') : t('activities.check_all')}
              onChange={() => (allChecked || someChecked ? onClear() : onSelectAll())}
            />
            <button
              type="button"
              className="activities-panel__invert"
              disabled={activities.length === 0}
              onClick={onInvertSelection}
              aria-label={t('activities.invert')}
              title={t('activities.invert_title')}
            >
              {/* A checkbox-sized square split on the diagonal, one half filled — reads as a
                  sibling of the select-all checkbox beside it rather than a separate text chip.
                  Lucide has no such glyph, so it's drawn to Lucide's own geometry (its `square`:
                  24-unit grid, 2-unit stroke, rx 2) to stay one family with the rest. */}
              <svg
                viewBox="0 0 24 24"
                width="16"
                height="16"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinejoin="round"
                aria-hidden="true"
                focusable="false"
              >
                <rect x="3" y="3" width="18" height="18" rx="2" />
                <path d="M21 3v16a2 2 0 0 1-2 2H3Z" fill="currentColor" />
              </svg>
            </button>

            <span className="activities-panel__toolbar-spacer" aria-hidden="true" />

            <button
              type="button"
              className="activities-panel__visibility"
              disabled={!hasTarget}
              onClick={onToggleGroupVisibility}
              aria-label={visibilityTitle}
              title={visibilityTitle}
            >
              {groupHasHidden ? <EyeOff size={16} /> : <Eye size={16} />}
            </button>
            <button
              type="button"
              className="activities-panel__edit"
              disabled={readOnly || !hasTarget}
              onClick={() => onEdit(targetActivities)}
              aria-label={editTitle}
              title={readOnly ? t('activities.demo_edit_activities') : editTitle}
            >
              <Pencil size={16} />
            </button>
            <AddToStoryMenu
              readOnly={readOnly}
              target={targetActivities}
              targetName={targetName}
              summary={groupSummary}
              onCreated={onStoryCreated}
              onAdded={onStoryAdded}
            />
            <button
              type="button"
              className="activities-panel__delete"
              disabled={readOnly || !hasTarget || groupHasPending}
              onClick={() => setDeletingGroup(true)}
              aria-label={deleteTitle}
              title={readOnly ? t('activities.demo_delete') : deleteTitle}
            >
              <Trash2 size={16} />
            </button>
            <span className="activities-panel__toolbar-divider" aria-hidden="true" />
            <button
              type="button"
              className="activities-panel__focus"
              disabled={!hasTarget}
              onClick={onShowSelected}
              aria-label={focusTitle}
              title={focusTitle}
            >
              <Focus size={16} />
            </button>
          </div>

          {/* A click on the list's own empty space (or one of its notes) — not on a row —
              clears the selection, the panel's counterpart to clicking away from every track
              on the map. Rows are <li>s filling the list's width, so a click on one never
              reaches here as the target. */}
          <ul
            className="activities-panel__list"
            data-testid="activities-list"
            ref={listRef}
            onClick={(e) => {
              const target = e.target as HTMLElement;
              if (target === e.currentTarget || target.classList.contains('activities-panel__note')) onClearFocus();
            }}
          >
            {activities.map((activity) => (
              <ActivityRow
                key={activity.id}
                activity={activity}
                system={system}
                focused={focusedId === activity.id}
                hovered={hoveredId === activity.id}
                hidden={hiddenIds.has(activity.id)}
                checkbox={{ checked: checked.has(activity.id), onToggle: () => onToggle(activity.id) }}
                onFocus={() => onFocus(activity.id)}
                onHover={onHoverActivity}
              />
            ))}
            {loading && <li className="activities-panel__note">{t('common.loading')}</li>}
            {error && !loading && (
              <li className="activities-panel__note activities-panel__note--error">{error}</li>
            )}
            {!loading && !error && activities.length === 0 && (
              <li className="activities-panel__note">{t('activities.none_match')}</li>
            )}
          </ul>

          {/* Just the summary now — the fly-to-fit action it used to hold moved to the toolbar's
              own accent-tinted "Focus checked group on the map" icon above. */}
          <div className="activities-panel__footer">
            <span className="activities-panel__footer-summary">
              {t('activities.footer', { n: targetActivities.length, distance: formatTotalDistance(targetMeters, system) })}
            </span>
          </div>

          {/* FR-3.7's "not yet built" gap: something synced can be absent from the list above for
              two different reasons — it failed, or cross-source dedup already had it from
              somewhere else — and only the second is not a fault. Hidden entirely when there's
              nothing to say, the same as Android's own duplicatesHeading. */}
          {(duplicates.length > 0 || duplicatesError) && (
            <div className="activities-panel__duplicates" ref={duplicatesRef}>
              <button
                type="button"
                className="activities-panel__duplicates-toggle"
                aria-expanded={duplicatesOpen}
                onClick={() => setDuplicatesOpen((open) => !open)}
              >
                {duplicatesError
                  ? t('activities.duplicates_failed')
                  : tn('activities.duplicates_found', duplicates.length)}
                <span className="activities-panel__duplicates-chevron" aria-hidden="true">
                  {duplicatesOpen ? <ChevronDown size={14} /> : <ChevronUp size={14} />}
                </span>
              </button>
              {duplicatesOpen && !duplicatesError && (
                <ul className="activities-panel__duplicates-list" data-testid="activities-duplicates-list">
                  {duplicates.map((d) => (
                    <li key={d.id} className="activities-panel__duplicates-row">
                      {formatStartedAt(d.startedAt)} · {formatActivityType(d.activityType)}
                      {d.distanceMeters !== null && ` · ${formatDistance(d.distanceMeters, system)}`}
                      <br />
                      {t('activities.duplicate_from', { source: formatIngestSource(d.source), kept: formatIngestSource(d.supersededBy.source) })}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </>
      )}



      {deletingGroup && (
        <ConfirmDialog
          title={targetActivities.length === 1 ? t('activities.delete_one_title') : t('activities.delete_title')}
          message={
            targetActivities.length === 1
              ? t('activities.delete_one_body', { group: groupSummary })
              : t('activities.delete_many_body', { group: groupSummary })
          }
          confirmLabel={t('activities.delete_confirm')}
          busyLabel={t('common.deleting')}
          onConfirm={async () => {
            // Sequential, not Promise.all: N concurrent DELETEs against the same account's
            // fog_tiles rows would race each other's dirty-mark-and-render trigger for no
            // benefit — a checked group is a handful of rows a user selected by hand, not a
            // bulk-import scale operation, so there's no latency reason to parallelize this.
            const ids = targetActivities.map((a) => a.id);
            for (const id of ids) {
              await deleteActivity(id);
            }
            onActivitiesDeleted(ids);
          }}
          onClose={() => setDeletingGroup(false)}
        />
      )}
    </div>
  );
}
