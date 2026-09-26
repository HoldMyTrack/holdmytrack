import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { flyToBBox, flyToView, unionBBox } from './bbox';
import { WORLD_VIEW } from './config';
import { countryView } from './countryView';
import { exportFramedImage } from './exportMap';
import type { ExportPreset } from './exportPresets';
import { ensureFogLayer } from './fog';
import { ensureHeatmapLayer } from './heatmap';
import { setMapMode, type MapMode } from './mapMode';
import { labelInsertionPoint } from './layers';
import { type Flavor } from './style';
import { clearTrackBands, ensureBandLayer, setTrackBands, type BandMetric } from './trackBands';
import {
  ensureTrackLayer,
  refreshTrackLayer,
  setHiddenTracks,
  setHoveredTrack,
  setSelectedTracks,
  setTrackInteractivityHandlers,
} from './tracks';
import { useCoverageRefresh } from './useCoverageRefresh';
import { useMapInstance } from './useMapInstance';
import { DEFAULT_FLAVOR, parseHash, replaceHash, type HashState, type ViewState } from './viewState';
import { getActivityTrackMetrics, type Activity, type ActivityTrackMetrics } from '../api';
import { useAuth } from '../auth/AuthContext';
import { distanceBounds, passesFilters, typeFacets, type DistanceRange } from '../ui/activityFacets';
import { ActivitiesPanel, type PanelTab } from '../ui/ActivitiesPanel';
import { ActivityHistogram } from '../ui/ActivityHistogram';
import { EditTrackPanel } from '../ui/EditTrackPanel';
import { ExportControl } from '../ui/ExportControl';
import { ExportFrame, type FrameGeometry } from '../ui/ExportFrame';
import { dayDiff, dayInZone, todayLocal } from '../ui/dateMath';
import type { DateRange } from '../ui/RangePicker';
import { TrackProfile } from '../ui/TrackProfile';
import { useUnitSystem } from '../ui/units';
import { useActivityDays } from '../ui/useActivityDays';
import { useActivityList } from '../ui/useActivityList';
import { useActivityTotals } from '../ui/useActivityTotals';
import { useDuplicates } from '../ui/useDuplicates';
import { useImports } from '../ui/useImports';
import { t } from '../i18n';

/** How long a checkbox-selection spree pauses before the map auto-flies to fit it (replacing
 *  the old explicit "Fit map" button — see IMPLEMENTATION.md §4.7) — long enough that ticking
 *  three boxes in a row flies once, at the end, not three times. */
const SELECTION_FLY_DEBOUNCE_MS = 300;

/** How often the list is re-read while an Edit track reprocess is pending — the job is one
 *  activity's worth of ingest, so a few seconds is the right order of magnitude. */
const EDIT_PENDING_POLL_MS = 2000;

export interface MapViewProps {
  /** Mount with the Activities panel on its Privacy tab — `/?private-locations`, the header's
   *  account menu and Settings' link to it (App.tsx). */
  initialPrivateLocationsOpen?: boolean;
}

/** The frame-and-capture export flow's own state — lives here, not inside `ExportFrame.tsx`,
 *  since it spans that component, the map control that opens it (`ExportControl.tsx`), and the
 *  live map instance capture needs directly. */
type ExportFlow =
  | { stage: 'idle' }
  | { stage: 'framing' | 'capturing'; preset: ExportPreset | 'custom'; geometry: FrameGeometry };

/** The frame's size, in CSS pixels, for a shape. Custom gets a generous 70% of the container
 *  so it's immediately visible and easy to grab. A preset keeps its declared aspect ratio,
 *  fitted inside `fit` (the current frame's size when switching shape, so the frame doesn't
 *  jump to a very different size) and never more than 80% of either container dimension. */
function frameSize(
  preset: ExportPreset | 'custom',
  container: { width: number; height: number },
  fit?: { widthPx: number; heightPx: number },
): { widthPx: number; heightPx: number } {
  if (preset === 'custom') {
    return fit ?? { widthPx: container.width * 0.7, heightPx: container.height * 0.7 };
  }
  const maxW = Math.min(fit?.widthPx ?? Infinity, container.width * 0.8);
  const maxH = Math.min(fit?.heightPx ?? Infinity, container.height * 0.8);
  const ratio = preset.widthPx / preset.heightPx;
  return maxW / maxH >= ratio ? { widthPx: maxH * ratio, heightPx: maxH } : { widthPx: maxW, heightPx: maxW / ratio };
}

export function MapView({ initialPrivateLocationsOpen = false }: MapViewProps) {
  // docs/SPEC.md FR-2.1–FR-2.3: a demo account is
  // read-only (no upload/sync, no edit/delete) — see ActivitiesPanel's own readOnly prop and
  // the importControl below. `'email' in user` is the same narrowing api.ts's SessionUser
  // already establishes as the way to tell a DemoUser from an AuthUser.
  const { user } = useAuth();
  const isDemo = !('email' in user);

  // Read once per mount. A previous session's camera position can't leak into a new one: every
  // sign-in, sign-up, demo start and sign-out is a server-rendered page that ends in a
  // redirect to a bare `/`, with no hash to inherit (IMPLEMENTATION.md §4.13). (While those
  // were React screens swapped in place, App.tsx had to clear the hash itself.)
  const [initial] = useState<{ hash: HashState; view: ViewState; flavor: Flavor }>(() => {
    const hash = parseHash(window.location.hash);
    return {
      hash,
      view: hash.view ?? { longitude: WORLD_VIEW.longitude, latitude: WORLD_VIEW.latitude, zoom: WORLD_VIEW.zoom },
      flavor: hash.flavor ?? DEFAULT_FLAVOR,
    };
  });

  const container = useRef<HTMLDivElement>(null);
  const [exportFlow, setExportFlow] = useState<ExportFlow>({ stage: 'idle' });
  const [exportError, setExportError] = useState<string | null>(null);
  // Normal is what already rendered before fog existed — it needed no new work to count
  // as a "mode" (IMPLEMENTATION.md §4.2.2).
  const [mapMode, setMapModeState] = useState<MapMode>('normal');

  const today = useMemo(() => todayLocal(), []);

  // Left-panel ActivitiesPanel/ActivityHistogram layout.
  // Selection lives here, not in ActivitiesPanel, since the map instance and the auto-fly
  // effects below both need it. The panel itself is not optional/toggleable.
  //
  // Two fully independent mechanisms, reported live as wrongly coupled when they shared one
  // Set: the checkbox builds a multi-row group (toggleActivityChecked, add/remove, additive —
  // checking a second row never unchecks the first), while clicking a row's own text/body is
  // a single "look at just this one" focus (focusActivity, replace) that does not touch any
  // checkbox. Both bold their row(s) on the map (unioned below), but each drives its own fly
  // target — the checkbox flies to fit the whole group (debounced), a row click flies to that
  // one activity alone (immediate) — and clearing/checking-all only ever touches the checked
  // group, never the focused row. Both start empty/null: there is no sensible default over a
  // real, unknown history.
  const [checkedActivityIds, setCheckedActivityIds] = useState<Set<string>>(() => new Set());
  // Bumped only by a checkbox click (toggleActivityChecked) — what the debounced group fly below
  // answers to, rather than every change to checkedActivityIds.
  const [groupFlyRequest, setGroupFlyRequest] = useState(0);
  const [focusedActivityId, setFocusedActivityId] = useState<string | null>(null);
  // The row (or map track) currently under the pointer — reported both ways (Activities
  // Panel's onMouseEnter/Leave, tracks.ts's onHover) into this one piece of state so a single
  // effect (setHoveredTrack below) is the only thing that ever touches the map's transient
  // "hover" feature-state. Purely a preview: never drives the camera, and clears back to
  // null the moment the pointer leaves either surface.
  const [hoveredActivityId, setHoveredActivityId] = useState<string | null>(null);
  // The eye icon's own state — independent of the selection above and of the TYPE/DISTANCE
  // filters below. Hiding a track is a purely visual "don't paint this one" instruction, not
  // a claim about what's selected or filtered, so it gets its own Set.
  const [hiddenActivityIds, setHiddenActivityIds] = useState<Set<string>>(() => new Set());

  // Colored zone segments (trackBands.ts) for whichever activity is currently row-click
  // focused — null whenever focusedActivityId is (see the fetch effect below). bandMetric
  // defaults to speed since every activity has it; heartrate is only offered once
  // trackMetrics.heartrateAvailable says every point actually has one.
  const [trackMetrics, setTrackMetrics] = useState<ActivityTrackMetrics | null>(null);
  const [bandMetric, setBandMetric] = useState<BandMetric>('speed');
  // Bumped when an Edit track reprocess finishes, so the focused activity's bands/profile are
  // refetched against its new points — the fetch below is otherwise keyed on the id alone.
  const [trackMetricsVersion, setTrackMetricsVersion] = useState(0);

  // The Edit track session (§4.7.7) — at most one activity at a time. While it's set, every
  // other track is hidden, the panel and timeline are inert, and EditTrackPanel owns the map.
  const [editingActivityId, setEditingActivityId] = useState<string | null>(null);
  const editingTrack = editingActivityId !== null;

  // The Activities panel's tab — here rather than in the panel so `/?private-locations` can open
  // onto Privacy (FR-8.1), and so the tab outlives the panel unmounting for Fog/Heatmap.
  const [panelTab, setPanelTab] = useState<PanelTab>(initialPrivateLocationsOpen ? 'private' : 'activities');

  // The list/summary filter — the highlighted band in the range picker. The picker's own pan
  // position is not here on purpose: it lives in useActivityDays and the two are independent,
  // so paging back through history never touches what's selected (see RangePicker.tsx).
  // selectedRange starts null: its real default (the 5 most recent activity-days — see the
  // effect below) needs the picker's first page of days, which hasn't loaded yet on first
  // render.
  const [selectedRange, setSelectedRangeState] = useState<DateRange | null>(null);

  // TYPE/DISTANCE facets — pure client-side filters over
  // whatever the current date range already fetched, per activityFacets.ts.
  const [excludedTypes, setExcludedTypes] = useState<Set<string>>(() => new Set());
  const [distanceFilter, setDistanceFilter] = useState<DistanceRange | null>(null);

  // The checkbox's own add/remove — see the state comment above for why this no longer
  // shares a Set with row-click focus.
  const toggleActivityChecked = useCallback((id: string) => {
    setCheckedActivityIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
    setGroupFlyRequest((n) => n + 1);
  }, []);
  // focusActivity itself (the row-click handler) is defined further down, alongside
  // clearSelection — both need fitToSelection/activities/mapHiddenIds, which aren't in scope
  // yet at this point in the component.
  //
  // The header toolbar's "Group visible" — there's no per-row eye icon any more (hiding a
  // single activity now goes through check-then-toolbar, the same as every other single-item
  // action), so this is the only visibility toggle left, always applied to the whole checked
  // group. The rule: if any checked activity is currently hidden, show the whole group
  // (removes every checked id from hiddenActivityIds); otherwise hide the whole group (adds
  // every checked id). Mirrors a typical bulk-checkbox toggle — one click reveals everything
  // in the group, click again to hide it — rather than per-row toggling each one
  // individually, which would leave the group in a mixed state no single click could
  // cleanly undo.
  const toggleGroupVisibility = useCallback(() => {
    setHiddenActivityIds((prev) => {
      const anyHidden = [...checkedActivityIds].some((id) => prev.has(id));
      const next = new Set(prev);
      for (const id of checkedActivityIds) {
        if (anyHidden) next.delete(id);
        else next.add(id);
      }
      return next;
    });
  }, [checkedActivityIds]);
  // A toggle, not a one-way remove: a row stays in the Type dropdown's list either way, so
  // there's always a way back to a type without needing "Reset filters" (which would also
  // throw away the distance filter).
  const toggleType = useCallback((type: string) => {
    setExcludedTypes((prev) => {
      const next = new Set(prev);
      if (next.has(type)) {
        next.delete(type);
      } else {
        next.add(type);
      }
      return next;
    });
  }, []);
  const resetActivityFilters = useCallback(() => {
    setExcludedTypes(new Set());
    setDistanceFilter(null);
  }, []);

  // Changing the date range changes which rows exist, so a selection, hidden-set or
  // TYPE/DISTANCE filter built against the old one may no longer make sense against the new
  // one — a stale distance band in particular could silently exclude everything if the new
  // range's own min/max don't overlap it. "Reset filters" is the explicit version of this;
  // this is the implicit one that fires on every drag-driven range change.
  //
  // Also the one place userChangedRangeRef is set — see the auto-default effect below for
  // why: once the user has deliberately picked a range, an upload landing outside it must
  // never silently override that choice the way it's allowed to before any real choice has
  // been made.
  const userChangedRangeRef = useRef(false);
  // Set here and consumed by the range fly further down: only a range the user picked moves
  // the camera, never the default re-deriving itself after an upload or sync.
  const flyToNextRangeRef = useRef(false);
  const changeSelectedRange = useCallback((next: DateRange) => {
    userChangedRangeRef.current = true;
    flyToNextRangeRef.current = true;
    setSelectedRangeState(next);
    setCheckedActivityIds(new Set());
    setFocusedActivityId(null);
    setHiddenActivityIds(new Set());
    setExcludedTypes(new Set());
    setDistanceFilter(null);
  }, []);

  const activityQuery = useMemo(
    () => (selectedRange ? { from: selectedRange.from, to: selectedRange.to } : {}),
    [selectedRange],
  );

  // Fetched once here rather than in each consumer: the header badge, the panel subtext and
  // the map's drawn tracks all have to agree on the same rows after an upload.
  const {
    activities,
    loading: activitiesLoading,
    error: activitiesError,
    reload: reloadActivities,
    loadedKey: activitiesLoadedKey,
  } = useActivityList(activityQuery);
  const { totals, reload: reloadTotals } = useActivityTotals(activityQuery);
  // Not scoped by activityQuery — FR-3.7's duplicate list, like the histogram, answers "what
  // happened to my whole history", not "what's in the currently selected date range".
  const duplicates = useDuplicates();
  // The range picker's own bars and pan position, independent of the selection above — it
  // pages by days-with-activity rather than by calendar window, see useActivityDays.
  const {
    visibleDays,
    earliest,
    ready: daysReady,
    pageStep,
    canPanEarlier,
    canPanLater,
    panBy,
    reload: reloadHistogram,
    generation: historyGeneration,
    setBarsPerView,
  } = useActivityDays();

  // Defaults to the 5 most recent activity-days, not all-time: an all-time default spans
  // every loaded bar on first paint, which leaves the selection band pinned to both edges
  // with nowhere to slide — the drag-to-move gesture below has nothing to demonstrate itself
  // with until the user already knows to shrink the range first. Five recent days starts the
  // band well short of either edge instead, so sliding it works the first time it's tried.
  // `visibleDays` is ascending, so its last 5 entries are the most recent 5.
  //
  // Deliberately *not* keyed on `visibleDays` (read fresh from the closure instead, without
  // being a dependency) and guarded on `userChangedRangeRef` rather than `selectedRange !==
  // null`: this used to settle the first time `selectedRange` left null and never run again,
  // which is exactly wrong for an account with zero history when it's first reached — the
  // fallback (`earliest ?? today`) degenerates to `{today, today}` with nothing to actually
  // show, and that "default" then permanently blocked this effect from ever reconsidering,
  // even once a real upload landed on some other day entirely. Every demo account starts at
  // exactly zero history, so this hit every single one of them on its very first upload —
  // reported live as "the Activities list doesn't pick up a demo's newly uploaded activity",
  // confirmed directly: the row appeared instantly after a manual reload (which re-derives
  // the default fresh against real data) but never on its own. `historyGeneration` (bumped
  // only by `reloadHistogram`, i.e. a real refetch — never by plain panning, which changes
  // `visibleDays` just as much but must never re-pick the selection out from under a user
  // who's simply browsing) is what lets this safely reconsider on new data without also
  // firing on every Earlier/Later click.
  useEffect(() => {
    if (userChangedRangeRef.current || !daysReady) return;
    const recentDays = visibleDays.slice(-5);
    setSelectedRangeState({ from: recentDays[0]?.date ?? earliest ?? today, to: today });
    // visibleDays deliberately isn't a dependency — see the comment above.
  }, [daysReady, earliest, historyGeneration, today]);

  // The histogram header's own stats — deliberately not totals.count/distanceMeters, which
  // ActivitiesPanel's subtext already shows; repeating them in the histogram too would just
  // be the same two numbers twice. Computed from `activities` (the selectedRange fetch),
  // not from the picker's `visibleDays`, since that's the pan window and can show a
  // completely different stretch of history while a selection elsewhere stays put.
  const selectedRangeDays = useMemo(
    () => (selectedRange ? dayDiff(selectedRange.from, selectedRange.to) + 1 : 0),
    [selectedRange],
  );
  const selectedActiveDays = useMemo(
    () => new Set(activities.map((a) => a.startedAt.slice(0, 10))).size,
    [activities],
  );

  // Rows with a reprocess still pending — an Edit track (§4.7.7) or a Private location change
  // — read by the polling and completion effects further down, by the bands effect, and by
  // mapHiddenIds: a Pending track isn't drawn (FR-5.15).
  const pendingIds = useMemo(() => activities.filter((a) => a.pending).map((a) => a.id), [activities]);

  const activityDistanceBounds = useMemo(() => distanceBounds(activities), [activities]);
  const facets = useMemo(() => typeFacets(activities, distanceFilter), [activities, distanceFilter]);
  const filteredActivities = useMemo(
    () => activities.filter((a) => passesFilters(a, excludedTypes, distanceFilter)),
    [activities, excludedTypes, distanceFilter],
  );
  // Union of "filtered out by TYPE/DISTANCE", "hidden by its own eye icon" and "Pending" — all
  // answer the same question for the map (don't paint this track, don't fly to it), so all
  // flow into one filter. The tracks tile already leaves a Pending activity out server-side;
  // this hides it the moment the list shows it Pending, before the layer's refetch lands.
  // Pending isn't added to hiddenActivityIds itself, so the user's own Visibility choice
  // survives the reprocess untouched.
  const mapHiddenIds = useMemo(() => {
    const filteredOut = activities.filter((a) => !passesFilters(a, excludedTypes, distanceFilter)).map((a) => a.id);
    return new Set([...filteredOut, ...hiddenActivityIds, ...pendingIds]);
  }, [activities, excludedTypes, distanceFilter, hiddenActivityIds, pendingIds]);

  const unitSystem = useUnitSystem();
  const map = useMapInstance({
    container,
    initialView: initial.view,
    initialFlavor: initial.flavor,
    scaleUnit: unitSystem,
  });

  const handleExportOpen = useCallback(() => {
    if (!map || !container.current) return;
    const center = map.getCenter();
    const size = frameSize('custom', { width: container.current.clientWidth, height: container.current.clientHeight });
    setExportError(null);
    // The header button toggles: pressing Export again while framing puts the frame away
    // (but not mid-capture — that finishes or fails first).
    setExportFlow((flow) => {
      if (flow.stage === 'framing') return { stage: 'idle' };
      if (flow.stage === 'capturing') return flow;
      return { stage: 'framing', preset: 'custom', geometry: { center: { lng: center.lng, lat: center.lat }, ...size } };
    });
  }, [map]);

  const handleExportPresetChange = useCallback((preset: ExportPreset | 'custom') => {
    const el = container.current;
    if (!el) return;
    setExportFlow((flow) =>
      flow.stage === 'framing'
        ? {
            stage: 'framing',
            preset,
            geometry: { center: flow.geometry.center, ...frameSize(preset, { width: el.clientWidth, height: el.clientHeight }, flow.geometry) },
          }
        : flow,
    );
  }, []);

  const handleExportCapture = useCallback(async () => {
    if (exportFlow.stage !== 'framing' || !map) return;
    const { preset, geometry } = exportFlow;
    setExportFlow({ stage: 'capturing', preset, geometry });
    setExportError(null);
    try {
      const blob = await exportFramedImage(
        map,
        { flavor: initial.flavor, mode: mapMode, activityQuery, hiddenIds: [...mapHiddenIds] },
        {
          ...geometry,
          ...(preset !== 'custom' && { target: { widthPx: preset.widthPx, heightPx: preset.heightPx } }),
        },
      );
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `holdmytrack-${new Date().toISOString().slice(0, 10)}.png`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      setExportFlow({ stage: 'idle' });
    } catch (err) {
      // The cause is a developer's detail (a canvas or render timeout), not something to act on.
      console.error('export failed', err);
      setExportError(t('export.failed'));
      // Stays in 'framing', not 'idle' — a failed capture (e.g. a network timeout) shouldn't
      // discard the frame the user just positioned, forcing them to redo it from scratch.
      setExportFlow((flow) => (flow.stage === 'capturing' ? { stage: 'framing', preset: flow.preset, geometry: flow.geometry } : flow));
    }
  }, [exportFlow, map, initial.flavor, mapMode, activityQuery, mapHiddenIds]);

  const handleExportCancel = useCallback(() => {
    setExportFlow({ stage: 'idle' });
    setExportError(null);
  }, []);

  // The flight over the combined bounds of every selected row — a single selected row's
  // "combined bounds" is just its own bbox, so this covers both the one-row and multi-row case
  // without a separate immediate-fly path for the former.
  const fitToSelection = useCallback(
    (selection: Activity[]) => {
      if (!map) return;
      const flyBounds = unionBBox(selection.flatMap((a) => (a.bbox ? [a.bbox] : [])));
      if (flyBounds) flyToBBox(map, flyBounds);
    },
    [map],
  );

  // Fog/Heatmap show coverage no filter narrows any more (all-time for Fog, the rolling
  // window above for Heatmap) — entering either from Normal has to clear the checked/focused
  // selection (neither mode can select or focus a single activity), then restore it exactly on
  // the way back to Normal. Only that one transition does either: toggling directly between Fog
  // and Heatmap touches neither, since Normal's selection state was never disturbed to begin
  // with. The camera is deliberately left alone on every mode change — an auto-fly here was
  // reported live as disorienting (switching modes to glance at coverage shouldn't also yank
  // the view to some computed bbox), so a mode switch now only ever changes what's drawn, never
  // where the camera is pointed; the user's own pan/zoom controls stay the only way to move it.
  const modeSnapshotRef = useRef<{ checked: Set<string>; focused: string | null } | null>(null);
  const changeMapMode = useCallback(
    (next: MapMode) => {
      if (next === mapMode) return;
      if (mapMode === 'normal') {
        modeSnapshotRef.current = { checked: checkedActivityIds, focused: focusedActivityId };
        setCheckedActivityIds(new Set());
        setFocusedActivityId(null);
      } else if (next === 'normal') {
        const snapshot = modeSnapshotRef.current;
        modeSnapshotRef.current = null;
        if (snapshot) {
          setCheckedActivityIds(snapshot.checked);
          setFocusedActivityId(snapshot.focused);
        }
      }
      setMapModeState(next);
    },
    [mapMode, checkedActivityIds, focusedActivityId],
  );

  // Auto-fly after a checkbox click ("FLYING TO 3 SELECTED"), replacing the old explicit "Fit
  // map to selection" button — see IMPLEMENTATION.md §4.7. Debounced so a multi-checkbox spree
  // flies once, after the user pauses, not once per checkbox.
  //
  // Keyed on groupFlyRequest, which only a checkbox click bumps — not on checkedActivityIds
  // itself. Keyed on the state, it also fired whenever the group changed for any other reason:
  // coming back to Normal from Fog/Heatmap (which restores the saved group) flew the camera back
  // to it after the user had panned away — reported live — and so did a list refresh or hiding a
  // checked track. Select all, Invert selection and Clear fly on their own, immediately, and the
  // "Focus checked group" button re-flies on demand. The group, list and hidden set are read
  // from a ref when the timer fires, so the fly always uses the current ones without the effect
  // re-running when they change.
  //
  // Excludes mapHiddenIds: checking a row is independent of its own eye icon (hiding its track),
  // so a hidden-but-checked activity is a real, reachable state — reported live as flying to
  // that activity's bbox anyway, which looks like flying to empty water since nothing is drawn
  // there. If every checked activity is hidden, unionBBox([]) is null and fitToSelection is a
  // no-op, same as an empty group.
  const groupFlyInputs = useRef({ checkedActivityIds, activities, mapHiddenIds });
  groupFlyInputs.current = { checkedActivityIds, activities, mapHiddenIds };
  useEffect(() => {
    if (groupFlyRequest === 0) return;
    const timer = window.setTimeout(() => {
      const { checkedActivityIds: ids, activities: all, mapHiddenIds: hidden } = groupFlyInputs.current;
      if (ids.size === 0) return;
      fitToSelection(all.filter((a) => ids.has(a.id) && !hidden.has(a.id)));
    }, SELECTION_FLY_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [groupFlyRequest, fitToSelection]);

  // Clicking a row's own text/body — a single "look at just this one" focus, independent of
  // the checkbox group above (see the state comment near checkedActivityIds/focusedActivityId
  // for why these no longer share a Set). Immediate, not debounced: it's one deliberate click
  // that fully replaces the previous focus, not a spree of partial changes to coalesce — the
  // same reasoning clearSelection below already has for its own single-click case. Excludes a
  // currently-hidden target the same way the checked-group effect above does: fitToSelection
  // over an empty array (the target filtered out) is a no-op, not a fly to empty water.
  const focusActivity = useCallback(
    (id: string) => {
      setFocusedActivityId(id);
      const target = activities.filter((a) => a.id === id && !mapHiddenIds.has(a.id));
      fitToSelection(target);
    },
    [activities, mapHiddenIds, fitToSelection],
  );

  // ROADMAP.md's "View on map" item — SyncTab.tsx's per-row action, reusing focusActivity
  // above rather than inventing a second fly-to mechanism. The one thing a row click doesn't
  // already handle: the target activity may not be in the currently selected date range (an
  // old Takeout import, a Health Connect backfill), in which case focusActivity would silently
  // find nothing in `activities` and no-op. When that happens, this narrows the range to just
  // that activity's own day (changeSelectedRange, the same mechanism a manual single-day pick
  // already uses — FR-6.5) and defers the actual focus to the effect below, which fires once
  // that range's own refetch has actually landed and the id is really there — a two-step async
  // sequence, not a single call, since the range change and the fly both depend on a fetch
  // landing first. Also restores Normal mode first: Fog/Heatmap have no per-track focus
  // concept, and their own mode-switch effect already clears focus whenever entering either.
  const pendingFocusIdRef = useRef<string | null>(null);
  const viewActivityOnMap = useCallback(
    (activityId: string, startedAtIso: string) => {
      if (mapMode !== 'normal') changeMapMode('normal');
      const day = dayInZone(startedAtIso, user.timezone);
      if (selectedRange !== null && day >= selectedRange.from && day <= selectedRange.to) {
        focusActivity(activityId);
        return;
      }
      pendingFocusIdRef.current = activityId;
      changeSelectedRange({ from: day, to: day });
      // The focus below flies to the activity itself; the range fly would override it.
      flyToNextRangeRef.current = false;
    },
    [mapMode, changeMapMode, selectedRange, changeSelectedRange, focusActivity, user.timezone],
  );
  useEffect(() => {
    const pending = pendingFocusIdRef.current;
    if (pending === null) return;
    if (activities.some((a) => a.id === pending)) {
      pendingFocusIdRef.current = null;
      focusActivity(pending);
    }
  }, [activities, focusActivity]);

  // The footer's "Clear" button (shown once at least one row is checked): empties the checked
  // group (so every previously-bold-by-checkbox track drops that highlight — a focused row,
  // if any, is untouched) and flies out to fit everything currently drawn — the same "don't
  // fly to something not drawn" exclusion as the effects above, just over the whole list
  // instead of the group. Immediate, not debounced: a single deliberate click.
  const clearSelection = useCallback(() => {
    setCheckedActivityIds(new Set());
    const visible = activities.filter((a) => !mapHiddenIds.has(a.id));
    fitToSelection(visible);
  }, [activities, mapHiddenIds, fitToSelection]);

  // The footer's "Select all" button (shown instead of "Clear" once nothing is checked) —
  // checks every row the panel currently lists (already narrowed by TYPE/DISTANCE, per
  // filteredActivities below) and flies to fit them, the same immediate shape clearSelection
  // uses. Deliberately scoped to what's actually shown, not every activity in the date range
  // regardless of filters — checking rows the user can't see would be a surprise.
  const selectAll = useCallback(() => {
    const ids = filteredActivities.map((a) => a.id);
    setCheckedActivityIds(new Set(ids));
    const visible = filteredActivities.filter((a) => !mapHiddenIds.has(a.id));
    fitToSelection(visible);
  }, [filteredActivities, mapHiddenIds, fitToSelection]);

  // The toolbar's invert-selection icon — checks every listed row that isn't checked and unchecks
  // every one that is. Same scope as selectAll: only rows the panel currently lists, so a
  // checked row outside the current TYPE/DISTANCE filters is dropped rather than kept checked
  // out of sight. Flies to fit the new group, or — when inverting leaves nothing checked —
  // out to everything drawn, exactly as clearSelection would.
  const invertSelection = useCallback(() => {
    const inverted = filteredActivities.filter((a) => !checkedActivityIds.has(a.id));
    setCheckedActivityIds(new Set(inverted.map((a) => a.id)));
    const group = inverted.length > 0 ? inverted : activities;
    fitToSelection(group.filter((a) => !mapHiddenIds.has(a.id)));
  }, [filteredActivities, checkedActivityIds, activities, mapHiddenIds, fitToSelection]);

  // "Show selected" — no state change, just a manual re-trigger of the same fly-to-fit the
  // debounced checkbox effect above already computes, for after panning away from the group.
  const showSelected = useCallback(() => {
    const visible = activities.filter((a) => checkedActivityIds.has(a.id) && !mapHiddenIds.has(a.id));
    fitToSelection(visible);
  }, [activities, checkedActivityIds, mapHiddenIds, fitToSelection]);

  // Read inside the effect below without being one of its dependencies: the fly-to-fit it
  // does is keyed on the *range* changing, but the bounds it flies to still have to exclude
  // whatever those filters currently hide (Pending included), or the camera would zoom out to
  // include a track that isn't even being drawn.
  const mapHiddenIdsRef = useRef(mapHiddenIds);
  useEffect(() => {
    mapHiddenIdsRef.current = mapHiddenIds;
  }, [mapHiddenIds]);

  // Auto-fly when the blue band changes the *set* of activities, not just which rows the
  // panel lists — without this, narrowing the tracks layer to the new range (the effect
  // above) could leave the camera pointed at empty water if the previous view doesn't
  // overlap the new one at all. The very first time activities has anything in it is a
  // special case (docs/ROADMAP.md's "Fly to the most recent activity on first load"): a
  // saved/shared URL position (initial.hash.view) still wins, per FR-4.5, but otherwise this
  // flies to just the single most recent activity rather than the whole default selection —
  // an account with scattered recent history (e.g. one activity in another country yesterday,
  // one locally today) would otherwise fitBounds to a near-world view, which reads as broken
  // rather than just generic. After that, only a range the user picked (changeSelectedRange)
  // flies, to fit whatever's now actually visible.
  //
  // Nothing else moves the camera: not a reload of the same range (the Pending poll every 2s,
  // a finished upload or sync, a Private location change), and not a new range the default
  // re-derived because new data arrived (the auto-default effect above, until the user picks
  // one). Keyed on the query the list was fetched for, not on `activities`, which is a new
  // array on every reload — keyed on that, a batch of uploads or Pending rows flew the camera
  // back over and over (reported live).
  const hasFlownToActivitiesRef = useRef(false);
  const flownRangeKeyRef = useRef<string | null>(null);
  useEffect(() => {
    if (!map || activitiesLoadedKey === null) return;
    if (activitiesLoadedKey === flownRangeKeyRef.current) return;
    flownRangeKeyRef.current = activitiesLoadedKey;
    if (!hasFlownToActivitiesRef.current) {
      // The first list, even an empty one: an account with no history yet gets the fallback
      // view below, and its first upload doesn't fly either.
      hasFlownToActivitiesRef.current = true;
      const drawn = activities.filter((a) => a.bbox !== null && !a.pending);
      if (initial.hash.view == null && drawn.length > 0) {
        const mostRecent = drawn.reduce((latest, a) => (a.startedAt > latest.startedAt ? a : latest));
        fitToSelection([mostRecent]);
      }
      return;
    }
    if (!flyToNextRangeRef.current) return;
    flyToNextRangeRef.current = false;
    const visible = activities.filter((a) => !mapHiddenIdsRef.current.has(a.id));
    const flyBounds = unionBBox(visible.flatMap((a) => (a.bbox ? [a.bbox] : [])));
    if (flyBounds) flyToBBox(map, flyBounds);
  }, [map, activities, activitiesLoadedKey, fitToSelection]);

  // The counterpart for an account with genuinely zero history (docs/ROADMAP.md's same "Fly
  // to the most recent activity" item, its zero-history fallback): the effect above never
  // fires at all once `activities` stays empty, so this is a separate one-shot guarded the
  // same way. `earliest`/`daysReady` (useActivityDays), not `activities.length === 0`, is the
  // right "genuinely zero" signal — activities is scoped to selectedRange, which degenerates
  // to {today, today} for a brand-new account regardless of whether it truly has no history
  // (see the comment on the default-range effect above for the exact same trap). Falls back
  // to the account's Country setting, at that country's own view, if set; otherwise a fixed,
  // deliberately zoomed-out world view (WORLD_VIEW) — never geolocation (ROADMAP.md's existing
  // stance: no permission prompts here).
  const hasFlownToFallbackRef = useRef(false);
  useEffect(() => {
    if (!map || hasFlownToFallbackRef.current) return;
    if (initial.hash.view != null) return;
    if (activities.length > 0) return;
    if (!daysReady || earliest != null) return;
    hasFlownToFallbackRef.current = true;
    flyToView(map, countryView(user.country) ?? WORLD_VIEW);
  }, [map, activities, daysReady, earliest, user.country]);

  // Clicking a track directly on the map is the row-text "focus" behavior, not the checkbox's
  // — it bolds just that one track, replacing whichever was focused before, and flies to it,
  // the same as clicking its row's text would. Reported live as wanting this over the
  // checkbox-mirroring behavior it used to have (add/remove, no fly): there is no visible
  // checkbox to check on the map itself, so "acts like the row you'd click" reads as the more
  // natural match for a direct click on the track than "acts like the checkbox" does.
  // focusActivity is already exactly the right shape ((string) => void) for tracks.ts's
  // onSelect, so no wrapper is needed. Hover just forwards straight into the shared
  // hoveredActivityId state below. Clicking away from every track (onClickAway) clears focus
  // the same way — reported live as the missing counterpart to clicking a track — but leaves
  // the checked group alone, matching that the two never touch each other in either direction.
  //
  // While a track is being edited the tracks layer is hidden, so every click would read as a
  // click away — and in Delete point mode, every click is aimed at a point. The handlers go
  // quiet for the session instead.
  useEffect(() => {
    setTrackInteractivityHandlers(
      editingTrack
        ? { onSelect: () => {}, onHover: () => {}, onClickAway: () => {} }
        : { onSelect: focusActivity, onHover: setHoveredActivityId, onClickAway: () => setFocusedActivityId(null) },
    );
  }, [focusActivity, editingTrack]);

  // The single source of truth for which tracks are bold on the map: the union of the checked
  // group and the focused row, however each got that way — a row's checkbox for the former, a
  // row's own text or a direct click on the map (both focusActivity, via the handler just
  // above) for the latter. Both render identically bold; this is the one place that union is
  // actually computed.
  const boldedActivityIds = useMemo(() => {
    const ids = new Set(checkedActivityIds);
    if (focusedActivityId) ids.add(focusedActivityId);
    return ids;
  }, [checkedActivityIds, focusedActivityId]);
  useEffect(() => {
    if (map) setSelectedTracks(map, [...boldedActivityIds]);
  }, [map, boldedActivityIds]);

  // Same idea for the transient hover preview — one effect, one place that ever calls
  // setHoveredTrack, fed by both a row's onMouseEnter/Leave (ActivitiesPanel, via
  // hoveredActivityId directly) and the map's own hover (via the handler just above).
  useEffect(() => {
    if (map) setHoveredTrack(map, hoveredActivityId);
  }, [map, hoveredActivityId]);

  // The eye icon's and the TYPE/DISTANCE filters' combined effect on the map: a client-side
  // layer filter, re-applied whenever the hidden set changes.
  useEffect(() => {
    if (map) setHiddenTracks(map, [...mapHiddenIds]);
  }, [map, mapHiddenIds]);

  // Colored zone segments (below) key off the row-click focus directly — not the checked
  // group — since bands are inherently a "look at this one activity" view, the same territory
  // a row click already owns.
  //
  // Fetches per-vertex speed/heart-rate for the focused activity — null (not stale data from
  // whatever was focused before) the instant focus moves to a different row or clears.
  useEffect(() => {
    if (!focusedActivityId) {
      setTrackMetrics(null);
      return;
    }
    const controller = new AbortController();
    getActivityTrackMetrics(focusedActivityId, controller.signal)
      .then((m) => {
        setTrackMetrics(m);
        // Fall back to speed if the newly-focused activity doesn't have complete heart-rate
        // coverage — carrying a stale "heartrate" selection into an activity that can't show
        // it would otherwise silently render nothing.
        setBandMetric((prev) => (prev === 'heartrate' && !m.heartrateAvailable ? 'speed' : prev));
      })
      .catch(() => {
        if (!controller.signal.aborted) setTrackMetrics(null);
      });
    return () => controller.abort();
  }, [focusedActivityId, trackMetricsVersion]);

  // Draws (or clears) the colored zone segments themselves — separate from the fetch effect
  // above so switching the Pace/Heart rate toggle re-renders instantly from data already in
  // hand, no refetch. Also cleared when the focused activity itself is hidden (its eye icon,
  // or a TYPE/DISTANCE filter narrowing it out) — reported live as hiding a focused activity's
  // plain track (setHiddenTracks, below) while this layer, deliberately drawn *on top* of that
  // same track (trackBands.ts's own doc comment), kept painting it regardless, since this
  // effect never checked mapHiddenIds. Re-showing it draws the band again from data already in
  // hand, no refetch, same as the Pace/Heart rate toggle above.
  //
  // Nor while the focused activity has a track edit pending: the metrics in hand describe its
  // pre-edit points, and the bands are drawn wider than and on top of the track itself, so
  // they'd paint the old shape over the new one until the refetch lands.
  const focusedPending = useMemo(
    () => focusedActivityId !== null && pendingIds.includes(focusedActivityId),
    [focusedActivityId, pendingIds],
  );
  useEffect(() => {
    if (!map) return;
    if (trackMetrics && focusedActivityId !== null && !mapHiddenIds.has(focusedActivityId) && !focusedPending) {
      setTrackBands(map, trackMetrics.points, bandMetric);
    } else {
      clearTrackBands(map);
    }
  }, [map, trackMetrics, bandMetric, focusedActivityId, mapHiddenIds, focusedPending]);

  // The blue band's effect on the map: when the selected date range changes, the tracks
  // layer's own tile query has to change with it, or the map keeps showing activities
  // outside the range the Activities list has already moved on from. `ensureTrackLayer`
  // alone doesn't do this — it only seeds a *new* source's initial tiles, and by this point
  // the source already exists, so its guarded `addSource` block is a no-op. `refreshTrackLayer`
  // is what actually points the existing source at the new `from`/`to` and forces a refetch.
  useEffect(() => {
    if (map) refreshTrackLayer(map, activityQuery);
  }, [map, activityQuery]);

  // Unlike tracks, Fog/Heatmap have no refresh-on-change effect here: neither mode is scoped
  // by date/TYPE/DISTANCE/hidden-track state any more, so `ensureFogLayer`/`ensureHeatmapLayer`
  // (both called once in `reattachOverlays` below, with no query) never have anything to
  // refresh against.

  // Fog/Heatmap are re-rendered by a queued job after an upload or delete, so they can't be
  // refetched right away like the tracks layer — this waits for the account's coverage jobs
  // to drain, then refetches both (useCoverageRefresh.ts).
  const watchCoverage = useCoverageRefresh(map);

  // A finished upload is a new track on the map and a new row in every §4.7 response, so
  // refresh all four together rather than let the Sync tab's badge fall behind the geometry.
  const handleUploaded = useCallback(() => {
    if (map) refreshTrackLayer(map, activityQuery);
    reloadActivities();
    reloadTotals();
    reloadHistogram();
    // A finished upload/sync is also the one thing that can produce a new duplicate.
    duplicates.refresh();
    // Deletes come through here too (handleActivitiesDeleted), so this covers both.
    watchCoverage();
  }, [map, activityQuery, reloadActivities, reloadTotals, reloadHistogram, duplicates.refresh, watchCoverage]);

  // The Sync tab's upload queue and history — held here rather than in the tab, so an upload
  // and its polling survive the panel unmounting (Fog/Heatmap) or showing the other tab.
  const imports = useImports(handleUploaded);

  // §4.7.5/§4.7.6: one or more deleted activities need the exact same four-part refresh a
  // finished upload does (unlike editing type/description, deleting changes distance/duration
  // totals and the histogram too) — plus dropping every deleted id from any local selection
  // state that could otherwise still reference it. focusedActivityId is the one that actually
  // matters for correctness: left pointing at a now-deleted id, the colored-zone-segments/
  // pace-profile effect would keep trying to fetch track-metrics for an activity that no
  // longer exists. checked/hidden are cleared too for tidiness, though a dangling id there is
  // already harmless — activities filters itself out of both the moment reloadActivities()
  // lands. Batched rather than called once per id (the header toolbar's "Delete group" can
  // delete many at once) — one combined refresh, not N redundant ones.
  const handleActivitiesDeleted = useCallback(
    (ids: string[]) => {
      handleUploaded();
      const deleted = new Set(ids);
      setFocusedActivityId((current) => (current !== null && deleted.has(current) ? null : current));
      setCheckedActivityIds((current) => {
        if (![...deleted].some((id) => current.has(id))) return current;
        const next = new Set(current);
        for (const id of deleted) next.delete(id);
        return next;
      });
      setHiddenActivityIds((current) => {
        if (![...deleted].some((id) => current.has(id))) return current;
        const next = new Set(current);
        for (const id of deleted) next.delete(id);
        return next;
      });
    },
    [handleUploaded],
  );

  // §4.7.7's Edit track: the toolbar's action over exactly one checked activity. Flies there
  // the same way a row click does, then hands the map to EditTrackPanel until it closes.
  const startEditTrack = useCallback(
    (activity: Activity) => {
      setHoveredActivityId(null);
      setEditingActivityId(activity.id);
      fitToSelection([activity]);
    },
    [fitToSelection],
  );
  // Activities whose edit was applied from this tab and whose finished reprocess hasn't been
  // picked up yet — see the completion effect below for why seeing the row go Pending isn't
  // enough on its own.
  const awaitingEditIdsRef = useRef<Set<string>>(new Set());
  const closeEditTrack = useCallback(
    (applied: boolean) => {
      if (applied && editingActivityId !== null) awaitingEditIdsRef.current.add(editingActivityId);
      setEditingActivityId(null);
      // The row now reads Pending — the effects below poll until the reprocess lands.
      if (applied) reloadActivities();
    },
    [editingActivityId, reloadActivities],
  );
  // A saved or deleted Private location reprocesses every activity it could clip. The list
  // reload shows those rows Pending right away, and the Pending poll below refreshes the map
  // when they finish — but a batch that's done before the reload even returns is never seen
  // Pending at all, so the coverage watch refreshes everything once the server has drained.
  const handlePrivateLocationsChanged = useCallback(() => {
    reloadActivities();
    watchCoverage(() => {
      if (map) refreshTrackLayer(map, activityQuery);
      reloadActivities();
      reloadTotals();
      reloadHistogram();
      setTrackMetricsVersion((v) => v + 1);
    });
  }, [map, activityQuery, reloadActivities, reloadTotals, reloadHistogram, watchCoverage]);
  const editingActivity = useMemo(
    () => (editingActivityId === null ? null : (activities.find((a) => a.id === editingActivityId) ?? null)),
    [activities, editingActivityId],
  );
  // The session can't outlive its activity — deleted from another tab, say, and gone from the
  // next reload. Without this the map would stay locked with no editor left to close it.
  useEffect(() => {
    if (editingActivityId !== null && editingActivity === null && !activitiesLoading) setEditingActivityId(null);
  }, [editingActivityId, editingActivity, activitiesLoading]);

  // While any row is pending, re-read the list every few seconds. Keyed on `activities`
  // itself, so each landed reload schedules the next and polling stops by itself once nothing
  // is pending any more.
  useEffect(() => {
    if (pendingIds.length === 0) return;
    const timer = window.setTimeout(reloadActivities, EDIT_PENDING_POLL_MS);
    return () => window.clearTimeout(timer);
  }, [pendingIds, reloadActivities]);

  // A row that stops being pending has new points, distance and duration: the drawn track,
  // the totals, the timeline bars and (if it's focused) its bands all need the new version.
  // None of it moves the camera — the row simply reappears where it is (FR-5.15).
  //
  // Fog/Heatmap go through the coverage watch rather than refetching here: the server clears
  // Pending just before its last render (ProcessTrackEdit), so the badge clearing doesn't yet
  // mean the tiles are current — the job finishing does. A row that *starts* being pending
  // starts the same watch, which also picks up the render that drops it from coverage.
  //
  // Two ways to tell a reprocess finished. A row seen Pending and now not is the obvious one,
  // but it misses the common case: the job is often done within milliseconds of being
  // claimed, before the list reload Apply triggers has even come back, so that row is never
  // seen Pending at all — found live, with the focused activity's bands still drawing the
  // pre-edit shape over the edited track. So an activity applied from this tab also counts as
  // finished the first time a list fetched after the Apply shows it not pending. Keyed on
  // `pendingIds`, a new array per fetched list, and the list fetch Apply starts aborts any
  // earlier one, so every list this sees after the Apply really was fetched after it.
  const previousPendingRef = useRef<ReadonlySet<string>>(new Set());
  const lastPendingIdsRef = useRef<string[] | null>(null);
  useEffect(() => {
    // Only a newly fetched list says anything new — the other dependencies (the map, the
    // range) re-run this against the list already in hand, which may predate the Apply.
    if (pendingIds === lastPendingIdsRef.current) return;
    lastPendingIdsRef.current = pendingIds;
    const now = new Set(pendingIds);
    let finished = [...previousPendingRef.current].some((id) => !now.has(id));
    const started = pendingIds.some((id) => !previousPendingRef.current.has(id));
    previousPendingRef.current = now;
    if (started) {
      // Drawn from tiles fetched before it went Pending; the server now leaves it out.
      if (map) refreshTrackLayer(map, activityQuery);
      watchCoverage();
    }
    for (const id of awaitingEditIdsRef.current) {
      if (now.has(id)) continue;
      awaitingEditIdsRef.current.delete(id);
      finished = true;
    }
    if (!finished) return;
    if (map) refreshTrackLayer(map, activityQuery);
    // Includes the Country/Region tiers, since an edit can un-visit a region too.
    watchCoverage();
    reloadTotals();
    reloadHistogram();
    setTrackMetricsVersion((v) => v + 1);
  }, [pendingIds, map, activityQuery, reloadTotals, reloadHistogram, watchCoverage]);

  /**
   * Re-attach anything that is not part of the basemap style.
   *
   * `setStyle` preserves the camera but discards custom layers, so every overlay
   * the later phases add has to be re-added on `styledata`. Nothing is added yet;
   * the insertion point is computed here so the fog phase is a small change
   * rather than a re-plumbing.
   */
  const reattachOverlays = useCallback(
    (instance: MapLibreMap) => {
      const beforeId = labelInsertionPoint(instance);
      // Fog and heatmap go in first (order between them doesn't matter — only one is ever
      // visible), tracks last, all at the same beforeId (beneath basemap labels —
      // layers.ts) — MapLibre paints later-added layers on top, so tracks land *above*
      // fog, letting a cleared route show through the veil rather than being buried under
      // it. setMapMode has to run again after this: addLayer always starts a fresh layer
      // hidden (fog.ts/heatmap.ts), so a styledata mid-non-Normal-mode would otherwise
      // silently drop back to Normal.
      ensureFogLayer(instance, beforeId);
      ensureHeatmapLayer(instance, beforeId);
      ensureTrackLayer(instance, beforeId, activityQuery);
      // After tracks, so it paints on top and fully overlays the one track it applies to —
      // see trackBands.ts's own doc comment for why this is a second layer rather than a
      // change to the shared one.
      ensureBandLayer(instance, beforeId);
      setMapMode(instance, mapMode, editingTrack);
      // Same reasoning as setMapMode just above: addLayer always starts the tracks layer
      // with no filter, so a styledata that recreates it would otherwise silently un-hide
      // everything the eye icon/TYPE/DISTANCE filters had hidden. Both setMapMode and
      // setHiddenTracks diff against the layer's current value before calling into
      // MapLibre — see docs/DEVELOPMENT.md's setFilter/setLayoutProperty gotcha for why
      // that isn't optional here.
      setHiddenTracks(instance, [...mapHiddenIds]);
      // Same reasoning again: ensureBandLayer above always (re)creates an empty source, so a
      // styledata mid-focus would otherwise silently wipe whatever bands were showing until
      // the selection happened to change again. Same mapHiddenIds check as the live effect
      // above: a styledata while the focused activity is hidden must not repaint its band.
      if (trackMetrics && focusedActivityId !== null && !mapHiddenIds.has(focusedActivityId) && !focusedPending) {
        setTrackBands(instance, trackMetrics.points, bandMetric);
      }
    },
    [mapMode, editingTrack, mapHiddenIds, activityQuery, trackMetrics, bandMetric, focusedActivityId, focusedPending],
  );

  useEffect(() => {
    if (!map) return;
    const onStyleData = () => reattachOverlays(map);
    map.on('styledata', onStyleData);
    // `map` only becomes non-null after the initial 'load' event, which is itself a
    // consequence of the style already having loaded — a *later* 'styledata' isn't
    // guaranteed to fire again on its own (verified directly: it didn't, in practice,
    // until a theme toggle forced a setStyle). Attach explicitly here too so overlays
    // show up on first paint, not only after the user happens to switch themes.
    reattachOverlays(map);
    return () => {
      map.off('styledata', onStyleData);
    };
  }, [map, reattachOverlays]);

  // Mode changes outside of a styledata event (the toggle itself, not a theme swap) still
  // need to flip the layer's visibility — reattachOverlays only re-runs on styledata.
  useEffect(() => {
    if (!map) return;
    setMapMode(map, mapMode, editingTrack);
  }, [map, mapMode, editingTrack]);

  // Keep the hash current. `moveend` rather than `move`: one rewrite per gesture,
  // not one per animation frame.
  useEffect(() => {
    if (!map) return;
    const sync = () => {
      const centre = map.getCenter();
      replaceHash({ longitude: centre.lng, latitude: centre.lat, zoom: map.getZoom() }, initial.flavor);
    };
    sync();
    map.on('moveend', sync);
    return () => {
      map.off('moveend', sync);
    };
  }, [map]);

  return (
    <div className="app-shell">
      <div className="app-body">
        {mapMode === 'normal' && (
          // Inert for the whole Edit track session: changing the range, the selection or a
          // filter underneath an open editor would pull the activity out from under it.
          // display: contents keeps the wrapper out of the flex layout.
          <div className="edit-track-lock" inert={editingTrack}>
            <ActivitiesPanel
              readOnly={isDemo}
              activities={filteredActivities}
              loading={activitiesLoading}
              error={activitiesError}
              totals={totals}
              facets={facets}
              excludedTypes={excludedTypes}
              onToggleType={toggleType}
              distanceBounds={activityDistanceBounds}
              distanceFilter={distanceFilter}
              onChangeDistance={setDistanceFilter}
              onResetFilters={resetActivityFilters}
              checked={checkedActivityIds}
              focusedId={focusedActivityId}
              hoveredId={hoveredActivityId}
              onToggle={toggleActivityChecked}
              onFocus={focusActivity}
              onHoverActivity={setHoveredActivityId}
              onClear={clearSelection}
              onSelectAll={selectAll}
              onInvertSelection={invertSelection}
              onShowSelected={showSelected}
              hiddenIds={hiddenActivityIds}
              onToggleGroupVisibility={toggleGroupVisibility}
              onActivityUpdated={reloadActivities}
              onActivitiesDeleted={handleActivitiesDeleted}
              onEditTrack={startEditTrack}
              duplicates={duplicates.duplicates}
              duplicatesError={duplicates.error}
              imports={imports}
              onViewOnMap={viewActivityOnMap}
              tab={panelTab}
              onTabChange={setPanelTab}
              map={map}
              onPrivateLocationsChanged={handlePrivateLocationsChanged}
            />
          </div>
        )}

        <div className="map-root">
          <div ref={container} className="map-canvas" data-testid="map-canvas" />
          {map && <ExportControl map={map} active={exportFlow.stage !== 'idle'} onOpen={handleExportOpen} />}
          {map && (exportFlow.stage === 'framing' || exportFlow.stage === 'capturing') && (
            <ExportFrame
              map={map}
              containerRef={container}
              preset={exportFlow.preset}
              geometry={exportFlow.geometry}
              busy={exportFlow.stage === 'capturing'}
              error={exportError}
              onGeometryChange={(geometry) =>
                setExportFlow((flow) => (flow.stage === 'framing' ? { stage: 'framing', preset: flow.preset, geometry } : flow))
              }
              onPresetChange={handleExportPresetChange}
              onCapture={() => void handleExportCapture()}
              onCancel={handleExportCancel}
            />
          )}
          {map && editingActivity && <EditTrackPanel map={map} activity={editingActivity} onClose={closeEditTrack} />}
          {!editingTrack && (
            <div className="map-mode-toggle" role="group" aria-label={t('map.mode')} data-testid="map-mode-toggle">
              <button
                type="button"
                className={mapMode === 'normal' ? 'map-mode-toggle__btn map-mode-toggle__btn--active' : 'map-mode-toggle__btn'}
                aria-pressed={mapMode === 'normal'}
                onClick={() => changeMapMode('normal')}
              >
                {t('map.mode_normal')}
              </button>
              {/* Normal on one side, the two coverage views on the other: two levels of choice. */}
              <span className="map-mode-toggle__divider" aria-hidden="true" />
              <button
                type="button"
                className={mapMode === 'fog' ? 'map-mode-toggle__btn map-mode-toggle__btn--active' : 'map-mode-toggle__btn'}
                aria-pressed={mapMode === 'fog'}
                onClick={() => changeMapMode('fog')}
              >
                {t('map.mode_fog')}
              </button>
              <button
                type="button"
                className={mapMode === 'heatmap' ? 'map-mode-toggle__btn map-mode-toggle__btn--active' : 'map-mode-toggle__btn'}
                aria-pressed={mapMode === 'heatmap'}
                onClick={() => changeMapMode('heatmap')}
              >
                {t('map.mode_heatmap')}
              </button>
            </div>
          )}
          {trackMetrics && !editingTrack && (
            <div className="track-metric-toggle" data-testid="track-metric-toggle">
              {/* Only worth a toggle when there's something to toggle to — an activity with
                  no heart-rate coverage just shows pace, with no single-option control for
                  it. */}
              {trackMetrics.heartrateAvailable && (
                <div className="trends__bucket" role="group" aria-label={t('map.colored_by')}>
                  <button
                    type="button"
                    className="trends__bucket-btn"
                    aria-pressed={bandMetric === 'speed'}
                    onClick={() => setBandMetric('speed')}
                  >
                    {t('map.metric_pace')}
                  </button>
                  <button
                    type="button"
                    className="trends__bucket-btn"
                    aria-pressed={bandMetric === 'heartrate'}
                    onClick={() => setBandMetric('heartrate')}
                  >
                    {t('map.metric_hr')}
                  </button>
                </div>
              )}
              <TrackProfile
                points={trackMetrics.points}
                metric={bandMetric}
                elevationAvailable={trackMetrics.elevationAvailable}
              />
            </div>
          )}
        </div>
      </div>

      {mapMode === 'normal' && (
        <div className="edit-track-lock" inert={editingTrack}>
          <ActivityHistogram
            days={visibleDays}
            onPan={panBy}
            pageStep={pageStep}
            canPanEarlier={canPanEarlier}
            canPanLater={canPanLater}
            selectedRange={selectedRange ?? { from: today, to: today }}
            onChangeSelection={changeSelectedRange}
            selectedRangeDays={selectedRangeDays}
            selectedActiveDays={selectedActiveDays}
            onCapacityChange={setBarsPerView}
            historyStart={earliest ?? selectedRange?.from ?? today}
            today={today}
          />
        </div>
      )}
    </div>
  );
}

