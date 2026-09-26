import { useCallback, useEffect, useRef, useState } from 'react';
import { ChevronDown, ChevronUp, Eye, EyeOff, Focus, Pencil, Trash2, Waypoints } from 'lucide-react';
import { deleteActivity, type Activity, type ActivityTotals, type DuplicateActivity } from '../api';
import { ConfirmDialog } from './ConfirmDialog';
import { DistanceFilter } from './DistanceFilter';
import { EditActivityDialog } from './EditActivityDialog';
import { SyncTab } from './SyncTab';
import type { DistanceRange, TypeFacet } from './activityFacets';
import {
  formatActivityType,
  formatDistance,
  formatDuration,
  formatIngestSource,
  formatStartedAt,
  formatTotalDistance,
} from './format';
import { useUnitSystem } from './units';
import type { ImportsState } from './useImports';
import { lang, t, tn } from '../i18n';

/**
 * The left sidebar, with two tabs: **Activities** (the list, below) and **Sync** (SyncTab.tsx —
 * file upload and the import history, formerly the header's Import dropdown). The tab is local
 * UI state; the upload queue behind Sync is MapView's (useImports), so it keeps running while
 * the Activities tab is showing.
 *
 * The activity list — a permanent left sidebar (not a collapsible
 * dropdown, nor a paginated one: §4.7's list
 * endpoint no longer pages, so this renders every row the current date range matched, filtered
 * further by TYPE/DISTANCE). MapView owns the fetch, the range, and both filters' state; this
 * component is purely presentational plus its own dropdown-open/panel-resize local UI state.
 *
 * DistanceFilter.tsx renders standalone and always visible now, between the subtext line and
 * the toolbar below — no longer hidden behind the old "Filter" toggle button, which is gone.
 *
 * A header toolbar sits directly above the row list. Every single-item action (edit, delete,
 * hide) lives here now, not on the row — a row has only its checkbox and its text; there are
 * no more per-row icon buttons at all. Acting on one activity means checking just its own box
 * first, the same as acting on many: a master checkbox mirrors the checked group's state, a
 * Type dropdown (TYPE checkboxes plus an "All types" convenience row that clears every
 * exclusion; Distance is its own standalone control above, see above), then four icon actions
 * over the checked group — Show/hide, Edit (Type/Name/Description for exactly one checked
 * activity, Type only for more than one — see EditActivityDialog.tsx), Delete, and, set off by
 * a divider, Focus on map (fly-to-fit, moved here from the footer's old text button). The
 * footer keeps only the "N selected · X km" summary.
 *
 * Four interactions per row, all living in MapView (which holds the map instance they need;
 * this component owns only rendering and its own local UI state). Row-click focus and the
 * checkbox group are two fully independent mechanisms — reported live as wrongly coupled once,
 * when a row click also silently checked/unchecked boxes:
 *  - Hovering the row (anywhere on it) previews that activity's track — bold on the map,
 *    nothing else — for as long as the pointer stays there. Purely a preview: the camera
 *    never moves for it, and it clears the moment the pointer leaves.
 *  - Clicking the row's text sets it as the single row-click *focus* — bolds it, flies there,
 *    and replaces whichever row was focused before (only ever one row is "just clicked" at a
 *    time). Never touches any checkbox.
 *  - The checkbox instead *adds or removes* this one row from the checked group, for building
 *    up a multi-row selection — every checked row bolds, and MapView's debounced auto-fly
 *    moves to fit the whole group. Never touches the row-click focus, in either direction.
 *  - The toolbar's Show/hide button toggles whether the checked group's tracks paint on the
 *    map at all, independent of all three of the above — there is no longer a per-row eye icon
 *    to toggle just one activity directly; a hidden activity's row dims in place instead.
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
  /** The row-click focus — at most one id, independent of `checked` above. */
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
  /** The row's own hover preview, `null` on leave — see the component doc comment above. */
  onHoverActivity: (id: string | null) => void;
  /** Empties the checked group — the header checkbox's "uncheck all" state. */
  onClear: () => void;
  /** Checks every currently-listed row — the header checkbox's "check all" state. */
  onSelectAll: () => void;
  /** Checks every unchecked listed row and unchecks every checked one — the toolbar's
   *  invert-selection icon beside the header checkbox. */
  onInvertSelection: () => void;
  /** Flies to fit the current checked group without changing it. */
  onShowSelected: () => void;
  /** Activities currently hidden from the map — dims the row (there's no per-row eye icon any
   *  more), and what the header toolbar's "Group visible" button toggles in bulk over
   *  `checked`. */
  hiddenIds: Set<string>;
  /** The header toolbar's "Group visible": if any checked activity is currently hidden, show
   *  the whole checked group; otherwise hide the whole group. See MapView's
   *  toggleGroupVisibility for the exact rule. */
  onToggleGroupVisibility: () => void;
  /** §4.7.4: a row's edit dialog saved successfully — reload the list so the renamed type/
   *  description (and the TYPE filter chip it may now belong to) reflect it immediately. */
  onActivityUpdated: () => void;
  /** §4.7.5's "Delete group" — the only delete entry point now (a single activity is deleted
   *  by checking just its own box first, then this same button), so always an array even for
   *  one id. Unlike onActivityUpdated, this also has to drop every deleted id from
   *  `checked`/`hiddenIds`/`focusedId` and refresh the map's track layer and totals/histogram
   *  (deleting changes distance/duration, editing never does) — MapView's own
   *  handleActivitiesDeleted does more than a plain reload. */
  onActivitiesDeleted: (ids: string[]) => void;
  /** §4.7.7's Edit track — the toolbar action over exactly one checked activity. MapView owns
   *  the session itself (hiding the other tracks, the flight, the editor window). */
  onEditTrack: (activity: Activity) => void;
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
}

type PanelTab = 'activities' | 'sync';

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
  focusedId,
  hoveredId,
  onToggle,
  onFocus,
  onHoverActivity,
  onClear,
  onSelectAll,
  onInvertSelection,
  onShowSelected,
  hiddenIds,
  onToggleGroupVisibility,
  onActivityUpdated,
  onActivitiesDeleted,
  onEditTrack,
  duplicates,
  duplicatesError,
  imports,
  onViewOnMap,
}: ActivitiesPanelProps) {
  const [tab, setTab] = useState<PanelTab>('activities');
  const hasActiveFilters = excludedTypes.size > 0 || distanceFilter !== null;

  // The Type dropdown (Type + Distance, per the header toolbar redesign) — closed by
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

  // Scrolls the newly row-click-focused activity into view, centered — reported live as
  // having to hunt for the now-bolded row by eye after clicking a track on the map, since a
  // long list scrolled it out of view as often as not. Keyed on focusedId alone (not
  // `checked`): a row click always has exactly one target to center on, while a checkbox spree
  // building up a multi-row group has no single row to scroll to, and would otherwise jerk the
  // list around after every click. `data-activity-id` on the row below is what this looks up.
  const listRef = useRef<HTMLUListElement>(null);
  useEffect(() => {
    if (focusedId === null) return;
    const row = listRef.current?.querySelector(`[data-activity-id="${CSS.escape(focusedId)}"]`);
    row?.scrollIntoView({ block: 'center', behavior: 'smooth' });
  }, [focusedId]);

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

  // §4.7.4's edit dialog — at most one open at a time, but now over either one row (a row's
  // own text no longer has a pencil icon; there isn't one any more) or the whole checked
  // group (the toolbar's Edit-selected button), so this holds an array rather than a single
  // Activity. EditActivityDialog itself branches on its length: exactly one edits Type, Name,
  // and Description as before; more than one edits Type only. `facets` (already computed for
  // the TYPE filter chips) doubles as the dialog's <datalist> suggestions either way.
  const [editingActivities, setEditingActivities] = useState<Activity[] | null>(null);

  // §4.7.5's delete confirm dialog — the toolbar's "Delete group" is now the only delete
  // entry point (there's no per-row delete button any more; a single activity is deleted by
  // checking just its own box first), so this covers both the one-activity and many-activity
  // case uniformly. The confirm message below already branches on checkedActivities.length.
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

  // The footer summary describes the checked group specifically, not the row-click focus —
  // matching that the footer's own "Show selected" is about that group too.
  const checkedActivities = activities.filter((a) => checked.has(a.id));
  const checkedMeters = checkedActivities.reduce((sum, a) => sum + (a.distanceMeters ?? 0), 0);
  const system = useUnitSystem();

  // The header toolbar's master checkbox — checked once every currently-listed row is in
  // `checked`, indeterminate for a partial selection, unchecked for none. `indeterminate`
  // has no JSX prop (it's a DOM property, not an HTML attribute), hence the ref + effect.
  const selectAllRef = useRef<HTMLInputElement>(null);
  const allChecked = activities.length > 0 && checkedActivities.length === activities.length;
  const someChecked = checkedActivities.length > 0 && !allChecked;
  useEffect(() => {
    if (selectAllRef.current) selectAllRef.current.indeterminate = someChecked;
  }, [someChecked]);

  // §4.7.5's ConfirmDialog message needs the group's own summary — reuses formatStartedAt's
  // shortest form isn't meaningful for N activities, so this names the count and total
  // distance instead, the same two numbers the footer summary already shows.
  const groupSummary = t('activities.group_summary', {
    activities: tn('activities.count', checkedActivities.length),
    distance: formatTotalDistance(checkedMeters, system),
  });

  // Group visible's own icon mirrors the row-level eye icon's open/closed convention: closed
  // (about to reveal) once any checked activity is currently hidden, open otherwise — matching
  // MapView's toggleGroupVisibility rule (show the whole group the moment any of it is hidden).
  const groupHasHidden = checkedActivities.some((a) => hiddenIds.has(a.id));

  // Edit track works on one activity's points, so it needs exactly one checked row — and one
  // with a track to edit that isn't already mid-reprocess.
  const editTrackTarget = checkedActivities.length === 1 ? checkedActivities[0]! : null;
  const editTrackReason = readOnly
    ? t('activities.demo_edit_tracks')
    : editTrackTarget === null
      ? t('activities.edit_track_check_one')
      : editTrackTarget.pending
        ? t('activities.edit_track_processing')
        : editTrackTarget.bbox === null
          ? t('activities.no_track')
          : null;
  // Pending rows can't be edited or deleted until their reprocess lands — the job would
  // otherwise race the edit or delete for the same row.
  const groupHasPending = checkedActivities.some((a) => a.pending);

  return (
    <div
      className={`activities-panel${sheetExpanded ? ' activities-panel--sheet-expanded' : ''}`}
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
      <div className="activities-panel__head" role="tablist" aria-label={t('activities.panel')}>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'activities'}
          className="activities-panel__tab"
          data-testid="activities-panel-tab-activities"
          onClick={() => setTab('activities')}
        >
          <span className="activities-panel__heading-text">{t('activities.tab')}</span>
          <span className="activities-panel__badge">{activities.length.toLocaleString(lang)}</span>
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
          : totals !== null
            ? t('activities.loaded', { distance: formatTotalDistance(totals.distanceMeters, system) })
            : t('common.loading')}
        <span className="activities-panel__sheet-chevron" aria-hidden="true">
          {sheetExpanded ? <ChevronDown size={20} /> : <ChevronUp size={20} />}
        </span>
      </button>

      {tab === 'sync' ? (
        <SyncTab
          imports={imports}
          readOnly={readOnly}
          onViewOnMap={(activityId, startedAt) => {
            setTab('activities');
            onViewOnMap(activityId, startedAt);
          }}
        />
      ) : (
        <>
          <DistanceFilter
            bounds={distanceBounds}
            value={distanceFilter}
            onChangeDistance={onChangeDistance}
            onReset={onResetFilters}
            hasActiveFilters={hasActiveFilters}
          />

          {/* The header toolbar — right above the row list. There are no more per-row action
              icons to stay column-aligned with (Visible/Edit/Delete all moved here, operating on
              the checked group), so this is a plain compact strip: select-all checkbox, the invert-selection icon, the Type
              dropdown, a spacer, the three group-action chips, a divider, then the one
              accent-tinted "focus the map on this group" action. */}
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

            <span className="activities-panel__toolbar-spacer" aria-hidden="true" />

            <button
              type="button"
              className="activities-panel__visibility"
              disabled={checked.size === 0}
              onClick={onToggleGroupVisibility}
              aria-label={groupHasHidden ? t('activities.show_group_label') : t('activities.hide_group_label')}
              title={groupHasHidden ? t('activities.show_group') : t('activities.hide_group')}
            >
              {groupHasHidden ? <EyeOff size={16} /> : <Eye size={16} />}
            </button>
            <button
              type="button"
              className="activities-panel__edit"
              disabled={readOnly || checked.size === 0}
              onClick={() => setEditingActivities(checkedActivities)}
              aria-label={t('activities.edit_group_label')}
              title={
                readOnly
                  ? t('activities.demo_edit_activities')
                  : checkedActivities.length === 1
                    ? t('activities.edit_one')
                    : t('activities.edit_many')
              }
            >
              <Pencil size={16} />
            </button>
            <button
              type="button"
              className="activities-panel__edit-track"
              disabled={editTrackReason !== null}
              onClick={() => editTrackTarget && onEditTrack(editTrackTarget)}
              aria-label={t('activities.edit_track_label')}
              title={editTrackReason ?? t('activities.edit_track')}
            >
              <Waypoints size={16} />
            </button>
            <button
              type="button"
              className="activities-panel__delete"
              disabled={readOnly || checked.size === 0 || groupHasPending}
              onClick={() => setDeletingGroup(true)}
              aria-label={t('activities.delete_group_label')}
              title={readOnly ? t('activities.demo_delete') : t('activities.delete_group')}
            >
              <Trash2 size={16} />
            </button>
            <span className="activities-panel__toolbar-divider" aria-hidden="true" />
            <button
              type="button"
              className="activities-panel__focus"
              disabled={checked.size === 0}
              onClick={onShowSelected}
              aria-label={t('activities.focus_group_label')}
              title={t('activities.focus_group')}
            >
              <Focus size={16} />
            </button>
          </div>

          <ul className="activities-panel__list" data-testid="activities-list" ref={listRef}>
            {activities.map((activity) => {
              const isChecked = checked.has(activity.id);
              const isFocused = focusedId === activity.id;
              const isHovered = hoveredId === activity.id;
              const isHidden = hiddenIds.has(activity.id);
              const isPending = activity.pending;
              // A user-entered name (§4.7's revised decision) leads; started_at is the fallback
              // for a row that has none — never the reverse, so an activity's date doesn't
              // disappear from the list just because it also has a name (shown in the meta line
              // below instead). Sorting itself is untouched either way: the list's order comes
              // entirely from the server's own `ORDER BY started_at DESC`, never from this label.
              const displayName = activity.name?.trim() || null;
              const label = displayName ?? formatStartedAt(activity.startedAt);
              const classes = ['activities-panel__row'];
              // Bold on the map is the union of checked and focused — the row highlight matches.
              if (isChecked || isFocused) classes.push('activities-panel__row--selected');
              if (isHidden) classes.push('activities-panel__row--hidden');
              if (isPending) classes.push('activities-panel__row--pending');
              return (
                <li
                  key={activity.id}
                  data-activity-id={activity.id}
                  className={classes.join(' ')}
                  // The description (§4.7.4) shows as a hover tooltip only — no second visible
                  // line, and undefined (not an empty string) when there is none, so a row with
                  // nothing written there gets no title attribute at all rather than an empty
                  // one a browser might still render as an inert tooltip.
                  title={activity.description ?? undefined}
                  onMouseEnter={() => onHoverActivity(activity.id)}
                  onMouseLeave={() => onHoverActivity(null)}
                >
                  <input
                    type="checkbox"
                    className="activities-panel__checkbox"
                    checked={isChecked}
                    // A pending row (§4.7.7) is disabled until its reprocess lands, except that an
                    // already-checked one can still be unchecked.
                    disabled={isPending && !isChecked}
                    aria-label={isChecked ? t('activities.row_uncheck', { label }) : t('activities.row_check', { label })}
                    onChange={() => onToggle(activity.id)}
                  />
                  <button
                    type="button"
                    className="activities-panel__text"
                    disabled={isPending}
                    aria-pressed={isFocused}
                    aria-label={t('activities.fly_to', { label })}
                    title={activity.bbox === null ? t('activities.no_track') : label}
                    onClick={() => onFocus(activity.id)}
                  >
                    <span className={`activities-panel__title${isHovered ? ' activities-panel__title--hovered' : ''}`}>{label}</span>
                    <span className="activities-panel__meta">
                      {/* The date moves down here, ahead of distance/duration, once a name has
                          taken its place as the title above — otherwise it's already the title
                          and repeating it here would be redundant. Type trails the line now
                          (there's no separate trailing column any more — single-item actions,
                          including visibility, all moved to the toolbar via check-then-toolbar,
                          so a bare row has nothing left to show but this text). */}
                      {displayName && `${formatStartedAt(activity.startedAt)} · `}
                      {formatDistance(activity.distanceMeters, system)} · {formatDuration(activity.durationSeconds)} ·{' '}
                      {formatActivityType(activity.activityType)}
                    </span>
                  </button>
                  {(isPending || isHidden) && (
                    // One right-aligned group, so the badges share one right edge whatever the
                    // text beside them does, and a row that's both stacks them there together.
                    <span className="activities-panel__badges">
                      {isPending && (
                        <span className="activities-panel__hidden-badge" title={t('activities.pending_title')}>
                          {t('activities.pending')}
                        </span>
                      )}
                      {isHidden && <span className="activities-panel__hidden-badge">{t('activities.hidden')}</span>}
                    </span>
                  )}
                </li>
              );
            })}
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
              {t('activities.footer', { n: checkedActivities.length, distance: formatTotalDistance(checkedMeters, system) })}
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

      {editingActivities && (
        <EditActivityDialog
          activities={editingActivities}
          knownTypes={facets}
          onClose={() => setEditingActivities(null)}
          onSaved={onActivityUpdated}
        />
      )}

      {deletingGroup && (
        <ConfirmDialog
          title={t('activities.delete_title')}
          message={
            checkedActivities.length === 1
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
            const ids = checkedActivities.map((a) => a.id);
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
