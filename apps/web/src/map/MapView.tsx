import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { flyToBBox, flyToView, unionBBox } from './bbox';
import { basemapOrigin, satelliteSource, WORLD_VIEW } from './config';
import { countryView } from './countryView';
import { exportFramedImage, maxExportSide } from './exportMap';
import { DEFAULT_CUSTOM_SIZE, fitWindow, outputSize, usableCustomSize, viewfinderArea, type CustomSizeId, type ExportChoice, type Rect } from './exportViewfinder';
import type { ExportPreset } from './exportPresets';
import { ensureBikePathLayers, setBikePathsVisible } from './bikePaths';
import { ensureFogLayer } from './fog';
import { ensureHeatmapLayer } from './heatmap';
import { setMapMode, type MapMode } from './mapMode';
import { loadOverlays, saveOverlays, type Overlays } from './overlays';
import { raisePathLayers } from './paths';
import { usePhotoMarkers, type PhotoMarkerItem, type PhotoMarkerOverlay } from './photos';
import { setSatelliteVisible } from './satellite';
import { capturesSince, mergeSpotCaptures } from './spotCaptures';
import { ensureSpotsLayer, setSpotClickHandler, setSpotsCaptured, setSpotsVisible, type Spot, type SpotCategory } from './spots';
import { labelInsertionPoint } from './layers';
import { buildStyle, isDarkBase, type Flavor } from './style';
import { clearTrackBands, ensureBandLayer, setTrackBands } from './trackBands';
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
import { flavorForTheme, parseHash, pinnedFlavor, replaceHash, type HashState, type ViewState } from './viewState';
import { API_BASE_URL, getActivityTrackMetrics, getSpotCaptures, type Activity, type ActivityTrackMetrics, type SpotCapture, type Story } from '../api';
import { useAuth } from '../auth/AuthContext';
import { distanceBounds, passesFilters, typeFacets, type DistanceRange } from '../ui/activityFacets';
import { ActivitiesPanel, type PanelTab, type StoriesPanel } from '../ui/ActivitiesPanel';
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
import { usePhotos, type PhotoScope } from '../ui/usePhotos';
import { useStories } from '../ui/useStories';
import { useStoryInbox } from '../ui/useStoryInbox';
import { useStory } from '../ui/useStory';
import { currentTheme, useTheme } from '../ui/useTheme';
import { lang, t } from '../i18n';

/** How often the list is re-read while a reprocess is pending: one activity's ingest takes seconds. */
const EDIT_PENDING_POLL_MS = 2000;

/** The Story the URL opens, `/?story=<id>` (FR-14.6). Kept in the URL so a refresh or a shared link returns to it. */
function storyParam(): string | null {
  return new URLSearchParams(window.location.search).get('story') || null;
}

/** `/?tab=stories` opens the Stories tab with no Story picked; the copy-arrived email links here (FR-14.7). */
function storiesTabParam(): boolean {
  return new URLSearchParams(window.location.search).get('tab') === 'stories';
}

/** The current URL with `?story=` set, or removed for null; the hash and other parameters kept. */
function urlWithStory(id: string | null): string {
  const params = new URLSearchParams(window.location.search);
  if (id === null) params.delete('story');
  else params.set('story', id);
  const qs = params.toString();
  return window.location.pathname + (qs ? `?${qs}` : '') + window.location.hash;
}

/** What opening or closing a Story does to history: push, replace, or nothing (Back/Forward already moved it). */
type StoryNav = 'push' | 'replace' | 'none';

function moveStoryUrl(id: string | null, nav: StoryNav) {
  if (nav === 'push') window.history.pushState(null, '', urlWithStory(id));
  else if (nav === 'replace') window.history.replaceState(null, '', urlWithStory(id));
}

export interface MapViewProps {
  /** Mount with the Activities panel on its Privacy tab: `/?private-locations` (App.tsx). */
  initialPrivateLocationsOpen?: boolean;
  /** Mount on one activity, its day selected and it focused: `/?activity=&day=`, the /sync page's "View on map". */
  initialActivity?: { id: string; day: string } | null;
}

/** The export viewfinder's state. Lives here because it spans `ExportViewfinder`, `ExportControl` and the map
 *  instance capture needs. The window is in the map container's CSS pixels. */
type ExportFlow =
  | { stage: 'idle' }
  | { stage: 'framing' | 'capturing'; preset: ExportPreset | 'custom'; customSize: CustomSizeId; window: Rect };

/** A shape's window: the largest it can be in the map's free area. Custom takes the whole area. */
function shapeWindow(preset: ExportPreset | 'custom', container: HTMLElement): Rect {
  return fitWindow(preset === 'custom' ? null : preset.widthPx / preset.heightPx, viewfinderArea(container.clientWidth, container.clientHeight));
}

/** How far apart, in metres, a group's photos must be for zooming in to separate them (§4.27). */
const GROUP_SPREAD_M = 15;

function haversineM(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const rad = Math.PI / 180;
  const h = Math.sin(((lat2 - lat1) * rad) / 2) ** 2 + Math.cos(lat1 * rad) * Math.cos(lat2 * rad) * Math.sin(((lon2 - lon1) * rad) / 2) ** 2;
  return 2 * 6_371_008.8 * Math.asin(Math.min(1, Math.sqrt(h)));
}

export function MapView({ initialPrivateLocationsOpen = false, initialActivity = null }: MapViewProps) {
  // A demo account is read-only (docs/SPEC.md FR-2.1); `'email' in user` tells a DemoUser from an AuthUser.
  const { user } = useAuth();
  const isDemo = !('email' in user);

  // Read once per mount. Every sign-in, sign-up, demo start and sign-out ends in a redirect to a
  // bare `/`, so a previous session's camera can't carry over (IMPLEMENTATION.md §4.19).
  const [initial] = useState<{ hash: HashState; view: ViewState; flavor: Flavor }>(() => {
    const hash = parseHash(window.location.hash);
    return {
      hash,
      view: hash.view ?? { longitude: WORLD_VIEW.longitude, latitude: WORLD_VIEW.latitude, zoom: WORLD_VIEW.zoom },
      flavor: pinnedFlavor(hash) ?? flavorForTheme(currentTheme()),
    };
  });

  // The basemap flavor follows the page theme and swaps live. A URL can pin another flavor until
  // the theme next changes: whoever switched the theme expects the map to follow.
  const theme = useTheme();
  const [pinned, setPinned] = useState(() => pinnedFlavor(initial.hash));
  const initialTheme = useRef(theme);
  useEffect(() => {
    if (theme !== initialTheme.current) setPinned(undefined);
  }, [theme]);
  const flavor: Flavor = pinned ?? flavorForTheme(theme);
  // Read by the hash sync's moveend listener, which outlives any one render.
  const pinnedRef = useRef(pinned);
  pinnedRef.current = pinned;

  const container = useRef<HTMLDivElement>(null);
  const [exportFlow, setExportFlow] = useState<ExportFlow>({ stage: 'idle' });
  const [exportError, setExportError] = useState<string | null>(null);
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
  const [openSpot, setOpenSpot] = useState<Spot | null>(null);
  // Places this account captured with the Android app (FR-15.6): badged, and dated in their popup.
  const [spotCaptures, setSpotCaptures] = useState<readonly SpotCapture[]>([]);

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
      : storyParam() !== null || storiesTabParam()
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
  const [storyId, setStoryId] = useState<string | null>(storyParam);
  const storyState = useStory(storyId);

  // Photos (FR-16), in Normal mode only: the Edit window's single activity, else the focused
  // activity, else the open Story.
  const photoScope = useMemo<PhotoScope>(() => {
    if (mapMode !== 'normal') return null;
    if (editWindowIds !== null) return editWindowIds.length === 1 ? { activity: editWindowIds[0]! } : null;
    if (focusedActivityId !== null) return { activity: focusedActivityId };
    if (storyId !== null) return { story: storyId };
    return null;
  }, [mapMode, editWindowIds, focusedActivityId, storyId]);
  // A Story's photos change with its members too.
  const storyMembers = storyState.story?.activityIds.join(',') ?? '';
  const photoState = usePhotos(photoScope, `${trackMetricsVersion}|${storyMembers}`);
  // The photos whose popup is open, from a marker's click; closed when the scope changes.
  const [openGroup, setOpenGroup] = useState<{ ids: string[]; index: number } | null>(null);
  // Only saved photos open.
  const openPhotos = useMemo(
    () => (openGroup ? openGroup.ids.flatMap((id) => photoState.photos.filter((p) => p.id === id)) : []),
    [openGroup, photoState.photos],
  );
  // The Photos tab's unsaved changes as they alter the markers (PhotosTab.tsx).
  const [photoOverlay, setPhotoOverlay] = useState<PhotoMarkerOverlay | null>(null);
  // The Edit window's tab: its Track tab hides the photos.
  const [editTab, setEditTab] = useState<EditTab>('activity');
  const photoScopeKey = photoScope === null ? null : JSON.stringify(photoScope);
  useEffect(() => setOpenGroup(null), [photoScopeKey]);
  const storiesList = useStories(panelTab === 'stories');

  // TYPE/DISTANCE filters over the loaded activities (activityFacets.ts).
  const [excludedTypes, setExcludedTypes] = useState<Set<string>>(() => new Set());
  const [distanceFilter, setDistanceFilter] = useState<DistanceRange | null>(null);

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
  // The toolbar acts on the checked group if anything is checked, else the focused row. Checked
  // wins over a union so a Delete never reaches past what was ticked.
  const toolbarTargetIds = useMemo(() => {
    if (checkedActivityIds.size > 0) return checkedActivityIds;
    return new Set(focusedActivityId !== null ? [focusedActivityId] : []);
  }, [checkedActivityIds, focusedActivityId]);
  // Show/Hide over the toolbar's target: if any of it is hidden, show all; otherwise hide all.
  // One click never leaves the group mixed.
  const toggleGroupVisibility = useCallback(() => {
    setHiddenActivityIds((prev) => {
      const anyHidden = [...toolbarTargetIds].some((id) => prev.has(id));
      const next = new Set(prev);
      for (const id of toolbarTargetIds) {
        if (anyHidden) next.delete(id);
        else next.add(id);
      }
      return next;
    });
  }, [toolbarTargetIds]);
  // A toggle: the type stays in the dropdown either way, so it can be turned back on without Reset filters.
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
  const activityQuery = useMemo(
    () =>
      storyId !== null
        ? { story: storyId }
        : selectedRange
          ? { from: selectedRange.from, to: selectedRange.to }
          : {},
    [selectedRange, storyId],
  );

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

  // The default range is the 5 most recent activity-days: what opening the map asks about, quick
  // to draw, and both knobs inside the slider's window.
  //
  // It is re-derived on every real refetch of days (`historyGeneration`) until the user picks a
  // range. Trap: keying on `visibleDays` would also re-pick on Earlier/Later paging, and settling
  // once would freeze a zero-history account (every demo) on `{today, today}` past its first upload.
  //
  // Held while the Edit window is open: an import landing newer days would slide the default past
  // the edited activity and close the window. It catches up when the window closes.
  useEffect(() => {
    if (userChangedRangeRef.current || !daysReady || editOpen) return;
    const recentDays = visibleDays.slice(-5);
    setSelectedRangeState({ from: recentDays[0]?.date ?? earliest ?? today, to: today });
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
  // Tracks not drawn and not flown to: filtered out, hidden, or Pending. The tile already omits a
  // Pending activity; this hides it before the refetch lands. Pending stays out of
  // hiddenActivityIds so the user's own choice survives the reprocess.
  const mapHiddenIds = useMemo(() => {
    const filteredOut = activities.filter((a) => !passesFilters(a, excludedTypes, distanceFilter)).map((a) => a.id);
    return new Set([...filteredOut, ...hiddenActivityIds, ...pendingIds]);
  }, [activities, excludedTypes, distanceFilter, hiddenActivityIds, pendingIds]);

  const unitSystem = useUnitSystem();
  const map = useMapInstance({
    container,
    initialView: initial.view,
    initialFlavor: initial.flavor,
    initialSatellite: satellite,
    scaleUnit: unitSystem,
  });

  // The Custom size last picked, kept for the next time the viewfinder opens in this visit.
  const customSizeRef = useRef<CustomSizeId>(DEFAULT_CUSTOM_SIZE);
  const exportMaxSide = useMemo(() => (map ? maxExportSide(map) : 4096), [map]);

  const handleExportOpen = useCallback(() => {
    const el = container.current;
    if (!map || !el) return;
    setExportError(null);
    // The control toggles the viewfinder, except mid-capture.
    setExportFlow((flow) => {
      if (flow.stage === 'framing') return { stage: 'idle' };
      if (flow.stage === 'capturing') return flow;
      return { stage: 'framing', preset: 'custom', customSize: usableCustomSize(customSizeRef.current, exportMaxSide), window: shapeWindow('custom', el) };
    });
  }, [map, exportMaxSide]);

  // Another Custom size keeps the window; any other choice fits a window of the new shape.
  const handleExportChoiceChange = useCallback((choice: ExportChoice) => {
    const el = container.current;
    if (!el) return;
    if (choice.preset === 'custom') customSizeRef.current = choice.customSize;
    setExportFlow((flow) => {
      if (flow.stage !== 'framing') return flow;
      if (choice.preset === 'custom' && flow.preset === 'custom') return { ...flow, customSize: choice.customSize };
      return { ...flow, ...choice, window: shapeWindow(choice.preset, el) };
    });
  }, []);

  const handleExportCapture = useCallback(async () => {
    if (exportFlow.stage !== 'framing' || !map) return;
    const { preset, customSize, window: win } = exportFlow;
    setExportFlow({ ...exportFlow, stage: 'capturing' });
    setExportError(null);
    try {
      const center = map.unproject([win.x + win.w / 2, win.y + win.h / 2]);
      const blob = await exportFramedImage(
        map,
        { flavor, mode: mapMode, activityQuery, hiddenIds: [...mapHiddenIds], paths, satellite },
        { center: { lng: center.lng, lat: center.lat }, widthPx: win.w, heightPx: win.h, target: outputSize(preset, customSize, win) },
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
      // The cause (a canvas or render timeout) is for developers; the user gets a generic message.
      console.error('export failed', err);
      setExportError(t('export.failed'));
      // Back to framing, so a failed capture keeps the framed view.
      setExportFlow((flow) => (flow.stage === 'capturing' ? { ...flow, stage: 'framing' } : flow));
    }
  }, [exportFlow, map, flavor, mapMode, activityQuery, mapHiddenIds, paths, satellite]);

  const handleExportCancel = useCallback(() => {
    setExportFlow({ stage: 'idle' });
    setExportError(null);
  }, []);

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
    const inverted = filteredActivities.filter((a) => !checkedActivityIds.has(a.id));
    setCheckedActivityIds(new Set(inverted.map((a) => a.id)));
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
  // - The first list flies to the most recent activity (a whole Story inside one), unless the URL
  //   carries a view (FR-4.5). Fitting the whole default range could zoom out to the world.
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
      const drawn = activities.filter((a) => a.bbox !== null && !a.pending);
      // A Story with nothing to draw leaves this for the date range, which closing it returns to.
      if (storyId !== null && drawn.length === 0) return;
      // The first list, even an empty one: an account with no history gets the fallback view
      // below, and its first upload doesn't fly either.
      hasFlownToActivitiesRef.current = true;
      if (initial.hash.view == null && drawn.length > 0) {
        // A Story is a trip: show the whole of it.
        const mostRecent = drawn.reduce((latest, a) => (a.startedAt > latest.startedAt ? a : latest));
        fitToSelection(storyId !== null ? drawn : [mostRecent]);
      }
      return;
    }
    if (!flyToNextRangeRef.current) return;
    flyToNextRangeRef.current = false;
    const visible = activities.filter((a) => !mapHiddenIdsRef.current.has(a.id));
    const flyBounds = unionBBox(visible.flatMap((a) => (a.bbox ? [a.bbox] : [])));
    if (flyBounds) flyToBBox(map, flyBounds);
  }, [map, activities, activitiesLoadedKey, fitToSelection, storyId]);

  // An account with no history at all: the account's Country view if set, else WORLD_VIEW; never
  // geolocation. Trap: `activities` is empty for any range without activity, so "no history" is
  // read from useActivityDays' `earliest`.
  const hasFlownToFallbackRef = useRef(false);
  useEffect(() => {
    if (!map || hasFlownToFallbackRef.current) return;
    if (initial.hash.view != null) return;
    if (activities.length > 0) return;
    if (!daysReady || earliest != null) return;
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

  // A click on a spot opens its popup; hiding its category closes it.
  useEffect(() => {
    setSpotClickHandler(setOpenSpot);
  }, []);
  // Captures are made on the phone, so they're re-read when the page regains visibility or focus.
  // The first read takes the whole list; later ones only what's newer (spotCaptures.ts).
  useEffect(() => {
    let controller: AbortController | null = null;
    let known: readonly SpotCapture[] | null = null;
    const load = () => {
      controller?.abort();
      controller = new AbortController();
      const since = known ? capturesSince(known) : undefined;
      getSpotCaptures(controller.signal, since).then(
        (fresh) => {
          known = known ? mergeSpotCaptures(known, fresh) : fresh;
          setSpotCaptures(known);
        },
        () => {
          // Offline or aborted: the badges stay as they were.
        },
      );
    };
    const onVisible = () => {
      if (document.visibilityState === 'visible') load();
    };
    load();
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('focus', load);
    return () => {
      controller?.abort();
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('focus', load);
    };
  }, []);
  useEffect(() => {
    if (map) setSpotsCaptured(map, spotCaptures.map((c) => c.spot_id));
  }, [map, spotCaptures]);
  useEffect(() => {
    if (map) setSpotsVisible(map, spotsShown);
    setOpenSpot((open) => (open && !spotsShown.includes(open.category) ? null : open));
  }, [map, spotsShown]);

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
  const focusedPending = useMemo(
    () => focusedActivityId !== null && pendingIds.includes(focusedActivityId),
    [focusedActivityId, pendingIds],
  );
  useEffect(() => {
    if (!map) return;
    if (trackMetrics?.activityId === focusedActivityId && focusedActivityId !== null && !mapHiddenIds.has(focusedActivityId) && !focusedPending) {
      setTrackBands(map, trackMetrics.points);
    } else {
      clearTrackBands(map);
    }
  }, [map, trackMetrics, focusedActivityId, mapHiddenIds, focusedPending]);

  // Points the existing tracks source at the new query. Trap: `ensureTrackLayer` only seeds a new
  // source, so it can't do this.
  useEffect(() => {
    if (map) refreshTrackLayer(map, activityQuery);
  }, [map, activityQuery]);

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

  // Files dropped on the map go to the header's Upload menu. A demo account has none, so the map
  // takes no drop. The cover counts enters and leaves, since each child the drag crosses fires a pair.
  const [fileDragOver, setFileDragOver] = useState(false);
  const dragDepthRef = useRef(0);
  const carriesFiles = (event: React.DragEvent) => !isDemo && Array.from(event.dataTransfer.types).includes('Files');
  const onMapDragEnter = (event: React.DragEvent) => {
    if (!carriesFiles(event)) return;
    event.preventDefault();
    dragDepthRef.current += 1;
    setFileDragOver(true);
  };
  const onMapDragOver = (event: React.DragEvent) => {
    if (!carriesFiles(event)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = 'copy';
  };
  const onMapDragLeave = (event: React.DragEvent) => {
    if (!carriesFiles(event)) return;
    dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
    if (dragDepthRef.current === 0) setFileDragOver(false);
  };
  const onMapDrop = (event: React.DragEvent) => {
    if (!carriesFiles(event)) return;
    event.preventDefault();
    dragDepthRef.current = 0;
    setFileDragOver(false);
    window.dispatchEvent(new CustomEvent('hmt:upload-files', { detail: event.dataTransfer.files }));
  };

  // Deleted activities: the upload refresh (the slider's days change too), once per batch, and
  // their ids dropped from the selection. The focus matters most: left on a deleted id, the bands
  // would keep fetching its metrics.
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
      const next = storyParam();
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
  // The Track tab edits one activity with a finished track.
  const editTrackUnavailable =
    editWindowActivities === null || editWindowActivities.length !== 1
      ? t('activities.edit_track_check_one')
      : editWindowActivities[0]!.pending
        ? t('activities.edit_track_processing')
        : editWindowActivities[0]!.private
          ? t('activities.private_title')
          : editWindowActivities[0]!.bbox === null
            ? t('activities.no_track')
            : null;

  // Photos belong to one activity and need a track to sit on (FR-16.1).
  const editPhotosUnavailable =
    editWindowActivities === null || editWindowActivities.length !== 1
      ? t('photos.check_one')
      : editWindowActivities[0]!.private
        ? t('activities.private_title')
        : editWindowActivities[0]!.bbox === null
          ? t('photos.no_track')
          : null;
  // The Edit window opens on its Activity tab, with nothing being moved on the map.
  useEffect(() => {
    setEditTab('activity');
    setPhotoOverlay(null);
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
  //
  // Trap: the job often finishes before the list reload after Apply returns, so the row is never
  // seen Pending. An activity applied here therefore also counts as finished the first time a list
  // fetched after Apply shows it not pending. Each fetched list is a new `pendingIds` array, and
  // Apply's fetch aborts earlier ones, so every list seen after Apply was fetched after it.
  const previousPendingRef = useRef<ReadonlySet<string>>(new Set());
  const lastPendingIdsRef = useRef<string[] | null>(null);
  useEffect(() => {
    // Only a newly fetched list says anything new; the map or range changing re-runs this on an older list.
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
    // Includes the Country/Region tiers: an edit can un-visit a region.
    watchCoverage();
    reloadDays();
    storyState.reload();
    setTrackMetricsVersion((v) => v + 1);
  }, [pendingIds, map, activityQuery, reloadDays, storyState.reload, watchCoverage]);

  /** Re-adds every overlay after a style load: `setStyle` keeps the camera but drops custom layers. */
  const reattachOverlays = useCallback(
    (instance: MapLibreMap) => {
      const beforeId = labelInsertionPoint(instance);
      // Layer order matters: MapLibre paints later layers on top, all beneath the basemap labels
      // (layers.ts). Fog and heatmap first, so tracks show through the veil. Satellite imagery
      // reads dark, so it takes the dark flavors' veil (style.ts's isDarkBase).
      ensureFogLayer(instance, beforeId, isDarkBase(flavor, satellite));
      ensureHeatmapLayer(instance, beforeId, isDarkBase(flavor, satellite));
      // Trails and paths over the veil and under the activity tracks (paths.ts, bikePaths.ts).
      raisePathLayers(instance, beforeId);
      ensureBikePathLayers(instance, beforeId, flavor);
      ensureTrackLayer(instance, beforeId, activityQuery);
      // A recreated source has no feature-state, the focused track's `selected` included.
      setSelectedTracks(instance, focusedActivityId !== null ? [focusedActivityId] : []);
      // Bands after tracks, to paint over the one track they apply to (trackBands.ts).
      ensureBandLayer(instance, beforeId);
      // Spots last, above the basemap's labels too (spots.ts).
      ensureSpotsLayer(instance, spotsShown);
      // Re-applied because new layers start hidden or unfiltered: without these, a style load
      // mid-mode would drop to Normal and un-hide every hidden track. Both diff against the layer's
      // current value first (docs/DEVELOPMENT.md, the setFilter/setLayoutProperty gotcha).
      setMapMode(instance, mapMode, editingTrack);
      setBikePathsVisible(instance, paths);
      setSatelliteVisible(instance, satellite, mapMode);
      setHiddenTracks(instance, [...mapHiddenIds]);
      // The band source is recreated empty; redraw it under the same conditions as the live effect.
      if (trackMetrics?.activityId === focusedActivityId && focusedActivityId !== null && !mapHiddenIds.has(focusedActivityId) && !focusedPending) {
        setTrackBands(instance, trackMetrics.points);
      }
    },
    [flavor, satellite, mapMode, editingTrack, paths, spotsShown, mapHiddenIds, activityQuery, trackMetrics, focusedActivityId, focusedPending],
  );

  useEffect(() => {
    if (!map) return;
    const onStyleData = () => reattachOverlays(map);
    map.on('styledata', onStyleData);
    // Trap: `map` is set after 'load', and a later 'styledata' isn't guaranteed until a setStyle,
    // so attach directly too or overlays would wait for a theme switch.
    reattachOverlays(map);
    return () => {
      map.off('styledata', onStyleData);
    };
  }, [map, reattachOverlays]);

  // A theme change swaps the style in place, camera kept. Declared after the styledata effect on
  // purpose: effects run in order, so the reattach against the outgoing style happens before
  // setStyle starts loading the new one. `diff: false` drops every custom source, so no fog source
  // keeps the other theme's URL.
  const appliedFlavor = useRef(initial.flavor);
  useEffect(() => {
    if (!map || appliedFlavor.current === flavor) return;
    appliedFlavor.current = flavor;
    map.setStyle(buildStyle({ flavor, origin: basemapOrigin(), satellite: satelliteSource(), satelliteOn: satellite, lang, globe: true }), {
      diff: false,
    });
    // Only a flavor change swaps the style; the Satellite button flips layers in place (below).
  }, [map, flavor]);

  // Mode, paths and satellite changes between style loads flip the layers in place.
  useEffect(() => {
    if (!map) return;
    setMapMode(map, mapMode, editingTrack);
  }, [map, mapMode, editingTrack]);

  useEffect(() => {
    if (!map) return;
    setBikePathsVisible(map, paths);
  }, [map, paths]);

  useEffect(() => {
    if (!map) return;
    setSatelliteVisible(map, satellite, mapMode);
  }, [map, satellite, mapMode]);

  // Keeps the hash current, once per gesture (`moveend`), not per frame.
  useEffect(() => {
    if (!map) return;
    const sync = () => {
      const centre = map.getCenter();
      replaceHash({ longitude: centre.lng, latitude: centre.lat, zoom: map.getZoom() }, pinnedRef.current);
    };
    sync();
    map.on('moveend', sync);
    return () => {
      map.off('moveend', sync);
    };
  }, [map, pinned]);

  // The photos in scope sit on their route while it's drawn (FR-16): not while the Track tab has the
  // map or the route is hidden. The Photos tab's unsaved moves and new photos are overlaid.
  const photoMarkers = useMemo<PhotoMarkerItem[]>(() => {
    if (photoScope === null || (editOpen && editTab === 'track')) return [];
    if ('activity' in photoScope && mapHiddenIds.has(photoScope.activity)) return [];
    const items: PhotoMarkerItem[] = photoState.photos
      .filter((p) => p.lon !== null && p.lat !== null && !mapHiddenIds.has(p.activityId))
      .map((p) => ({ id: p.id, lon: p.lon!, lat: p.lat!, thumbSrc: API_BASE_URL + p.thumbUrl, caption: p.caption }));
    if (photoOverlay === null) return items;
    const upserts = new Map(photoOverlay.upserts.map((item) => [item.id, item]));
    const kept = items.filter((item) => !photoOverlay.hidden.includes(item.id)).map((item) => upserts.get(item.id) ?? item);
    const keptIds = new Set(kept.map((item) => item.id));
    return [...kept, ...photoOverlay.upserts.filter((item) => !keptIds.has(item.id))];
  }, [photoScope, editOpen, editTab, mapHiddenIds, photoState.photos, photoOverlay]);
  // A marker's click: one photo opens its popup. A group spread over more than GROUP_SPREAD_M zooms
  // to fit (at most z19, where that spread is wider than a marker); a tighter group opens the popup
  // on its first photo.
  const openPhotoMarker = useCallback(
    (ids: string[]) => {
      if (!map) return;
      const group = photoMarkers.filter((item) => ids.includes(item.id));
      const spread = Math.max(0, ...group.flatMap((a) => group.map((b) => haversineM(a.lat, a.lon, b.lat, b.lon))));
      if (group.length > 1 && spread > GROUP_SPREAD_M) {
        const lons = group.map((p) => p.lon);
        const lats = group.map((p) => p.lat);
        map.fitBounds([Math.min(...lons), Math.min(...lats), Math.max(...lons), Math.max(...lats)], { padding: 80, maxZoom: 19 });
        return;
      }
      setOpenGroup({ ids, index: 0 });
    },
    [map, photoMarkers],
  );
  const openPhotoId = openPhotos.length > 0 && openGroup ? openPhotos[Math.min(openGroup.index, openPhotos.length - 1)]!.id : null;
  usePhotoMarkers(map, photoMarkers, {
    onOpen: openPhotoMarker,
    activeId: photoOverlay?.activeId ?? openPhotoId,
    loneId: photoOverlay?.activeId ?? null,
  });

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
          className={exportFlow.stage === 'idle' ? 'map-root' : 'map-root map-root--exporting'}
          onDragEnter={onMapDragEnter}
          onDragOver={onMapDragOver}
          onDragLeave={onMapDragLeave}
          onDrop={onMapDrop}
        >
          {fileDragOver && (
            <div className="map-drop" data-testid="map-drop">
              <span className="map-drop__label">{t('map.drop_files')}</span>
            </div>
          )}
          <div ref={container} className="map-canvas" data-testid="map-canvas" />
          {map && <ExportControl map={map} active={exportFlow.stage !== 'idle'} onOpen={handleExportOpen} />}
          {map && (exportFlow.stage === 'framing' || exportFlow.stage === 'capturing') && (
            <ExportViewfinder
              map={map}
              containerRef={container}
              preset={exportFlow.preset}
              customSize={exportFlow.customSize}
              maxSidePx={exportMaxSide}
              window={exportFlow.window}
              busy={exportFlow.stage === 'capturing'}
              error={exportError}
              onWindowChange={(window) => setExportFlow((flow) => (flow.stage === 'framing' ? { ...flow, window } : flow))}
              onChoiceChange={handleExportChoiceChange}
              onCapture={() => void handleExportCapture()}
              onCancel={handleExportCancel}
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
                onOverlay: setPhotoOverlay,
              }}
              onTabChange={setEditTab}
              onClose={closeEditWindow}
            />
          )}
          {map && openGroup && openPhotos.length > 0 && (
            <PhotoPopup
              map={map}
              photos={openPhotos}
              index={Math.min(openGroup.index, openPhotos.length - 1)}
              onIndex={(index) => setOpenGroup({ ...openGroup, index })}
              onClose={() => setOpenGroup(null)}
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
          {map && openSpot && <SpotPopup map={map} spot={openSpot} capturedAt={spotCaptures.find((c) => c.spot_id === openSpot.id)?.captured_at ?? null} onClose={() => setOpenSpot(null)} />}
        </div>
      </div>

    </div>
  );
}
