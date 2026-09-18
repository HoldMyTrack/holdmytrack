import { useCallback, useEffect, useRef, useState } from 'react';
import { deleteActivity, type Activity, type ActivityTotals } from '../api';
import { ConfirmDialog } from './ConfirmDialog';
import { DistanceFilter } from './DistanceFilter';
import { EditActivityDialog } from './EditActivityDialog';
import type { DistanceRange, TypeFacet } from './activityFacets';
import { formatActivityType, formatDistance, formatDuration, formatStartedAt, formatTotalDistance } from './format';
import { useUnitSystem } from './units';

/**
 * The activity list — a permanent left sidebar (not a collapsible
 * dropdown, nor a paginated one: §4.7's list
 * endpoint no longer pages, so this renders every row the current date range matched, filtered
 * further by TYPE/DISTANCE). MapView owns the fetch, the range, and both filters' state; this
 * component is purely presentational plus its own dropdown-open/panel-resize local UI state.
 *
 * DistanceFilter.tsx renders standalone and always visible now, between the subtext line and
 * the toolbar below — no longer hidden behind the old "Filter" toggle button, which is gone.
 *
 * A header toolbar (§4.7.6) sits directly above the row list, its columns aligned with each
 * row's own layout so it reads as that list's header: a master checkbox (above the row
 * checkbox column) mirroring the checked group's state, a Type dropdown (above the TYPE
 * column — TYPE checkboxes only now; Distance moved out, see above) and two icon-only bulk
 * actions, Group visible and Delete group (above the row's own Visible/Delete icon columns),
 * that operate on the whole checked group at once rather than one row at a time. The footer's
 * old Select all/Clear text button is gone, retired in the header checkbox's favor.
 *
 * Four interactions per row, all living in MapView (which holds the map instance they need;
 * this component owns only rendering and its own local UI state). Row-click focus and the
 * checkbox group are two fully independent mechanisms now — reported live as wrongly coupled
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
 *  - The eye icon toggles whether that activity's track paints on the map at all, independent
 *    of all three of the above.
 */
export interface ActivitiesPanelProps {
  /** A demo account (docs/ROADMAP.md's "Email verification + demo without real ingest") — the
   *  backend already rejects every mutation a demo session attempts (requireNotDemo), so this
   *  disables the controls that would otherwise error, with a `title` explaining why, rather
   *  than either hiding them (which would hide the feature existing at all, undercutting the
   *  demo's whole point of letting someone feel the app) or leaving them enabled to fail.
   *  Group visible and the per-row eye icon stay enabled either way — purely local UI state,
   *  never sent to the backend, so there's nothing for a demo account to be blocked from
   *  there. */
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
  /** Flies to fit the current checked group without changing it. */
  onShowSelected: () => void;
  /** Activities currently hidden from the map — the eye icon's own state, and what the
   *  header toolbar's "Group visible" button toggles in bulk over `checked`. */
  hiddenIds: Set<string>;
  onToggleVisibility: (id: string) => void;
  /** The header toolbar's "Group visible": if any checked activity is currently hidden, show
   *  the whole checked group; otherwise hide the whole group. See MapView's
   *  toggleGroupVisibility for the exact rule. */
  onToggleGroupVisibility: () => void;
  /** §4.7.4: a row's edit dialog saved successfully — reload the list so the renamed type/
   *  description (and the TYPE filter chip it may now belong to) reflect it immediately. */
  onActivityUpdated: () => void;
  /** §4.7.5: a row was deleted and purged — unlike onActivityUpdated, this also has to drop
   *  the id from `checked`/`hiddenIds`/`focusedId` if it was in any of them, and refresh the
   *  map's track layer and totals/histogram (deleting changes distance/duration, editing
   *  never does), so MapView's own onActivityDeleted does more than a plain reload. */
  onActivityDeleted: (id: string) => void;
  /** The header toolbar's "Delete group" — same full-purge semantics as onActivityDeleted,
   *  batched: one combined refresh instead of one per deleted activity. */
  onActivitiesDeleted: (ids: string[]) => void;
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
  focusedId,
  hoveredId,
  onToggle,
  onFocus,
  onHoverActivity,
  onClear,
  onSelectAll,
  onShowSelected,
  hiddenIds,
  onToggleVisibility,
  onToggleGroupVisibility,
  onActivityUpdated,
  onActivityDeleted,
  onActivitiesDeleted,
}: ActivitiesPanelProps) {
  const hasActiveFilters = excludedTypes.size > 0 || distanceFilter !== null;

  // The Type dropdown (Type + Distance, per the header toolbar redesign) — closed by
  // default, same "most sessions don't start by narrowing filters" reasoning the old "Filter"
  // toggle button had. Dismiss on outside click or Escape, the same hand-wired pattern
  // UploadPanel.tsx/UserMenu.tsx already use (not shared into a hook for a third call site).
  const [typeFilterOpen, setTypeFilterOpen] = useState(false);
  const typeFilterRef = useRef<HTMLDivElement>(null);

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

  // §4.7.4's edit-type-and-description dialog — at most one row's, since only one dialog is
  // ever open at a time. `facets` (already computed for the TYPE filter chips) doubles as the
  // dialog's <datalist> suggestions, so opening it needs no fetch of its own.
  const [editingActivity, setEditingActivity] = useState<Activity | null>(null);

  // §4.7.5's delete confirm dialog — same "at most one row's" shape as editingActivity above.
  const [deletingActivity, setDeletingActivity] = useState<Activity | null>(null);

  // The header toolbar's "Delete group" confirm — a separate boolean rather than reusing
  // deletingActivity above, since a bulk delete has no single Activity to point at.
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
  const groupSummary = `${checkedActivities.length} ${checkedActivities.length === 1 ? 'activity' : 'activities'} (${formatTotalDistance(checkedMeters, system)})`;

  // Group visible's own icon mirrors the row-level eye icon's open/closed convention: closed
  // (about to reveal) once any checked activity is currently hidden, open otherwise — matching
  // MapView's toggleGroupVisibility rule (show the whole group the moment any of it is hidden).
  const groupHasHidden = checkedActivities.some((a) => hiddenIds.has(a.id));

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
        aria-label="Resize the activities panel"
        onPointerDown={onResizePointerDown}
        onPointerMove={onResizePointerMove}
        onPointerUp={onResizePointerUp}
        onPointerCancel={onResizePointerUp}
      />
      <div className="activities-panel__head">
        <div className="activities-panel__heading">
          <span className="activities-panel__heading-text">Activities</span>
          <span className="activities-panel__badge">{activities.length.toLocaleString()}</span>
        </div>
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
        {totals !== null ? `${formatTotalDistance(totals.distanceMeters, system)} loaded` : 'Loading…'}
        <span className="activities-panel__sheet-chevron" aria-hidden="true">
          {sheetExpanded ? '▾' : '▴'}
        </span>
      </button>

      <DistanceFilter
        bounds={distanceBounds}
        value={distanceFilter}
        onChangeDistance={onChangeDistance}
        onReset={onResetFilters}
        hasActiveFilters={hasActiveFilters}
      />

      {/* The header toolbar — right above the row list, its columns aligned with each row's
          own layout (checkbox / icon / text / TYPE / Visible / Edit / Delete) so it reads as
          that list's own header rather than a floating control strip. Group visible and
          Delete group are icon-only, reusing the exact per-row Visible/Delete button classes
          for a pixel-identical look — Edit has no bulk equivalent, so that column is a bare
          spacer, matching its row counterpart's width so Delete still lines up under Delete. */}
      <div className="activities-panel__toolbar">
        <input
          ref={selectAllRef}
          type="checkbox"
          className="activities-panel__checkbox"
          checked={allChecked}
          disabled={activities.length === 0}
          aria-label={allChecked ? 'Uncheck all activities' : 'Check all activities'}
          title={allChecked ? 'Uncheck all' : 'Check all'}
          onChange={() => (allChecked || someChecked ? onClear() : onSelectAll())}
        />
        <span className="activities-panel__toolbar-spacer activities-panel__toolbar-spacer--icon" aria-hidden="true" />
        <span className="activities-panel__toolbar-spacer activities-panel__toolbar-spacer--text" aria-hidden="true" />

        <div className="activities-panel__type-dropdown" ref={typeFilterRef}>
          <button
            type="button"
            className="activities-panel__type-trigger"
            aria-haspopup="true"
            aria-expanded={typeFilterOpen}
            onClick={() => setTypeFilterOpen((open) => !open)}
          >
            Type
            {excludedTypes.size > 0 && <span className="activities-panel__type-trigger-dot" aria-hidden="true" />}
            <span className="activities-panel__type-trigger-caret" aria-hidden="true">
              ▾
            </span>
          </button>
          {typeFilterOpen && (
            <div className="activities-panel__type-panel" role="dialog" aria-label="Filter by type">
              {facets.length === 0 ? (
                <p className="activities-panel__type-panel-empty">No activities to filter yet.</p>
              ) : (
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
              )}
            </div>
          )}
        </div>

        <button
          type="button"
          className="activities-panel__visibility"
          disabled={checked.size === 0}
          onClick={onToggleGroupVisibility}
          aria-label={groupHasHidden ? 'Show every checked activity on the map' : 'Hide every checked activity from the map'}
          title={groupHasHidden ? 'Show checked group' : 'Hide checked group'}
        >
          <EyeIcon open={!groupHasHidden} />
        </button>
        <span className="activities-panel__toolbar-spacer activities-panel__toolbar-spacer--edit" aria-hidden="true" />
        <button
          type="button"
          className="activities-panel__delete"
          disabled={readOnly || checked.size === 0}
          onClick={() => setDeletingGroup(true)}
          aria-label="Delete every checked activity"
          title={readOnly ? 'Not available for demo accounts — create an account to delete activities' : 'Delete checked group'}
        >
          <TrashIcon />
        </button>
      </div>

      <ul className="activities-panel__list" data-testid="activities-list">
        {activities.map((activity) => {
          const isChecked = checked.has(activity.id);
          const isFocused = focusedId === activity.id;
          const isHovered = hoveredId === activity.id;
          const isHidden = hiddenIds.has(activity.id);
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
          return (
            <li
              key={activity.id}
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
                aria-label={isChecked ? `Remove ${label} from selection` : `Add ${label} to selection`}
                onChange={() => onToggle(activity.id)}
              />
              <span className="activities-panel__icon" aria-hidden="true">
                <RowIcon />
              </span>
              <button
                type="button"
                className="activities-panel__text"
                aria-pressed={isFocused}
                aria-label={`Fly to ${label}`}
                title={activity.bbox === null ? 'No track recorded for this activity' : label}
                onClick={() => onFocus(activity.id)}
              >
                <span className={`activities-panel__title${isHovered ? ' activities-panel__title--hovered' : ''}`}>{label}</span>
                <span className="activities-panel__meta">
                  {/* The date moves down here, ahead of distance/duration, once a name has
                      taken its place as the title above — otherwise it's already the title
                      and repeating it here would be redundant. */}
                  {displayName && `${formatStartedAt(activity.startedAt)} · `}
                  {formatDistance(activity.distanceMeters, system)} · {formatDuration(activity.durationSeconds)}
                </span>
              </button>
              <span className="activities-panel__type">{formatActivityType(activity.activityType)}</span>
              {/* Order is Visible, Edit, Delete — a direct product choice, not alphabetical
                  or age-of-feature order. */}
              <button
                type="button"
                className="activities-panel__visibility"
                aria-pressed={!isHidden}
                aria-label={isHidden ? `Show ${label} on the map` : `Hide ${label} on the map`}
                title={isHidden ? 'Hidden — click to show on the map' : 'Visible — click to hide from the map'}
                onClick={() => onToggleVisibility(activity.id)}
              >
                <EyeIcon open={!isHidden} />
              </button>
              <button
                type="button"
                className="activities-panel__edit"
                disabled={readOnly}
                aria-label={`Edit type, name, and description for ${label}`}
                title={readOnly ? 'Not available for demo accounts — create an account to edit activities' : 'Edit type, name, and description'}
                onClick={() => setEditingActivity(activity)}
              >
                <PencilIcon />
              </button>
              <button
                type="button"
                className="activities-panel__delete"
                disabled={readOnly}
                aria-label={`Delete ${label}`}
                title={readOnly ? 'Not available for demo accounts — create an account to delete activities' : 'Delete this activity'}
                onClick={() => setDeletingActivity(activity)}
              >
                <TrashIcon />
              </button>
            </li>
          );
        })}
        {loading && <li className="activities-panel__note">Loading…</li>}
        {error && !loading && (
          <li className="activities-panel__note activities-panel__note--error">{error}</li>
        )}
        {!loading && !error && activities.length === 0 && (
          <li className="activities-panel__note">No activities match the current filters.</li>
        )}
      </ul>

      <div className="activities-panel__footer">
        <span className="activities-panel__footer-summary">
          {checkedActivities.length} selected · {formatTotalDistance(checkedMeters, system)}
        </span>
        <div className="activities-panel__footer-actions">
          <button
            type="button"
            className="activities-panel__show-selected"
            onClick={onShowSelected}
            disabled={checked.size === 0}
          >
            Show selected
          </button>
        </div>
      </div>

      {editingActivity && (
        <EditActivityDialog
          activity={editingActivity}
          knownTypes={facets.map((f) => f.type)}
          onClose={() => setEditingActivity(null)}
          onSaved={onActivityUpdated}
        />
      )}

      {deletingActivity && (
        <ConfirmDialog
          title="Delete this activity?"
          message={`${formatStartedAt(deletingActivity.startedAt)} will be permanently deleted — its track, fog/heatmap coverage, and any performance records it contributed to. This can't be undone.`}
          confirmLabel="Delete"
          busyLabel="Deleting…"
          onConfirm={async () => {
            const id = deletingActivity.id;
            await deleteActivity(id);
            onActivityDeleted(id);
          }}
          onClose={() => setDeletingActivity(null)}
        />
      )}

      {deletingGroup && (
        <ConfirmDialog
          title="Delete this group?"
          message={
            checkedActivities.length === 1
              ? `${groupSummary} will be permanently deleted — its track, fog/heatmap coverage, and any performance records it contributed to. This can't be undone.`
              : `${groupSummary} will be permanently deleted — their tracks, fog/heatmap coverage, and any performance records they contributed to. This can't be undone.`
          }
          confirmLabel="Delete group"
          busyLabel="Deleting…"
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

/**
 * A generic per-row glyph. Deliberately one fixed shape: the elevation profile it evokes
 * would have to come from activity_streams (§3.4), which the list endpoint does not serve
 * and should not — drawing a *varying* fake profile per row would imply data that isn't there.
 */
function RowIcon() {
  return (
    <svg viewBox="0 0 16 16" width="16" height="16">
      <polyline
        points="1,13 5,9 9,10 12,4 15,6"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinejoin="round"
      />
    </svg>
  );
}

/** A simple pencil glyph for the §4.7.4 edit affordance — same viewBox/stroke weight as
 *  RowIcon/EyeIcon so all three read as one family of row-action icons. */
function PencilIcon() {
  return (
    <svg viewBox="0 0 16 16" width="14" height="14">
      <path
        d="M2 14 L2.6 11.2 L10.5 3.3 A1.4 1.4 0 0 1 12.5 3.3 L12.7 3.5 A1.4 1.4 0 0 1 12.7 5.5 L4.8 13.4 Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.3"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
      <line x1="9.3" y1="4.5" x2="11.5" y2="6.7" stroke="currentColor" strokeWidth="1.3" />
    </svg>
  );
}

/** Open eye (visible) or the same eye with a slash through it (hidden). */
function EyeIcon({ open }: { open: boolean }) {
  return (
    <svg viewBox="0 0 16 16" width="15" height="15">
      <path
        d="M1 8 C3 4, 6 2.5, 8 2.5 C10 2.5, 13 4, 15 8 C13 12, 10 13.5, 8 13.5 C6 13.5, 3 12, 1 8 Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.3"
        strokeLinejoin="round"
      />
      <circle cx="8" cy="8" r="2.1" fill="none" stroke="currentColor" strokeWidth="1.3" />
      {!open && <line x1="1.5" y1="13.5" x2="14.5" y2="2.5" stroke="currentColor" strokeWidth="1.3" />}
    </svg>
  );
}

/** A simple trash-can glyph for the §4.7.5 delete affordance — same viewBox/stroke weight as
 *  RowIcon/PencilIcon/EyeIcon so all four read as one family of row-action icons. */
function TrashIcon() {
  return (
    <svg viewBox="0 0 16 16" width="14" height="14">
      <path
        d="M3 4.5 H13 M6 4.5 V2.8 A0.8 0.8 0 0 1 6.8 2 H9.2 A0.8 0.8 0 0 1 10 2.8 V4.5 M4.2 4.5 L4.8 13.2 A1 1 0 0 0 5.8 14.1 H10.2 A1 1 0 0 0 11.2 13.2 L11.8 4.5"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.3"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <line x1="6.5" y1="7" x2="6.5" y2="11.5" stroke="currentColor" strokeWidth="1.1" strokeLinecap="round" />
      <line x1="9.5" y1="7" x2="9.5" y2="11.5" stroke="currentColor" strokeWidth="1.1" strokeLinecap="round" />
    </svg>
  );
}
