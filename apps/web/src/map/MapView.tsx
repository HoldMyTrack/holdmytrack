import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import 'maplibre-gl/dist/maplibre-gl.css';
import { activityQuery as queryFor } from './activityQuery';
import { flyToBBox, flyToView, unionBBox } from './bbox';
import { firstListFit, needsFallbackView } from './cameraRules';
import { satelliteSource, WORLD_VIEW } from './config';
import { countryView } from './countryView';
import { defaultRange } from './defaultRange';
import type { MapMode } from './mapMode';
import { loadOverlays, saveOverlays, type Overlays } from './overlays';
import { pendingChange } from './pending';
import { invertedChecks, mapHiddenIds as hiddenOnMap, toggled, toggledVisibility, toolbarTarget, withoutIds } from './selection';
import { storiesTabParam, storyParam, urlWithStory, type StoryNav } from './storyUrl';
import type { SpotCategory } from './spots';
import { clearTrackBands, setTrackBands } from './trackBands';
import { refreshTrackLayer, setHiddenTracks, setHoveredTrack, setSelectedTracks, setTrackInteractivityHandlers } from './tracks';
import { useCoverageRefresh } from './useCoverageRefresh';
import { useExportFlow } from './useExportFlow';
import { useFileDrop } from './useFileDrop';
import { useHashSync, useInitialView } from './useInitialView';
import { useMapInstance } from './useMapInstance';
import { useMapLayers } from './useMapLayers';
import { usePhotoLayer } from './usePhotoLayer';
import { useSpots } from './useSpots';
import { getActivityTrackMetrics, type Activity, type ActivityTrackMetrics, type Story } from '../api';
import { useAuth } from '../auth/AuthContext';
import { distanceBounds, passesFilters, typeFacets, type DistanceRange } from '../ui/activityFacets';
import { ActivitiesPanel, type PanelTab, type StoriesPanel } from '../ui/ActivitiesPanel';
import { editPhotosBlocker, editTrackBlocker } from '../ui/editAvailability';
import { EditActivityWindow, type EditTab, type EditWindowResult } from '../ui/EditActivityWindow';
import { ExportControl } from '../ui/ExportControl';
import { ExportViewfinder } from '../ui/ExportViewfinder';
import { OverlaysMenu } from '../ui/OverlaysMenu';
import { BasemapToggle } from '../ui/BasemapToggle';
import { PhotoPopup } from '../ui/PhotoPopup';
import { ShowInArea } from '../ui/ShowInArea';
import { ZoomLevelNotice } from '../ui/ZoomLevelNotice';
import { SpotPopup } from '../ui/SpotPopup';
import { todayLocal, type DateRange } from '../ui/dateMath';
import { useUnitSystem } from '../ui/units';
import { useActivityDays } from '../ui/useActivityDays';
import { useActivityList } from '../ui/useActivityList';
import { useStories } from '../ui/useStories';
import { useStoryInbox } from '../ui/useStoryInbox';
import { useStory } from '../ui/useStory';
import { t } from '../i18n';

/** How often the list is re-read while a reprocess is pending: one activity's ingest takes seconds. */
const EDIT_PENDING_POLL_MS = 2000;

function moveStoryUrl(id: string | null, nav: StoryNav) {
  if (nav === 'push') window.history.pushState(null, '', urlWithStory(window.location, id));
  else if (nav === 'replace') window.history.replaceState(null, '', urlWithStory(window.location, id));
}

export interface MapViewProps {
  /** Mount with the Activities panel on its Privacy tab: `/?private-locations` (App.tsx). */
  initialPrivateLocationsOpen?: boolean;
  /** Mount on one activity, its day selected and it focused: `/?activity=&day=`, the /sync page's "View on map". */
  initialActivity?: { id: string; day: string } | null;
}

export function MapView({ initialPrivateLocationsOpen = false, initialActivity = null }: MapViewProps) {
  // A demo account is read-only (docs/SPEC.md FR-2.1); `'email' in user` tells a DemoUser from an AuthUser.
  const { user } = useAuth();
  const isDemo = !('email' in user);

  const { initial, flavor, pinned } = useInitialView();

  const container = useRef<HTMLDivElement>(null);
  // One of Normal, Fog and Heatmap at a time (docs/SPEC.md FR-4.4).
  const [mapMode, setMapModeState] = useState<MapMode>('normal');
  // The Satellite button and the Layers menu's paths and Spots categories, remembered per browser (overlays.ts).
  const [overlays, setOverlays] = useState<Overlays>(loadOverlays);
  const changeOverlays = useCallback((next: Overlays) => {
    saveOverlays(next);
    setOverlays(next);
  }, []);
  const paths = useMemo(
    () => ({
      bikePaths: overlays.enabled && overlays.bikePaths,
      sharedPaths: overlays.enabled && overlays.sharedPaths,
      mtbTrails: overlays.enabled && overlays.mtbTrails,
    }),
    [overlays.enabled, overlays.bikePaths, overlays.sharedPaths, overlays.mtbTrails],
  );
  // Satellite imagery (FR-4.14) only when the deployment configures some, whatever was saved.
  const satelliteAvailable = satelliteSource() !== null;
  const satellite = satelliteAvailable && overlays.satellite;

  const today = useMemo(() => todayLocal(), []);

  // Selection lives here because the map and the fly-to callbacks need it. Focus and the checked
  // group are independent: a row's text or its track *focuses* it (one at a time, bolded, flown
  // to; clicking empty space clears it). A checkbox only builds the toolbar's group: no bold, no
  // fly, and it never touches the focus. Both start empty.
  const [checkedActivityIds, setCheckedActivityIds] = useState<Set<string>>(() => new Set());
  const [focusedActivityId, setFocusedActivityId] = useState<string | null>(null);
  // The row or track under the pointer, from either surface, so one effect owns the map's hover
  // feature-state. A preview only: it never moves the camera.
  const [hoveredActivityId, setHoveredActivityId] = useState<string | null>(null);
  // Tracks hidden from the map; independent of selection and of the TYPE/DISTANCE filters.
  const [hiddenActivityIds, setHiddenActivityIds] = useState<Set<string>>(() => new Set());

  // The focused activity's pace-colored segments (trackBands.ts); null whenever nothing is focused.
  const [trackMetrics, setTrackMetrics] = useState<ActivityTrackMetrics | null>(null);
  // Bumped when a reprocess finishes, so the bands refetch against the new points.
  const [trackMetricsVersion, setTrackMetricsVersion] = useState(0);

  // The Edit window's activities, by id. While it's open the panel is inert and the map's own controls step aside.
  const [editWindowIds, setEditWindowIds] = useState<string[] | null>(null);
  const editOpen = editWindowIds !== null;
  // The Track tab's session (§4.7.7): one activity, until the window closes. Every other track is
  // hidden and TrackEditor owns the map.
  const [editingActivityId, setEditingActivityId] = useState<string | null>(null);
  const editingTrack = editingActivityId !== null;
  const spotsShown = useMemo<readonly SpotCategory[]>(
    () => (editingTrack || !overlays.enabled ? [] : overlays.spots),
    [editingTrack, overlays.enabled, overlays.spots],
  );

  // The panel's tab lives here so `/?private-locations` (FR-8.1) and `/?story=` (FR-14.6) can open
  // on it, and so it survives the panel unmounting in Fog and Heatmap.
  const [panelTab, setPanelTab] = useState<PanelTab>(
    initialActivity !== null
      ? 'activities'
      : storyParam(window.location.search) !== null || storiesTabParam(window.location.search)
        ? 'stories'
        : initialPrivateLocationsOpen
          ? 'private'
          : 'activities',
  );

  // The date slider's selection. The slider's own window lives in useActivityDays, so paging
  // through history never changes the selection. Starts null: its default needs the first page of days.
  const [selectedRange, setSelectedRangeState] = useState<DateRange | null>(null);

  // The open Story (FR-14.6). While one is open, the list and tracks are its activities with no
  // date range; `selectedRange` is kept for when the Story closes.
  const [storyId, setStoryId] = useState<string | null>(() => storyParam(window.location.search));
  const storyState = useStory(storyId);

  // The Edit window's tab: its Track tab hides the photos.
  const [editTab, setEditTab] = useState<EditTab>('activity');
  const storiesList = useStories(panelTab === 'stories');

  // TYPE/DISTANCE filters over the loaded activities (activityFacets.ts).
  const [excludedTypes, setExcludedTypes] = useState<Set<string>>(() => new Set());
  const [distanceFilter, setDistanceFilter] = useState<DistanceRange | null>(null);

  const toggleActivityChecked = useCallback((id: string) => {
    setCheckedActivityIds((prev) => toggled(prev, id));
  }, []);
  const toolbarTargetIds = useMemo(() => toolbarTarget(checkedActivityIds, focusedActivityId), [checkedActivityIds, focusedActivityId]);
  const toggleGroupVisibility = useCallback(() => {
    setHiddenActivityIds((prev) => toggledVisibility(prev, toolbarTargetIds));
  }, [toolbarTargetIds]);
  // A toggle: the type stays in the dropdown either way, so it can be turned back on without Reset filters.
  const toggleType = useCallback((type: string) => {
    setExcludedTypes((prev) => toggled(prev, type));
  }, []);
  const resetActivityFilters = useCallback(() => {
    setExcludedTypes(new Set());
    setDistanceFilter(null);
  }, []);

  // Once the user picks a range, the default never replaces it, even when an upload lands outside it.
  const userChangedRangeRef = useRef(false);
  // Only a range the user picked moves the camera, never a re-derived default.
  const flyToNextRangeRef = useRef(false);
  // A new range clears the selection, focus, hidden set and filters (docs/SPEC.md FR-6.6): they
  // could name activities the new range doesn't list, and a stale distance band could exclude all.
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

  // A Story or the date range, never both.
  const activityQuery = useMemo(() => queryFor(storyId, selectedRange), [selectedRange, storyId]);

  // Fetched once here so the panel and the drawn tracks agree on the same rows.
  const {
    activities,
    loading: activitiesLoading,
    error: activitiesError,
    reload: reloadActivities,
    loadedKey: activitiesLoadedKey,
  } = useActivityList(activityQuery);
  // The slider's window of activity-days and its pan position, independent of the selection.
  const {
    visibleDays,
    earliest,
    ready: daysReady,
    canPanEarlier,
    canPanLater,
    panBy,
    reveal: revealDay,
    shift: shiftDays,
    canShift,
    reload: reloadDays,
    generation: historyGeneration,
  } = useActivityDays();

  // The default range (defaultRange.ts) is re-derived on every real refetch of days
  // (`historyGeneration`) until the user picks a range. Trap: keying on `visibleDays` would also
  // re-pick on Earlier/Later paging, and settling once would freeze a zero-history account (every
  // demo) on `{today, today}` past its first upload.
  //
  // Held while the Edit window is open: an import landing newer days would slide the default past
  // the edited activity and close the window. It catches up when the window closes.
  useEffect(() => {
    if (userChangedRangeRef.current || !daysReady || editOpen) return;
    setSelectedRangeState(defaultRange(visibleDays, earliest, today));
    // visibleDays isn't a dependency; see above.
  }, [daysReady, earliest, historyGeneration, today, editOpen]);

  // Rows with a reprocess pending (a track edit or a Private location change). A Pending track isn't drawn (FR-5.15).
  const pendingIds = useMemo(() => activities.filter((a) => a.pending).map((a) => a.id), [activities]);

  const activityDistanceBounds = useMemo(() => distanceBounds(activities), [activities]);
  // The distance slider shows only while the activities span a range. A band left set after that
  // would keep filtering with nothing on screen to clear it.
  const distanceSliderShown = activityDistanceBounds !== null && activityDistanceBounds.min < activityDistanceBounds.max;
  useEffect(() => {
    if (!distanceSliderShown) setDistanceFilter(null);
  }, [distanceSliderShown]);
  const facets = useMemo(() => typeFacets(activities, distanceFilter), [activities, distanceFilter]);
  const filteredActivities = useMemo(
    () => activities.filter((a) => passesFilters(a, excludedTypes, distanceFilter)),
    [activities, excludedTypes, distanceFilter],
  );
  // The tile already omits a Pending activity; this hides it before the refetch lands.
  const mapHiddenIds = useMemo(
    () => hiddenOnMap(activities, excludedTypes, distanceFilter, hiddenActivityIds, pendingIds),
    [activities, excludedTypes, distanceFilter, hiddenActivityIds, pendingIds],
  );

  const unitSystem = useUnitSystem();
  const map = useMapInstance({
    container,
    initialView: initial.view,
    initialFlavor: initial.flavor,
    initialSatellite: satellite,
    scaleUnit: unitSystem,
  });

  const exportScene = useMemo(
    () => ({ flavor, mode: mapMode, activityQuery, hiddenIds: [...mapHiddenIds], paths, satellite }),
    [flavor, mapMode, activityQuery, mapHiddenIds, paths, satellite],
  );
  const exporter = useExportFlow(map, container, exportScene);

  // A Story's photos change with its members too.
  const storyMembers = storyState.story?.activityIds.join(',') ?? '';
  const photoLayer = usePhotoLayer(map, {
    mode: mapMode,
    editWindowIds,
    focusedId: focusedActivityId,
    storyId,
    version: `${trackMetricsVersion}|${storyMembers}`,
    trackTabShown: editOpen && editTab === 'track',
    mapHiddenIds,
  });
  const photoState = photoLayer.photos;

  // Flies to the combined bounds of the given activities; one activity is its own bbox.
  const fitToSelection = useCallback(
    (selection: Activity[]) => {
      if (!map) return;
      const flyBounds = unionBBox(selection.flatMap((a) => (a.bbox ? [a.bbox] : [])));
      if (flyBounds) flyToBBox(map, flyBounds);
    },
    [map],
  );

  // Fog and Heatmap can't select or focus, so leaving Normal clears the checked group and focus
  // and returning to Normal restores them. Switching between Fog and Heatmap touches neither. A
  // mode change never moves the camera.
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

  // Focuses one activity and flies to it, unless it's hidden (an empty fit is a no-op). With the
  // toolbar's Focus on map, the only fly to an activity.
  const focusActivity = useCallback(
    (id: string) => {
      setFocusedActivityId(id);
      const target = activities.filter((a) => a.id === id && !mapHiddenIds.has(a.id));
      fitToSelection(target);
    },
    [activities, mapHiddenIds, fitToSelection],
  );

  // "View on map" (`/?activity=&day=`). If the activity's day is outside the selected range, the
  // range narrows to that day and the focus waits for the new list (the effect below). The day is
  // the activity's local day, as the server reads a bare from/to (IMPLEMENTATION.md §4.30); a UTC
  // day would miss an evening activity west of Greenwich. Returns to Normal first, since only it focuses.
  const pendingFocusIdRef = useRef<string | null>(null);
  const viewActivityOnDay = useCallback(
    (activityId: string, day: string) => {
      if (mapMode !== 'normal') changeMapMode('normal');
      // The slider's window moves to the day too, or its knobs would sit off its edges.
      revealDay(day);
      if (selectedRange !== null && day >= selectedRange.from && day <= selectedRange.to) {
        focusActivity(activityId);
        return;
      }
      pendingFocusIdRef.current = activityId;
      changeSelectedRange({ from: day, to: day });
      // The focus flies to the activity; the range fly would override it.
      flyToNextRangeRef.current = false;
    },
    [mapMode, changeMapMode, selectedRange, changeSelectedRange, focusActivity, revealDay],
  );
  // `/?activity=&day=`, once, on arrival: before any range of the user's own, so the default never replaces it.
  const arrivedOnActivityRef = useRef(false);
  useEffect(() => {
    if (initialActivity === null || arrivedOnActivityRef.current) return;
    arrivedOnActivityRef.current = true;
    viewActivityOnDay(initialActivity.id, initialActivity.day);
  }, [initialActivity, viewActivityOnDay]);
  // Waits for the map too: on arrival the list can land before the map has loaded.
  useEffect(() => {
    const pending = pendingFocusIdRef.current;
    if (pending === null || !map) return;
    if (activities.some((a) => a.id === pending)) {
      pendingFocusIdRef.current = null;
      focusActivity(pending);
    }
  }, [map, activities, focusActivity]);

  // A click on empty list space, the counterpart of clicking away from every track. The checked group stays.
  const clearFocus = useCallback(() => {
    setFocusedActivityId(null);
  }, []);

  // The master checkbox while fully checked: empties the group; the focus stays.
  const clearSelection = useCallback(() => {
    setCheckedActivityIds(new Set());
  }, []);

  // The master checkbox while unchecked or partial: checks every row listed, so never a row the
  // filters hide. No fly; Focus on map does that.
  const selectAll = useCallback(() => {
    setCheckedActivityIds(new Set(filteredActivities.map((a) => a.id)));
  }, [filteredActivities]);

  // Inverts the check over the listed rows; a checked row the filters hide is dropped. No fly.
  const invertSelection = useCallback(() => {
    setCheckedActivityIds(invertedChecks(filteredActivities, checkedActivityIds));
  }, [filteredActivities, checkedActivityIds]);

  // Focus on map: flies to the toolbar's target, hidden tracks excluded. The one way to look at a checked group.
  const showSelected = useCallback(() => {
    const visible = activities.filter((a) => toolbarTargetIds.has(a.id) && !mapHiddenIds.has(a.id));
    fitToSelection(visible);
  }, [activities, toolbarTargetIds, mapHiddenIds, fitToSelection]);

  // Read by the range fly below without being its dependency: the fly is keyed on the range, but
  // must still leave out tracks that aren't drawn.
  const mapHiddenIdsRef = useRef(mapHiddenIds);
  useEffect(() => {
    mapHiddenIdsRef.current = mapHiddenIds;
  }, [mapHiddenIds]);

  // Camera moves on a newly loaded list:
  // - The first list's fit is cameraRules.ts's firstListFit.
  // - After that, only a range the user picked or a Story opened from the map flies, to what's visible.
  // Keyed on the query the list was fetched for, because `activities` is a new array on every reload:
  // the Pending poll, a finished upload or a Private location change must not move the camera.
  // Arriving on one activity (`/?activity=`) flies to it instead, so the first-list fly stands aside.
  const hasFlownToActivitiesRef = useRef(initialActivity !== null);
  const flownRangeKeyRef = useRef<string | null>(null);
  useEffect(() => {
    if (!map || activitiesLoadedKey === null) return;
    if (activitiesLoadedKey === flownRangeKeyRef.current) return;
    flownRangeKeyRef.current = activitiesLoadedKey;
    if (!hasFlownToActivitiesRef.current) {
      const fit = firstListFit(activities, storyId, initial.hash.view != null);
      if (fit === null) return;
      // The first list, even an empty one: an account with no history gets the fallback view
      // below, and its first upload doesn't fly either.
      hasFlownToActivitiesRef.current = true;
      if (fit.length > 0) fitToSelection(fit);
      return;
    }
    if (!flyToNextRangeRef.current) return;
    flyToNextRangeRef.current = false;
    const visible = activities.filter((a) => !mapHiddenIdsRef.current.has(a.id));
    const flyBounds = unionBBox(visible.flatMap((a) => (a.bbox ? [a.bbox] : [])));
    if (flyBounds) flyToBBox(map, flyBounds);
  }, [map, activities, activitiesLoadedKey, fitToSelection, storyId]);

  // An account with no history at all: the account's Country view if set, else WORLD_VIEW; never geolocation.
  const hasFlownToFallbackRef = useRef(false);
  useEffect(() => {
    if (!map || hasFlownToFallbackRef.current) return;
    if (!needsFallbackView(initial.hash.view != null, activities.length, daysReady, earliest)) return;
    hasFlownToFallbackRef.current = true;
    flyToView(map, countryView(user.country) ?? WORLD_VIEW);
  }, [map, activities, daysReady, earliest, user.country]);

  // A click on a track focuses it as a row click does; a click away clears the focus and leaves
  // the checked group. While the Edit window is open the handlers go quiet: its group and focus must
  // stay, and during a track session every click is aimed at a point.
  useEffect(() => {
    setTrackInteractivityHandlers(
      editOpen
        ? { onSelect: () => {}, onHover: () => {}, onClickAway: () => {} }
        : { onSelect: focusActivity, onHover: setHoveredActivityId, onClickAway: clearFocus },
    );
  }, [focusActivity, clearFocus, editOpen]);

  const spots = useSpots(map, spotsShown);

  // Only the focused track is bold; checked tracks aren't.
  useEffect(() => {
    if (map) setSelectedTracks(map, focusedActivityId !== null ? [focusedActivityId] : []);
  }, [map, focusedActivityId]);

  useEffect(() => {
    if (map) setHoveredTrack(map, hoveredActivityId);
  }, [map, hoveredActivityId]);

  useEffect(() => {
    if (map) setHiddenTracks(map, [...mapHiddenIds]);
  }, [map, mapHiddenIds]);

  // Fetches the focused activity's per-vertex speed. Until it lands the previous activity's may be
  // in hand, so drawing checks `activityId` against the focus.
  useEffect(() => {
    if (!focusedActivityId) {
      setTrackMetrics(null);
      return;
    }
    const controller = new AbortController();
    getActivityTrackMetrics(focusedActivityId, controller.signal)
      .then(setTrackMetrics)
      .catch(() => {
        if (!controller.signal.aborted) setTrackMetrics(null);
      });
    return () => controller.abort();
  }, [focusedActivityId, trackMetricsVersion]);

  // Draws the bands from data in hand. Not while the focused track is hidden: the bands are a
  // layer above the track and would keep painting it. Not while its edit is pending: they'd paint
  // the old shape over the new one.
  const bands = useMemo(() => {
    if (focusedActivityId === null || trackMetrics?.activityId !== focusedActivityId) return null;
    if (mapHiddenIds.has(focusedActivityId) || pendingIds.includes(focusedActivityId)) return null;
    return trackMetrics;
  }, [trackMetrics, focusedActivityId, mapHiddenIds, pendingIds]);
  useEffect(() => {
    if (!map) return;
    if (bands) setTrackBands(map, bands.points);
    else clearTrackBands(map);
  }, [map, bands]);

  // Fog and Heatmap are re-rendered by queued jobs, so after an upload or delete this waits for the
  // account's jobs to drain and then refetches both (useCoverageRefresh.ts). `coverageRendering`
  // drives the notice that they may be incomplete.
  const { watch: watchCoverage, rendering: coverageRendering } = useCoverageRefresh(map);

  // A finished import or a delete: tracks, list, days, the open Story and coverage all refresh.
  const handleUploaded = useCallback(() => {
    if (map) refreshTrackLayer(map, activityQuery);
    reloadActivities();
    reloadDays();
    storyState.reload();
    watchCoverage();
  }, [map, activityQuery, reloadActivities, reloadDays, storyState.reload, watchCoverage]);

  // A Story copy that arrived (FR-14.7) is new activities, refreshed as an import is.
  const storiesReload = storiesList.reload;
  const handleCopyArrived = useCallback(() => {
    storiesReload();
    handleUploaded();
  }, [storiesReload, handleUploaded]);
  const storyInbox = useStoryInbox(panelTab === 'stories', handleCopyArrived);

  // The header's Upload menu (static/upload.js) says when an upload or import has landed.
  useEffect(() => {
    window.addEventListener('hmt:imports-changed', handleUploaded);
    return () => window.removeEventListener('hmt:imports-changed', handleUploaded);
  }, [handleUploaded]);

  const fileDrop = useFileDrop(!isDemo);

  // Deleted activities: the upload refresh (the slider's days change too), once per batch, and
  // their ids dropped from the selection. The focus matters most: left on a deleted id, the bands
  // would keep fetching its metrics.
  const handleActivitiesDeleted = useCallback(
    (ids: string[]) => {
      handleUploaded();
      const deleted = new Set(ids);
      setFocusedActivityId((current) => (current !== null && deleted.has(current) ? null : current));
      setCheckedActivityIds((current) => withoutIds(current, deleted));
      setHiddenActivityIds((current) => withoutIds(current, deleted));
    },
    [handleUploaded],
  );

  const openEditWindow = useCallback((group: Activity[]) => {
    setHoveredActivityId(null);
    setEditWindowIds(group.map((a) => a.id));
  }, []);

  // Opening or closing a Story resets the list state as a new range does; the rows are a
  // different set. Neither touches the date range.
  const resetListState = useCallback(() => {
    setCheckedActivityIds(new Set());
    setFocusedActivityId(null);
    setHiddenActivityIds(new Set());
    setExcludedTypes(new Set());
    setDistanceFilter(null);
  }, []);
  const enterStory = useCallback(
    (id: string, nav: StoryNav = 'push') => {
      // Fits the camera to the Story once its list lands.
      flyToNextRangeRef.current = true;
      resetListState();
      setStoryId(id);
      setPanelTab('stories');
      moveStoryUrl(id, nav);
    },
    [resetListState],
  );
  const exitStory = useCallback(
    (nav: StoryNav = 'push') => {
      resetListState();
      setStoryId(null);
      // The list returns to the date range; the camera stays.
      flyToNextRangeRef.current = false;
      moveStoryUrl(null, nav);
    },
    [resetListState],
  );
  // Leaving the Stories tab closes its Story; opening it opens the newest (the effect below).
  const changePanelTab = useCallback(
    (next: PanelTab) => {
      if (next !== 'stories' && storyId !== null) exitStory();
      setPanelTab(next);
    },
    [storyId, exitStory],
  );
  // One Story is always open on the Stories tab: the newest, until another is picked.
  useEffect(() => {
    if (panelTab !== 'stories' || storyId !== null || !storiesList.ready) return;
    const newest = storiesList.stories[0];
    if (newest) enterStory(newest.id);
  }, [panelTab, storyId, storiesList.ready, storiesList.stories, enterStory]);
  // Back and Forward between Stories, and to and from the tab.
  const storyNavRef = useRef({ storyId, enterStory, exitStory });
  storyNavRef.current = { storyId, enterStory, exitStory };
  useEffect(() => {
    const onPopState = () => {
      const { storyId: current, enterStory: enter, exitStory: exit } = storyNavRef.current;
      const next = storyParam(window.location.search);
      if (next === current) return;
      if (next !== null) {
        enter(next, 'none');
        return;
      }
      exit('none');
      setPanelTab('activities');
    };
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, []);

  // A Story just made with Add to story's "New story…" opens on the Stories tab.
  const openCreatedStory = useCallback((story: Story) => enterStory(story.id), [enterStory]);
  // Activities added to an existing Story: the list reloads for the rows' badges. The server moved
  // the tile version, which only `watchCoverage` brings to this page; without it the Story could
  // later draw from stale cached tiles.
  const handleStoryAdded = useCallback(
    (story: Story) => {
      storiesList.replace(story);
      reloadActivities();
      watchCoverage();
    },
    [storiesList.replace, reloadActivities, watchCoverage],
  );

  const handleStoryEdited = useCallback(
    (story: Story) => {
      storiesList.replace(story);
      storyState.set(story); // only if it's still the open one (useStory)
    },
    [storiesList.replace, storyState.set],
  );
  // A deleted Story's activities stay. Deleting the open one opens the next newest in its place in
  // history; deleting the last leaves none open.
  const handleStoryDeleted = useCallback(
    (id: string) => {
      storiesList.remove(id);
      if (id !== storyId) return;
      const next = storiesList.stories.find((s) => s.id !== id);
      if (next) enterStory(next.id, 'replace');
      else exitStory('replace');
    },
    [storiesList.remove, storiesList.stories, storyId, enterStory, exitStory],
  );

  // An activity taken out of the open Story (FR-14.6): new totals, the row gone, and the tracks
  // redrawn at the tile version the removal moved to.
  const handleStoryActivityRemoved = useCallback(
    (story: Story, activityId: string) => {
      handleStoryEdited(story);
      setFocusedActivityId((id) => (id === activityId ? null : id));
      setHoveredActivityId((id) => (id === activityId ? null : id));
      reloadActivities();
      watchCoverage(() => {
        if (map) refreshTrackLayer(map, activityQuery);
      });
    },
    [handleStoryEdited, reloadActivities, watchCoverage, map, activityQuery],
  );

  const storiesPanel: StoriesPanel = useMemo(
    () => ({
      stories: storiesList.stories,
      ready: storiesList.ready,
      error: storiesList.error,
      inbox: storyInbox,
      openId: storyId,
      openStory: storyState.story,
      openError: storyState.error,
      onOpen: (id: string) => enterStory(id),
      onEdited: handleStoryEdited,
      onDeleted: handleStoryDeleted,
      onActivityRemoved: handleStoryActivityRemoved,
    }),
    [
      storiesList.stories,
      storiesList.ready,
      storiesList.error,
      storyInbox,
      storyId,
      storyState.story,
      storyState.error,
      enterStory,
      handleStoryEdited,
      handleStoryDeleted,
      handleStoryActivityRemoved,
    ],
  );
  // The Track tab's session (§4.7.7): flies to the activity, then TrackEditor owns the map until the window closes.
  const startEditTrack = useCallback(
    (activity: Activity) => {
      setHoveredActivityId(null);
      setEditingActivityId(activity.id);
      fitToSelection([activity]);
    },
    [fitToSelection],
  );
  // Activities whose track edit was applied here and whose finished reprocess isn't picked up yet
  // (the completion effect below).
  const awaitingEditIdsRef = useRef<Set<string>>(new Set());
  const closeEditWindow = useCallback(
    ({ saved, trackApplied, photosSaved }: EditWindowResult) => {
      if (trackApplied && editingActivityId !== null) awaitingEditIdsRef.current.add(editingActivityId);
      setEditWindowIds(null);
      setEditingActivityId(null);
      // A track edit leaves the row Pending; the effects below poll until the reprocess lands.
      if (saved) reloadActivities();
      if (photosSaved) photoState.reload();
    },
    [editingActivityId, reloadActivities, photoState.reload],
  );
  // A saved or deleted Private location reprocesses every activity it could clip. The reload shows
  // them Pending and the Pending poll refreshes the map, but a batch that finishes before the reload
  // returns is never seen Pending, so the coverage watch refreshes everything once the server drains.
  const handlePrivateLocationsChanged = useCallback(() => {
    reloadActivities();
    watchCoverage(() => {
      if (map) refreshTrackLayer(map, activityQuery);
      reloadActivities();
      reloadDays();
      storyState.reload();
      setTrackMetricsVersion((v) => v + 1);
    });
  }, [map, activityQuery, reloadActivities, reloadDays, storyState.reload, watchCoverage]);
  const editWindowActivities = useMemo(() => {
    if (editWindowIds === null) return null;
    const group = activities.filter((a) => editWindowIds.includes(a.id));
    return group.length === editWindowIds.length ? group : null;
  }, [activities, editWindowIds]);
  // The window closes when its activities leave the list (deleted elsewhere, say); otherwise the
  // map would stay locked with no editor to close it.
  useEffect(() => {
    if (editOpen && editWindowActivities === null && !activitiesLoading) {
      setEditWindowIds(null);
      setEditingActivityId(null);
    }
  }, [editOpen, editWindowActivities, activitiesLoading]);
  const editTrackKey = editTrackBlocker(editWindowActivities);
  const editTrackUnavailable = editTrackKey === null ? null : t(editTrackKey);
  const editPhotosKey = editPhotosBlocker(editWindowActivities);
  const editPhotosUnavailable = editPhotosKey === null ? null : t(editPhotosKey);
  // The Edit window opens on its Activity tab, with nothing being moved on the map.
  useEffect(() => {
    setEditTab('activity');
    photoLayer.setOverlay(null);
  }, [editOpen]);

  // While any row is pending, re-read the list. Keyed on each fetched list, so polling stops by
  // itself once nothing is pending.
  useEffect(() => {
    if (pendingIds.length === 0) return;
    const timer = window.setTimeout(reloadActivities, EDIT_PENDING_POLL_MS);
    return () => window.clearTimeout(timer);
  }, [pendingIds, reloadActivities]);

  // A row that leaves Pending has new points: the track, the days, the open Story and the bands
  // refresh. The camera stays (FR-5.15). Fog and Heatmap go through the coverage watch, because the
  // server clears Pending just before its last render; a row that enters Pending starts the same watch.
  // An edit applied here counts as finished once a later list shows it not pending (pending.ts).
  // Each fetched list is a new `pendingIds` array, and Apply's fetch aborts earlier ones, so every
  // list seen after Apply was fetched after it.
  const previousPendingRef = useRef<ReadonlySet<string>>(new Set());
  const lastPendingIdsRef = useRef<string[] | null>(null);
  useEffect(() => {
    // Only a newly fetched list says anything new; the map or range changing re-runs this on an older list.
    if (pendingIds === lastPendingIdsRef.current) return;
    lastPendingIdsRef.current = pendingIds;
    const change = pendingChange(previousPendingRef.current, pendingIds, awaitingEditIdsRef.current);
    previousPendingRef.current = change.now;
    awaitingEditIdsRef.current = change.awaiting;
    if (change.started) {
      // Drawn from tiles fetched before it went Pending; the server now leaves it out.
      if (map) refreshTrackLayer(map, activityQuery);
      watchCoverage();
    }
    if (!change.finished) return;
    if (map) refreshTrackLayer(map, activityQuery);
    // Includes the Country/Region tiers: an edit can un-visit a region.
    watchCoverage();
    reloadDays();
    storyState.reload();
    setTrackMetricsVersion((v) => v + 1);
  }, [pendingIds, map, activityQuery, reloadDays, storyState.reload, watchCoverage]);

  useMapLayers(map, {
    initialFlavor: initial.flavor,
    flavor,
    satellite,
    mode: mapMode,
    editingTrack,
    paths,
    spotsShown,
    activityQuery,
    mapHiddenIds,
    focusedId: focusedActivityId,
    bands,
  });
  useHashSync(map, pinned);

  return (
    <div className="app-shell">
      <div className="app-body">
        {mapMode === 'normal' && (
          // Inert while the Edit window is open: changing the range, selection or filters would pull
          // its activities out from under it. display: contents keeps the wrapper out of the layout.
          <div className="edit-track-lock" inert={editOpen}>
            <ActivitiesPanel
              readOnly={isDemo}
              activities={filteredActivities}
              loading={activitiesLoading}
              error={activitiesError}
              facets={facets}
              excludedTypes={excludedTypes}
              onToggleType={toggleType}
              distanceBounds={activityDistanceBounds}
              distanceFilter={distanceFilter}
              onChangeDistance={setDistanceFilter}
              onResetFilters={resetActivityFilters}
              checked={checkedActivityIds}
              targetIds={toolbarTargetIds}
              focusedId={focusedActivityId}
              hoveredId={hoveredActivityId}
              onToggle={toggleActivityChecked}
              onFocus={focusActivity}
              onClearFocus={clearFocus}
              onHoverActivity={setHoveredActivityId}
              onClear={clearSelection}
              onSelectAll={selectAll}
              onInvertSelection={invertSelection}
              onShowSelected={showSelected}
              hiddenIds={hiddenActivityIds}
              onToggleGroupVisibility={toggleGroupVisibility}
              onActivitiesDeleted={handleActivitiesDeleted}
              onEdit={openEditWindow}
              onStoryCreated={openCreatedStory}
              onStoryAdded={handleStoryAdded}
              stories={storiesPanel}
              tab={panelTab}
              onTabChange={changePanelTab}
              map={map}
              onPrivateLocationsChanged={handlePrivateLocationsChanged}
              dateRange={{
                days: visibleDays,
                onPan: panBy,
                canPanEarlier,
                canPanLater,
                onShift: shiftDays,
                canShift,
                value: selectedRange ?? { from: today, to: today },
                onChange: changeSelectedRange,
              }}
            />
          </div>
        )}

        <div
          className={exporter.flow.stage === 'idle' ? 'map-root' : 'map-root map-root--exporting'}
          {...fileDrop.handlers}
        >
          {fileDrop.over && (
            <div className="map-drop" data-testid="map-drop">
              <span className="map-drop__label">{t('map.drop_files')}</span>
            </div>
          )}
          <div ref={container} className="map-canvas" data-testid="map-canvas" />
          {map && <ExportControl map={map} active={exporter.flow.stage !== 'idle'} onOpen={exporter.toggle} />}
          {map && exporter.flow.stage !== 'idle' && (
            <ExportViewfinder
              map={map}
              containerRef={container}
              preset={exporter.flow.preset}
              customSize={exporter.flow.customSize}
              maxSidePx={exporter.maxSidePx}
              window={exporter.flow.window}
              busy={exporter.flow.stage === 'capturing'}
              error={exporter.error}
              onWindowChange={exporter.moveWindow}
              onChoiceChange={exporter.changeChoice}
              onCapture={() => void exporter.capture()}
              onCancel={exporter.cancel}
            />
          )}
          {map && editWindowActivities && (
            <EditActivityWindow
              map={map}
              activities={editWindowActivities}
              knownTypes={facets}
              trackUnavailable={editTrackUnavailable}
              onStartTrack={startEditTrack}
              photosUnavailable={editPhotosUnavailable}
              photos={{
                photos: photoState.photos,
                error: photoState.error,
                onOverlay: photoLayer.setOverlay,
              }}
              onTabChange={setEditTab}
              onClose={closeEditWindow}
            />
          )}
          {map && photoLayer.popup && (
            <PhotoPopup
              map={map}
              photos={photoLayer.popup.photos}
              index={photoLayer.popup.index}
              onIndex={photoLayer.showPopupIndex}
              onClose={photoLayer.closePopup}
            />
          )}
          {!editOpen && (
            <div className="map-toggles">
              <div className="map-mode-toggle" role="group" aria-label={t('map.mode')} data-testid="map-mode-toggle">
                <button
                  type="button"
                  className={mapMode === 'normal' ? 'map-mode-toggle__btn map-mode-toggle__btn--active' : 'map-mode-toggle__btn'}
                  aria-pressed={mapMode === 'normal'}
                  onClick={() => changeMapMode('normal')}
                >
                  {t('map.mode_normal')}
                </button>
                {/* Normal on one side, the two coverage views on the other. */}
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
              {/* Layers switch on and off over any mode; they're not a fourth mode. */}
              <OverlaysMenu overlays={overlays} onChange={changeOverlays} flavor={flavor} />
              {satelliteAvailable && (
                <BasemapToggle satellite={overlays.satellite} onChange={(on) => changeOverlays({ ...overlays, satellite: on })} />
              )}
              {/* On lines of their own under the toggles, however many rows they wrap to. */}
              {mapMode !== 'normal' && coverageRendering && (
                <div className="coverage-notice" role="status" data-testid="coverage-notice">
                  <span className="coverage-notice__pill">{t('map.coverage_updating')}</span>
                </div>
              )}
              {map && <ZoomLevelNotice map={map} mode={mapMode} />}
            </div>
          )}
          {map && !editOpen && <ShowInArea map={map} categories={spotsShown} />}
          {map && spots.openSpot && <SpotPopup map={map} spot={spots.openSpot} capturedAt={spots.capturedAt(spots.openSpot.id)} onClose={spots.closeSpot} />}
        </div>
      </div>

    </div>
  );
}
