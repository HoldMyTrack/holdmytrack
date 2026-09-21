import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { flyToBBox, flyToView, unionBBox } from './bbox';
import { browserOrigin, WORLD_VIEW } from './config';
import { countryView } from './countryView';
import { centreIsOutside, readCoverageBounds, type CoverageBounds } from './coverage';
import { exportFramedImage } from './exportMap';
import type { ExportPreset } from './exportPresets';
import { ensureFogLayer } from './fog';
import { ensureHeatmapLayer } from './heatmap';
import { setMapMode, type MapMode } from './mapMode';
import { labelInsertionPoint } from './layers';
import { archiveUrl, type Flavor } from './style';
import { clearTrackBands, ensureBandLayer, setTrackBands, type BandMetric } from './trackBands';
import {
  ensureTrackLayer,
  refreshTrackLayer,
  setHiddenTracks,
  setHoveredTrack,
  setSelectedTracks,
  setTrackInteractivityHandlers,
} from './tracks';
import { useMapInstance } from './useMapInstance';
import { DEFAULT_FLAVOR, parseHash, replaceHash, type HashState, type ViewState } from './viewState';
import { getActivityTrackMetrics, listActivities, type Activity, type ActivityTrackMetrics } from '../api';
import { useAuth } from '../auth/AuthContext';
import { distanceBounds, passesFilters, typeFacets, type DistanceRange } from '../ui/activityFacets';
import { ActivitiesPanel } from '../ui/ActivitiesPanel';
import { ActivityHistogram } from '../ui/ActivityHistogram';
import { CoverageNotice } from '../ui/CoverageNotice';
import { ExportButton } from '../ui/ExportButton';
import { ExportFrame, type FrameRect } from '../ui/ExportFrame';
import { ExportPresetDialog } from '../ui/ExportPresetDialog';
import { dayDiff, todayUTC } from '../ui/dateMath';
import { Header } from '../ui/Header';
import type { DateRange } from '../ui/RangePicker';
import { TrackProfile } from '../ui/TrackProfile';
import { useUnitSystem } from '../ui/units';
import { ImportPanel } from '../ui/ImportPanel';
import { useActivityDays } from '../ui/useActivityDays';
import { useActivityList } from '../ui/useActivityList';
import { useActivityTotals } from '../ui/useActivityTotals';
import { useDuplicates } from '../ui/useDuplicates';

/** How many calendar days back Heatmap's rolling window reaches — must match
 *  services/server/internal/fog's HeatmapWindowDays exactly, since this is only used to fetch
 *  the same set of activities server-side already renders, for the "fly to fit Heatmap's
 *  current coverage" camera target below. */
const HEATMAP_WINDOW_DAYS = 365;

/** `from` boundary for the Heatmap fly-to-coverage fetch below — a plain function, not a
 *  memoized value, since "365 days before right now" has to be read fresh at the moment of
 *  each transition into Heatmap, not fixed once at module load. */
function heatmapWindowStart(): string {
  const d = new Date();
  d.setUTCDate(d.getUTCDate() - HEATMAP_WINDOW_DAYS);
  return d.toISOString().slice(0, 10);
}

/** How long a checkbox-selection spree pauses before the map auto-flies to fit it (replacing
 *  the old explicit "Fit map" button — see IMPLEMENTATION.md §4.7) — long enough that ticking
 *  three boxes in a row flies once, at the end, not three times. */
const SELECTION_FLY_DEBOUNCE_MS = 300;

export interface MapViewProps {
  /** Wires the account menu's "Profile" item — App.tsx owns which screen is showing, MapView
   *  just needs somewhere to send that one click. */
  onOpenProfile: () => void;
  /** Same shape as onOpenProfile, for the account menu's "Settings" item. */
  onOpenSettings: () => void;
}

/** The frame-and-capture export flow's own state machine — lives here, not inside
 *  `ExportFrame.tsx`/`ExportPresetDialog.tsx` themselves, since it spans all three of those
 *  components plus the live map instance this component already owns (`ExportButton.tsx` opens
 *  it, `ExportPresetDialog` picks the shape, `ExportFrame` renders/drags it, and capture needs
 *  `map` directly) — the same "callback wiring belongs where the map instance is" reasoning
 *  `ExportButton.tsx`'s own doc comment already gives for why `map` is a prop there. */
type ExportFlow =
  | { stage: 'idle' }
  | { stage: 'picking' }
  | { stage: 'framing' | 'capturing'; preset: ExportPreset | 'custom'; rect: FrameRect };

/** A centered starting rect for a freshly-picked shape. Custom gets a generously-sized default
 *  (70% of the container) so it's immediately visible and easy to grab without the user first
 *  hunting for it. A preset's rect is sized to its own declared aspect ratio — accounting for
 *  the container's own aspect ratio, since `wFrac`/`hFrac` are fractions of two potentially
 *  different container dimensions, not a shared unit — clamped so neither dimension exceeds
 *  80% of the container. */
function defaultFrameRect(preset: ExportPreset | 'custom', containerRect: DOMRect): FrameRect {
  if (preset === 'custom') {
    return { xFrac: 0.15, yFrac: 0.15, wFrac: 0.7, hFrac: 0.7 };
  }
  const MAX_FRACTION = 0.8;
  const targetRatio = preset.widthPx / preset.heightPx;
  const containerRatio = containerRect.width / containerRect.height || 1;
  let wFrac: number;
  let hFrac: number;
  if (targetRatio / containerRatio >= 1) {
    wFrac = MAX_FRACTION;
    hFrac = wFrac * (containerRatio / targetRatio);
  } else {
    hFrac = MAX_FRACTION;
    wFrac = hFrac * (targetRatio / containerRatio);
  }
  return { xFrac: (1 - wFrac) / 2, yFrac: (1 - hFrac) / 2, wFrac, hFrac };
}

export function MapView({ onOpenProfile, onOpenSettings }: MapViewProps) {
  // docs/SPEC.md FR-2.1–FR-2.3: a demo account is
  // read-only (no upload/sync, no edit/delete) — see ActivitiesPanel's own readOnly prop and
  // the importControl below. `'email' in user` is the same narrowing api.ts's SessionUser
  // already establishes as the way to tell a DemoUser from an AuthUser.
  const { user } = useAuth();
  const isDemo = !('email' in user);

  // Read once per mount, not once per page load: App.tsx clears window.location.hash on every
  // app-initiated identity change (sign-out, demo start, sign-in — see clearSavedView in
  // viewState.ts), but that only helps if MapView re-reads the hash when it remounts for the
  // new session. A module-level constant here previously meant a stale hash from a *previous*
  // session's last camera position silently won over the new session's own fly-to-most-recent
  // (reported live: the Demo Customer account not flying to its own data after a same-tab
  // sign-out/demo-start, fixed by a full reload — which is exactly what re-evaluated a
  // module-level read, and exactly what a remount now does instead).
  const [initial] = useState<{ hash: HashState; view: ViewState; flavor: Flavor }>(() => {
    const hash = parseHash(window.location.hash);
    return {
      hash,
      view: hash.view ?? { longitude: WORLD_VIEW.longitude, latitude: WORLD_VIEW.latitude, zoom: WORLD_VIEW.zoom },
      flavor: hash.flavor ?? DEFAULT_FLAVOR,
    };
  });

  const container = useRef<HTMLDivElement>(null);
  const [bounds, setBounds] = useState<CoverageBounds | null>(null);
  const [outside, setOutside] = useState(false);
  const [exportFlow, setExportFlow] = useState<ExportFlow>({ stage: 'idle' });
  const [exportError, setExportError] = useState<string | null>(null);
  // Normal is what already rendered before fog existed — it needed no new work to count
  // as a "mode" (IMPLEMENTATION.md §4.2.2).
  const [mapMode, setMapModeState] = useState<MapMode>('normal');

  const today = useMemo(() => todayUTC(), []);

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
  const changeSelectedRange = useCallback((next: DateRange) => {
    userChangedRangeRef.current = true;
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

  const activityDistanceBounds = useMemo(() => distanceBounds(activities), [activities]);
  const facets = useMemo(() => typeFacets(activities, distanceFilter), [activities, distanceFilter]);
  const filteredActivities = useMemo(
    () => activities.filter((a) => passesFilters(a, excludedTypes, distanceFilter)),
    [activities, excludedTypes, distanceFilter],
  );
  // Union of "filtered out by TYPE/DISTANCE" and "hidden by its own eye icon" — both answer
  // the same question for the map (don't paint this track), so both flow into one filter.
  const mapHiddenIds = useMemo(() => {
    const filteredOut = activities.filter((a) => !passesFilters(a, excludedTypes, distanceFilter)).map((a) => a.id);
    return new Set([...filteredOut, ...hiddenActivityIds]);
  }, [activities, excludedTypes, distanceFilter, hiddenActivityIds]);

  const unitSystem = useUnitSystem();
  const map = useMapInstance({
    container,
    initialView: initial.view,
    initialFlavor: initial.flavor,
    scaleUnit: unitSystem,
  });

  const handlePickExportPreset = useCallback((preset: ExportPreset | 'custom') => {
    const rect = container.current
      ? defaultFrameRect(preset, container.current.getBoundingClientRect())
      : { xFrac: 0.15, yFrac: 0.15, wFrac: 0.7, hFrac: 0.7 };
    setExportFlow({ stage: 'framing', preset, rect });
  }, []);

  const handleExportCapture = useCallback(async () => {
    if (exportFlow.stage !== 'framing' || !map || !container.current) return;
    const { preset, rect } = exportFlow;
    setExportFlow({ stage: 'capturing', preset, rect });
    setExportError(null);
    try {
      const containerRect = container.current.getBoundingClientRect();
      const frameRect = new DOMRect(
        containerRect.left + rect.xFrac * containerRect.width,
        containerRect.top + rect.yFrac * containerRect.height,
        rect.wFrac * containerRect.width,
        rect.hFrac * containerRect.height,
      );
      const blob = await exportFramedImage(
        map,
        { flavor: initial.flavor, mode: mapMode, activityQuery, hiddenIds: [...mapHiddenIds] },
        {
          containerRect,
          frameRect,
          ...(preset !== 'custom' && { target: { widthPx: preset.widthPx, heightPx: preset.heightPx } }),
        },
      );
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `fitmap-${new Date().toISOString().slice(0, 10)}.png`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      setExportFlow({ stage: 'idle' });
    } catch (err) {
      setExportError(err instanceof Error ? err.message : String(err));
      // Stays in 'framing', not 'idle' — a failed capture (e.g. a network timeout) shouldn't
      // discard the frame the user just positioned, forcing them to redo it from scratch.
      setExportFlow((flow) => (flow.stage === 'capturing' ? { stage: 'framing', preset: flow.preset, rect: flow.rect } : flow));
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
  // selection (neither mode can select or focus a single activity) and fly to fit whatever
  // that mode actually renders, then restore the selection exactly on the way back to Normal.
  // Only that one transition does either: toggling directly between Fog and Heatmap touches
  // neither, since Normal's selection state was never disturbed to begin with.
  const modeSnapshotRef = useRef<{ checked: Set<string>; focused: string | null } | null>(null);
  const flyToModeCoverage = useCallback(
    (mode: 'fog' | 'heatmap') => {
      if (!map) return;
      const query = mode === 'heatmap' ? { from: heatmapWindowStart() } : {};
      listActivities(query)
        .then((all) => {
          const flyBounds = unionBBox(all.flatMap((a) => (a.bbox ? [a.bbox] : [])));
          if (flyBounds) flyToBBox(map, flyBounds);
        })
        .catch((error: unknown) => {
          console.error('Could not fetch activities to fly to mode coverage', error);
        });
    },
    [map],
  );
  const changeMapMode = useCallback(
    (next: MapMode) => {
      if (next === mapMode) return;
      if (mapMode === 'normal') {
        modeSnapshotRef.current = { checked: checkedActivityIds, focused: focusedActivityId };
        setCheckedActivityIds(new Set());
        setFocusedActivityId(null);
        flyToModeCoverage(next as 'fog' | 'heatmap');
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
    [mapMode, checkedActivityIds, focusedActivityId, flyToModeCoverage],
  );

  // Auto-fly on checked-group change ("FLYING TO 3 SELECTED"),
  // replacing the old explicit "Fit map to selection" button — see IMPLEMENTATION.md §4.7.
  // Debounced so a multi-checkbox spree flies once, after the user pauses, not once per
  // checkbox. Guarded on
  // a non-empty group because *this* effect has nothing sensible to fly to once it's empty —
  // clearSelection below is the one place checkedActivityIds goes back to empty on purpose,
  // and it flies to every activity instead, explicitly, rather than relying on this effect to
  // fire on the way down to zero.
  //
  // Excludes mapHiddenIds the same way the band-change effect below does: checking a row is
  // independent of its own eye icon (hiding its track), so a hidden-but-checked activity is a
  // real, reachable state — reported live as flying to that activity's bbox anyway, which
  // looks like flying to empty water since nothing is drawn there. If every checked activity
  // is currently hidden, unionBBox([]) is null and fitToSelection is a no-op, same as an
  // empty group.
  useEffect(() => {
    if (checkedActivityIds.size === 0) return;
    const timer = window.setTimeout(() => {
      const visible = activities.filter((a) => checkedActivityIds.has(a.id) && !mapHiddenIds.has(a.id));
      fitToSelection(visible);
    }, SELECTION_FLY_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [checkedActivityIds, activities, mapHiddenIds, fitToSelection]);

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

  // ROADMAP.md's "View on map" item — ImportPanel.tsx's per-row action, reusing focusActivity
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
      const day = startedAtIso.slice(0, 10); // YYYY-MM-DD (UTC) — same day-precision selectedRange itself uses
      if (selectedRange !== null && day >= selectedRange.from && day <= selectedRange.to) {
        focusActivity(activityId);
        return;
      }
      pendingFocusIdRef.current = activityId;
      changeSelectedRange({ from: day, to: day });
    },
    [mapMode, changeMapMode, selectedRange, changeSelectedRange, focusActivity],
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

  // "Show selected" — no state change, just a manual re-trigger of the same fly-to-fit the
  // debounced checkbox effect above already computes, for after panning away from the group.
  const showSelected = useCallback(() => {
    const visible = activities.filter((a) => checkedActivityIds.has(a.id) && !mapHiddenIds.has(a.id));
    fitToSelection(visible);
  }, [activities, checkedActivityIds, mapHiddenIds, fitToSelection]);

  // Read inside the effect below without being one of its dependencies: the fly-to-fit it
  // does is keyed on the *range* changing (activities is only ever a new array when
  // selectedRange or an upload changes it — TYPE/DISTANCE/eye-icon toggles narrow
  // mapHiddenIds without a new activities fetch), but the bounds it flies to still have to
  // exclude whatever those filters currently hide, or the camera would zoom out to include
  // a track that isn't even being drawn.
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
  // rather than just generic. Every change after the first flies to fit whatever's now
  // actually visible, as before.
  const hasFlownToActivitiesRef = useRef(false);
  useEffect(() => {
    if (!map || activities.length === 0) return;
    if (!hasFlownToActivitiesRef.current) {
      hasFlownToActivitiesRef.current = true;
      if (initial.hash.view == null) {
        const mostRecent = activities.reduce((latest, a) => (a.startedAt > latest.startedAt ? a : latest));
        fitToSelection([mostRecent]);
      }
      return;
    }
    const visible = activities.filter((a) => !mapHiddenIdsRef.current.has(a.id));
    const flyBounds = unionBBox(visible.flatMap((a) => (a.bbox ? [a.bbox] : [])));
    if (flyBounds) flyToBBox(map, flyBounds);
  }, [map, activities, fitToSelection]);

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
  useEffect(() => {
    setTrackInteractivityHandlers({
      onSelect: focusActivity,
      onHover: setHoveredActivityId,
      onClickAway: () => setFocusedActivityId(null),
    });
  }, [focusActivity]);

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
  }, [focusedActivityId]);

  // Draws (or clears) the colored zone segments themselves — separate from the fetch effect
  // above so switching the Pace/Heart rate toggle re-renders instantly from data already in
  // hand, no refetch. Also cleared when the focused activity itself is hidden (its eye icon,
  // or a TYPE/DISTANCE filter narrowing it out) — reported live as hiding a focused activity's
  // plain track (setHiddenTracks, below) while this layer, deliberately drawn *on top* of that
  // same track (trackBands.ts's own doc comment), kept painting it regardless, since this
  // effect never checked mapHiddenIds. Re-showing it draws the band again from data already in
  // hand, no refetch, same as the Pace/Heart rate toggle above.
  useEffect(() => {
    if (!map) return;
    if (trackMetrics && focusedActivityId !== null && !mapHiddenIds.has(focusedActivityId)) {
      setTrackBands(map, trackMetrics.points, bandMetric);
    } else {
      clearTrackBands(map);
    }
  }, [map, trackMetrics, bandMetric, focusedActivityId, mapHiddenIds]);

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

  // A finished upload is a new track on the map and a new row in every §4.7 response, so
  // refresh all four together rather than let the header badge fall behind the geometry.
  const handleUploaded = useCallback(() => {
    if (map) refreshTrackLayer(map, activityQuery);
    reloadActivities();
    reloadTotals();
    reloadHistogram();
    // A finished upload/sync is also the one thing that can produce a new duplicate.
    duplicates.refresh();
  }, [map, activityQuery, reloadActivities, reloadTotals, reloadHistogram, duplicates.refresh]);

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
      setMapMode(instance, mapMode);
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
      if (trackMetrics && focusedActivityId !== null && !mapHiddenIds.has(focusedActivityId)) {
        setTrackBands(instance, trackMetrics.points, bandMetric);
      }
    },
    [mapMode, mapHiddenIds, activityQuery, trackMetrics, bandMetric, focusedActivityId],
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
    setMapMode(map, mapMode);
  }, [map, mapMode]);

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

  // Coverage bounds come from the archive header, so the notice can never drift
  // out of sync with whatever extract is actually deployed.
  useEffect(() => {
    let cancelled = false;
    readCoverageBounds(archiveUrl(browserOrigin()))
      .then((value) => {
        if (!cancelled) setBounds(value);
      })
      .catch((error: unknown) => {
        // Non-fatal: without bounds we simply never show the notice.
        console.error('Could not read basemap coverage bounds', error);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!map || !bounds) return;
    const check = () => setOutside(centreIsOutside(map, bounds));
    check();
    map.on('moveend', check);
    return () => {
      map.off('moveend', check);
    };
  }, [map, bounds]);

  return (
    <div className="app-shell">
      <Header
        importControl={<ImportPanel readOnly={isDemo} onUploaded={handleUploaded} onViewOnMap={viewActivityOnMap} />}
        exportControl={<ExportButton map={map} onOpen={() => setExportFlow({ stage: 'picking' })} />}
        onOpenProfile={onOpenProfile}
        onOpenSettings={onOpenSettings}
      />

      {exportFlow.stage === 'picking' && (
        <ExportPresetDialog
          onPick={handlePickExportPreset}
          onClose={() => setExportFlow((flow) => (flow.stage === 'picking' ? { stage: 'idle' } : flow))}
        />
      )}

      <div className="app-body">
        {mapMode === 'normal' && (
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
            onShowSelected={showSelected}
            hiddenIds={hiddenActivityIds}
            onToggleGroupVisibility={toggleGroupVisibility}
            onActivityUpdated={reloadActivities}
            onActivitiesDeleted={handleActivitiesDeleted}
            duplicates={duplicates.duplicates}
            duplicatesError={duplicates.error}
          />
        )}

        <div className="map-root">
          <div ref={container} className="map-canvas" data-testid="map-canvas" />
          {(exportFlow.stage === 'framing' || exportFlow.stage === 'capturing') && (
            <ExportFrame
              containerRef={container}
              preset={exportFlow.preset}
              rect={exportFlow.rect}
              busy={exportFlow.stage === 'capturing'}
              error={exportError}
              onRectChange={(rect) =>
                setExportFlow((flow) => (flow.stage === 'framing' ? { stage: 'framing', preset: flow.preset, rect } : flow))
              }
              onCapture={() => void handleExportCapture()}
              onCancel={handleExportCancel}
            />
          )}
          <div className="map-mode-toggle" role="group" aria-label="Map mode" data-testid="map-mode-toggle">
            <button
              type="button"
              className={mapMode === 'normal' ? 'map-mode-toggle__btn map-mode-toggle__btn--active' : 'map-mode-toggle__btn'}
              aria-pressed={mapMode === 'normal'}
              onClick={() => changeMapMode('normal')}
            >
              Normal
            </button>
            <button
              type="button"
              className={mapMode === 'fog' ? 'map-mode-toggle__btn map-mode-toggle__btn--active' : 'map-mode-toggle__btn'}
              aria-pressed={mapMode === 'fog'}
              onClick={() => changeMapMode('fog')}
            >
              Fog
            </button>
            <button
              type="button"
              className={mapMode === 'heatmap' ? 'map-mode-toggle__btn map-mode-toggle__btn--active' : 'map-mode-toggle__btn'}
              aria-pressed={mapMode === 'heatmap'}
              onClick={() => changeMapMode('heatmap')}
            >
              Heatmap
            </button>
          </div>
          {outside && <CoverageNotice />}
          {trackMetrics && (
            <div className="track-metric-toggle" data-testid="track-metric-toggle">
              {/* Only worth a toggle when there's something to toggle to — an activity with
                  no heart-rate coverage just shows pace, with no single-option control for
                  it. */}
              {trackMetrics.heartrateAvailable && (
                <div className="trends__bucket" role="group" aria-label="Colored by">
                  <button
                    type="button"
                    className="trends__bucket-btn"
                    aria-pressed={bandMetric === 'speed'}
                    onClick={() => setBandMetric('speed')}
                  >
                    Pace
                  </button>
                  <button
                    type="button"
                    className="trends__bucket-btn"
                    aria-pressed={bandMetric === 'heartrate'}
                    onClick={() => setBandMetric('heartrate')}
                  >
                    Heart rate
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
        />
      )}
    </div>
  );
}

