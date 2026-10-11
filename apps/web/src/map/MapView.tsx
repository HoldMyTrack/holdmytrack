import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { flyToBBox, flyToView, unionBBox } from './bbox';
import { basemapOrigin, satelliteSource, WORLD_VIEW } from './config';
import { countryView } from './countryView';
import { exportFramedImage } from './exportMap';
import type { ExportPreset } from './exportPresets';
import { ensureBikePathLayers, setBikePathsVisible } from './bikePaths';
import { ensureFogLayer } from './fog';
import { ensureHeatmapLayer } from './heatmap';
import { setMapMode, type MapMode } from './mapMode';
import { loadOverlays, saveOverlays, type Overlays } from './overlays';
import { raisePathLayers } from './paths';
import { usePhotoMarkers, type PhotoMarkerItem, type PhotoMarkerOverlay } from './photos';
import { setSatelliteVisible } from './satellite';
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
import { ExportFrame, type FrameGeometry } from '../ui/ExportFrame';
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

/** How often the list is re-read while an Edit track reprocess is pending — the job is one
 *  activity's worth of ingest, so a few seconds is the right order of magnitude. */
const EDIT_PENDING_POLL_MS = 2000;

/** The Story the URL opens on the Stories tab, `/?story=<id>` (FR-14.6) — kept in the URL,
 *  unlike `?private-locations`, so a refresh or a shared link comes back to it. */
function storyParam(): string | null {
  return new URLSearchParams(window.location.search).get('story') || null;
}

/** `/?tab=stories` opens on the Stories tab with no Story picked — where the email about a copy
 *  of a Story someone sent links to, since its inbox is at the top of that tab (FR-14.7). */
function storiesTabParam(): boolean {
  return new URLSearchParams(window.location.search).get('tab') === 'stories';
}

/** The current URL with `?story=` set to id, or taken off for null — the hash (the camera)
 *  and any other parameter kept. */
function urlWithStory(id: string | null): string {
  const params = new URLSearchParams(window.location.search);
  if (id === null) params.delete('story');
  else params.set('story', id);
  const qs = params.toString();
  return window.location.pathname + (qs ? `?${qs}` : '') + window.location.hash;
}

/** What opening or closing a Story does to the URL: a new history entry, a replaced one, or
 *  nothing (Back/Forward already moved it). */
type StoryNav = 'push' | 'replace' | 'none';

function moveStoryUrl(id: string | null, nav: StoryNav) {
  if (nav === 'push') window.history.pushState(null, '', urlWithStory(id));
  else if (nav === 'replace') window.history.replaceState(null, '', urlWithStory(id));
}

export interface MapViewProps {
  /** Mount with the Activities panel on its Privacy tab — `/?private-locations` (App.tsx). */
  initialPrivateLocationsOpen?: boolean;
  /** Mount on one activity: its day selected, it focused — `/?activity=&day=` (App.tsx), the
   *  /sync page's "View on map". */
  initialActivity?: { id: string; day: string } | null;
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

/** How far apart, in metres, a group's photos must be for zooming in to separate them (§4.27). */
const GROUP_SPREAD_M = 15;

function haversineM(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const rad = Math.PI / 180;
  const h = Math.sin(((lat2 - lat1) * rad) / 2) ** 2 + Math.cos(lat1 * rad) * Math.cos(lat2 * rad) * Math.sin(((lon2 - lon1) * rad) / 2) ** 2;
  return 2 * 6_371_008.8 * Math.asin(Math.min(1, Math.sqrt(h)));
}

export function MapView({ initialPrivateLocationsOpen = false, initialActivity = null }: MapViewProps) {
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
      flavor: pinnedFlavor(hash) ?? flavorForTheme(currentTheme()),
    };
  });

  // The basemap flavor follows the page's light/dark theme, OS preference or account-menu
  // choice alike, and swaps live when that changes. A URL can pin one of the other flavors
  // (pinnedFlavor), but only until the theme next changes: whoever just switched the theme
  // expects the map to follow.
  const theme = useTheme();
  const [pinned, setPinned] = useState(() => pinnedFlavor(initial.hash));
  const initialTheme = useRef(theme);
  useEffect(() => {
    if (theme !== initialTheme.current) setPinned(undefined);
  }, [theme]);
  const flavor: Flavor = pinned ?? flavorForTheme(theme);
  // Read by the hash sync below, whose moveend listener outlives any one render.
  const pinnedRef = useRef(pinned);
  pinnedRef.current = pinned;

  const container = useRef<HTMLDivElement>(null);
  const [exportFlow, setExportFlow] = useState<ExportFlow>({ stage: 'idle' });
  const [exportError, setExportError] = useState<string | null>(null);
  // Normal is what already rendered before fog existed — it needed no new work to count
  // as a "mode" (IMPLEMENTATION.md §4.2.2).
  const [mapMode, setMapModeState] = useState<MapMode>('normal');
  // The Satellite button (BasemapToggle.tsx) and the Layers menu (OverlaysMenu.tsx): Bike
  // paths, Shared paths and each Spots category, over any mode, drawn while the Layers checkbox is
  // on; remembered per browser (overlays.ts).
  const [overlays, setOverlays] = useState<Overlays>(loadOverlays);
  const changeOverlays = useCallback((next: Overlays) => {
    saveOverlays(next);
    setOverlays(next);
  }, []);
  const paths = useMemo(
    () => ({
      bikePaths: overlays.enabled && overlays.bikePaths,
      sharedPaths: overlays.enabled && overlays.sharedPaths,
    }),
    [overlays.enabled, overlays.bikePaths, overlays.sharedPaths],
  );
  // Satellite imagery (FR-4.14): only when the deployment configures some, whatever was saved.
  const satelliteAvailable = satelliteSource() !== null;
  const satellite = satelliteAvailable && overlays.satellite;
  // The spot whose popup is open (SpotPopup, FR-15.3) — null when none is.
  const [openSpot, setOpenSpot] = useState<Spot | null>(null);
  // The places this account captured with the Android app (FR-15.6): drawn with the captured
  // badge, and dated in their popup.
  const [spotCaptures, setSpotCaptures] = useState<readonly SpotCapture[]>([]);

  const today = useMemo(() => todayLocal(), []);

  // Left-panel ActivitiesPanel layout.
  // Selection lives here, not in ActivitiesPanel, since the map instance and the fly-to
  // callbacks below both need it. The panel itself is not optional/toggleable.
  //
  // Two fully independent mechanisms, reported live as wrongly coupled when they shared one
  // Set: clicking a row's own text/body (or its track on the map) *selects* it — the single
  // focus (focusActivity, replace): bolds that one track, flies there, and replaces whatever
  // was focused before; clicking empty map or empty list space clears it. The checkbox only
  // builds a multi-row group (toggleActivityChecked, add/remove, additive) for the header
  // toolbar to act on: it doesn't bold anything on the map, doesn't fly, and never touches the
  // focus — nor does the focus ever touch a checkbox. Both start empty/null: there is no
  // sensible default over a real, unknown history.
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

  // Pace-colored segments (trackBands.ts) for whichever activity is currently row-click
  // focused — null whenever focusedActivityId is (see the fetch effect below).
  const [trackMetrics, setTrackMetrics] = useState<ActivityTrackMetrics | null>(null);
  // Bumped when an Edit track reprocess finishes, so the focused activity's bands are
  // refetched against its new points — the fetch below is otherwise keyed on the id alone.
  const [trackMetricsVersion, setTrackMetricsVersion] = useState(0);

  // The Edit window (EditActivityWindow.tsx) — the checked group it was opened over, by id. While
  // it's open the panel is inert and the map's own controls step aside.
  const [editWindowIds, setEditWindowIds] = useState<string[] | null>(null);
  const editOpen = editWindowIds !== null;
  // Its Track tab's session (§4.7.7) — one activity, from the first time that tab opens until
  // the window closes. While it's set, every other track is hidden and TrackEditor owns the map.
  const [editingActivityId, setEditingActivityId] = useState<string | null>(null);
  const editingTrack = editingActivityId !== null;
  // Spots step aside during a track session, like every other track: TrackEditor owns the map.
  const spotsShown = useMemo<readonly SpotCategory[]>(
    () => (editingTrack || !overlays.enabled ? [] : overlays.spots),
    [editingTrack, overlays.enabled, overlays.spots],
  );

  // The Activities panel's tab — here rather than in the panel so `/?private-locations` can open
  // onto Privacy (FR-8.1) and `/?story=` onto Stories (FR-14.6), and so the tab outlives the
  // panel unmounting for Fog/Heatmap.
  const [panelTab, setPanelTab] = useState<PanelTab>(
    initialActivity !== null
      ? 'activities'
      : storyParam() !== null || storiesTabParam()
        ? 'stories'
        : initialPrivateLocationsOpen
          ? 'private'
          : 'activities',
  );

  // The list/summary filter — the Activities tab's date slider (DateRangeSlider.tsx). The
  // slider's own window position is not here on purpose: it lives in useActivityDays and the
  // two are independent, so paging back through history never touches what's selected.
  // selectedRange starts null: its real default (the 5 most recent activity-days — see the
  // effect below) needs the first page of days, which hasn't loaded yet on first render.
  const [selectedRange, setSelectedRangeState] = useState<DateRange | null>(null);

  // The open Story (FR-14.6): while one is open on the Stories tab, the list and the
  // drawn tracks are the whole Story's activities, with no date range — `selectedRange` is left
  // alone, so leaving the tab finds it as it was. Opened from the tab, the URL, Add to story, or
  // by Back/Forward; closed by leaving the tab.
  const [storyId, setStoryId] = useState<string | null>(storyParam);
  const storyState = useStory(storyId);

  // Photos (FR-16), in Normal mode — neither Fog nor Heatmap focuses an activity or opens a
  // Story: the one activity the Edit window is open on, whose Photos tab manages them; else the
  // focused activity's, the route a person is looking at; else the open Story's, the whole
  // trip's pictures along its days.
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
  // The photos whose popup is open — one, or a group's, stepped through — from a marker's click;
  // closed whenever the photos in view change hands, and showing only those still there.
  const [openGroup, setOpenGroup] = useState<{ ids: string[]; index: number } | null>(null);
  // Only saved photos open; a photo the Photos tab hasn't saved yet has nothing to show.
  const openPhotos = useMemo(
    () => (openGroup ? openGroup.ids.flatMap((id) => photoState.photos.filter((p) => p.id === id)) : []),
    [openGroup, photoState.photos],
  );
  // The Photos tab's unsaved changes as they alter the markers (PhotosTab.tsx).
  const [photoOverlay, setPhotoOverlay] = useState<PhotoMarkerOverlay | null>(null);
  // Which tab the Edit window shows: its Track tab hides the photos.
  const [editTab, setEditTab] = useState<EditTab>('activity');
  const photoScopeKey = photoScope === null ? null : JSON.stringify(photoScope);
  useEffect(() => setOpenGroup(null), [photoScopeKey]);
  const storiesList = useStories(panelTab === 'stories');

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
  // selectAll — both need fitToSelection/activities/mapHiddenIds, which aren't in scope
  // yet at this point in the component.
  //
  // What the header toolbar acts on: the checked group whenever anything is checked, else the
  // focused (selected) row alone, else nothing. Checked wins rather than the two being unioned:
  // the group is built deliberately, one tick at a time, while a row gets focused just by
  // looking at it — so a Delete never reaches further than what was ticked, and a focus never
  // silently replaces a group either. The toolbar names its target, so which one applies is
  // never a guess.
  const toolbarTargetIds = useMemo(() => {
    if (checkedActivityIds.size > 0) return checkedActivityIds;
    return new Set(focusedActivityId !== null ? [focusedActivityId] : []);
  }, [checkedActivityIds, focusedActivityId]);
  //
  // The header toolbar's "Group visible" — there's no per-row eye icon any more (hiding a
  // single activity goes through the toolbar, the same as every other single-item action), so
  // this is the only visibility toggle left, applied to the toolbar's whole target. The rule:
  // if any of it is currently hidden, show all of it (removes every target id from
  // hiddenActivityIds); otherwise hide all of it (adds every target id). Mirrors a typical
  // bulk-checkbox toggle — one click reveals everything in the group, click again to hide it —
  // rather than per-row toggling each one individually, which would leave the group in a mixed
  // state no single click could cleanly undo.
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

  // A Story, or the date range — never both: a Story is the whole of it.
  const activityQuery = useMemo(
    () =>
      storyId !== null
        ? { story: storyId }
        : selectedRange
          ? { from: selectedRange.from, to: selectedRange.to }
          : {},
    [selectedRange, storyId],
  );

  // Fetched once here rather than in each consumer: the header badge, the panel's list and the
  // map's drawn tracks all have to agree on the same rows after an upload.
  const {
    activities,
    loading: activitiesLoading,
    error: activitiesError,
    reload: reloadActivities,
    loadedKey: activitiesLoadedKey,
  } = useActivityList(activityQuery);
  // The slider's window of days and its pan position, independent of the selection above — it
  // pages by days-with-activity rather than by calendar window, see useActivityDays.
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

  // Defaults to the 5 most recent activity-days, not all-time: "what did I do lately" is what
  // opening the map asks, and a whole history's tracks at once is slow to draw and fit. It also
  // leaves both knobs inside the slider's window, with room to move either way. `visibleDays`
  // is ascending, so its last 5 entries are the most recent 5.
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
  // only by `reloadDays`, i.e. a real refetch — never by plain panning, which changes
  // `visibleDays` just as much but must never re-pick the selection out from under a user
  // who's simply browsing) is what lets this safely reconsider on new data without also
  // firing on every Earlier/Later click.
  //
  // Held still while the Edit window is open: a big import lands newer activity-days on every
  // poll tick, sliding the default forward past the activity being edited — which drops it
  // from the list, and the window can't outlive its activities (below), so it kept closing
  // itself until the whole import had finished. `editOpen` is a dependency so the default
  // catches up on whatever landed meanwhile the moment the window closes.
  useEffect(() => {
    if (userChangedRangeRef.current || !daysReady || editOpen) return;
    const recentDays = visibleDays.slice(-5);
    setSelectedRangeState({ from: recentDays[0]?.date ?? earliest ?? today, to: today });
    // visibleDays deliberately isn't a dependency — see the comment above.
  }, [daysReady, earliest, historyGeneration, today, editOpen]);

  // Rows with a reprocess still pending — an Edit track (§4.7.7) or a Private location change
  // — read by the polling and completion effects further down, by the bands effect, and by
  // mapHiddenIds: a Pending track isn't drawn (FR-5.15).
  const pendingIds = useMemo(() => activities.filter((a) => a.pending).map((a) => a.id), [activities]);

  const activityDistanceBounds = useMemo(() => distanceBounds(activities), [activities]);
  // The slider, and its Reset, only show while the loaded activities span a range of distances.
  // A band left set when they stop doing so — deleted down to one, say — would go on filtering
  // with nothing on screen to clear it.
  const distanceSliderShown = activityDistanceBounds !== null && activityDistanceBounds.min < activityDistanceBounds.max;
  useEffect(() => {
    if (!distanceSliderShown) setDistanceFilter(null);
  }, [distanceSliderShown]);
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
    initialSatellite: satellite,
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
        { flavor, mode: mapMode, activityQuery, hiddenIds: [...mapHiddenIds], paths, satellite },
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
  }, [exportFlow, map, flavor, mapMode, activityQuery, mapHiddenIds, paths, satellite]);

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

  // Clicking a row's own text/body — a single "look at just this one" focus, independent of
  // the checkbox group above (see the state comment near checkedActivityIds/focusedActivityId
  // for why these no longer share a Set). Flies immediately: along with the toolbar's Focus
  // button, the only thing that ever flies to an activity. Excludes a
  // currently-hidden target: fitToSelection over an empty array (the target filtered out) is a
  // no-op, not a fly to empty water.
  const focusActivity = useCallback(
    (id: string) => {
      setFocusedActivityId(id);
      const target = activities.filter((a) => a.id === id && !mapHiddenIds.has(a.id));
      fitToSelection(target);
    },
    [activities, mapHiddenIds, fitToSelection],
  );

  // The /sync page's "View on map" (`/?activity=&day=`, below), reusing focusActivity above
  // rather than inventing a second fly-to mechanism. The one thing a row click doesn't already
  // handle: the target activity may not be in the currently selected date range (an old upload,
  // a Health Connect backfill), in which case focusActivity would silently find nothing
  // in `activities` and no-op. When that happens, this narrows the range to just that activity's
  // own day — in the account's timezone, as the link carries it (changeSelectedRange, the same
  // mechanism the slider commits through) — and defers the actual focus to the effect below,
  // which fires once that range's own refetch has actually landed and the id is really there.
  // Also restores Normal mode first: Fog/Heatmap have no per-track focus concept, and their own
  // mode-switch effect already clears focus whenever entering either.
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
      // The focus below flies to the activity itself; the range fly would override it.
      flyToNextRangeRef.current = false;
    },
    [mapMode, changeMapMode, selectedRange, changeSelectedRange, focusActivity, revealDay],
  );
  // `/?activity=&day=` (App.tsx), once, on arrival: before any range of the user's own, so the
  // default range never replaces it.
  const arrivedOnActivityRef = useRef(false);
  useEffect(() => {
    if (initialActivity === null || arrivedOnActivityRef.current) return;
    arrivedOnActivityRef.current = true;
    viewActivityOnDay(initialActivity.id, initialActivity.day);
  }, [initialActivity, viewActivityOnDay]);
  // Waits for the map too: arriving by `/?activity=` the list can land before the map has
  // loaded, and a focus then would have nothing to fly.
  useEffect(() => {
    const pending = pendingFocusIdRef.current;
    if (pending === null || !map) return;
    if (activities.some((a) => a.id === pending)) {
      pendingFocusIdRef.current = null;
      focusActivity(pending);
    }
  }, [map, activities, focusActivity]);

  // A click on empty space in the Activities panel's list — the panel's counterpart to clicking
  // away from every track on the map (tracks.ts's onClickAway). The checked group stays.
  const clearFocus = useCallback(() => {
    setFocusedActivityId(null);
  }, []);

  // The toolbar's master checkbox, clicked while fully checked: empties the checked group (a
  // focused row, if any, is untouched). Like every checkbox action, it leaves the camera alone.
  const clearSelection = useCallback(() => {
    setCheckedActivityIds(new Set());
  }, []);

  // The master checkbox, clicked while unchecked or partial — checks every row the panel
  // currently lists (already narrowed by TYPE/DISTANCE, per filteredActivities below).
  // Deliberately scoped to what's actually shown, not every activity in the date range
  // regardless of filters — checking rows the user can't see would be a surprise. No fly: a
  // checkbox only builds the toolbar's group, and Focus on map (showSelected below) is there to
  // look at it.
  const selectAll = useCallback(() => {
    setCheckedActivityIds(new Set(filteredActivities.map((a) => a.id)));
  }, [filteredActivities]);

  // The toolbar's invert-selection icon — checks every listed row that isn't checked and unchecks
  // every one that is. Same scope as selectAll: only rows the panel currently lists, so a
  // checked row outside the current TYPE/DISTANCE filters is dropped rather than kept checked
  // out of sight. No fly, same as selectAll.
  const invertSelection = useCallback(() => {
    const inverted = filteredActivities.filter((a) => !checkedActivityIds.has(a.id));
    setCheckedActivityIds(new Set(inverted.map((a) => a.id)));
  }, [filteredActivities, checkedActivityIds]);

  // The toolbar's Focus on map — no state change, just a fly to fit the toolbar's target (the
  // checked group, else the focused row), the one way to look at a checked group. Excludes
  // hidden tracks, so an all-hidden target is a no-op rather than a fly to empty water.
  const showSelected = useCallback(() => {
    const visible = activities.filter((a) => toolbarTargetIds.has(a.id) && !mapHiddenIds.has(a.id));
    fitToSelection(visible);
  }, [activities, toolbarTargetIds, mapHiddenIds, fitToSelection]);

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
  // rather than just generic. After that, only a range the user picked (changeSelectedRange) or
  // a Story opened from the map (enterStory) flies, to fit whatever's now actually visible.
  //
  // Nothing else moves the camera: not a reload of the same range (the Pending poll every 2s,
  // a finished upload or sync, a Private location change), and not a new range the default
  // re-derived because new data arrived (the auto-default effect above, until the user picks
  // one). Keyed on the query the list was fetched for, not on `activities`, which is a new
  // array on every reload — keyed on that, a batch of uploads or Pending rows flew the camera
  // back over and over (reported live).
  // Arriving on one activity (`/?activity=&day=`) is a view of its own: its day selected and it
  // focused, which flies to it — so the first list's fly to the most recent activity stands
  // aside.
  const hasFlownToActivitiesRef = useRef(initialActivity !== null);
  const flownRangeKeyRef = useRef<string | null>(null);
  useEffect(() => {
    if (!map || activitiesLoadedKey === null) return;
    if (activitiesLoadedKey === flownRangeKeyRef.current) return;
    flownRangeKeyRef.current = activitiesLoadedKey;
    if (!hasFlownToActivitiesRef.current) {
      const drawn = activities.filter((a) => a.bbox !== null && !a.pending);
      // A Story with nothing to draw (an empty one, or one that doesn't exist) leaves this for
      // the date range leaving the tab goes back to.
      if (storyId !== null && drawn.length === 0) return;
      // The first list, even an empty one: an account with no history yet gets the fallback
      // view below, and its first upload doesn't fly either.
      hasFlownToActivitiesRef.current = true;
      if (initial.hash.view == null && drawn.length > 0) {
        // Inside a Story, the whole of it — it's a trip, not a history to start at the end of.
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
  // While the Edit window is open the checked group and focus it was opened over must stay put,
  // and during a track session the tracks layer is hidden, so every click would read as a click
  // away — and in Delete point mode, every click is aimed at a point. The handlers go quiet.
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
  // Captures are made on the phone, so they're read again whenever the page comes back into view
  // or the window gets focus back — a browser left open on a desktop stays visible throughout.
  useEffect(() => {
    let controller: AbortController | null = null;
    const load = () => {
      controller?.abort();
      controller = new AbortController();
      getSpotCaptures(controller.signal).then(setSpotCaptures, () => {
        // Not loaded (offline, or aborted): the badges stay as they were.
      });
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

  // The one track bold on the map is the focused (selected) one, however it got that way — a
  // row's own text or a direct click on the map (both focusActivity, via the handler just
  // above). A checked row isn't bolded: the checkbox only builds the toolbar's group, and a map
  // full of bold checked tracks read as that many selections.
  useEffect(() => {
    if (map) setSelectedTracks(map, focusedActivityId !== null ? [focusedActivityId] : []);
  }, [map, focusedActivityId]);

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

  // Pace-colored segments (below) key off the row-click focus directly — not the checked
  // group — since bands are inherently a "look at this one activity" view, the same territory
  // a row click already owns.
  //
  // Fetches per-vertex speed for the focused activity. What's in hand may still be the previous
  // activity's until this lands, so everything drawing it checks `activityId` against the focus.
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

  // Draws (or clears) the pace-colored segments themselves — separate from the fetch effect
  // above so a visibility change re-renders from data already in hand, no refetch. Cleared
  // when the focused activity itself is hidden (its eye icon,
  // or a TYPE/DISTANCE filter narrowing it out) — reported live as hiding a focused activity's
  // plain track (setHiddenTracks, below) while this layer, deliberately drawn *on top* of that
  // same track (trackBands.ts's own doc comment), kept painting it regardless, since this
  // effect never checked mapHiddenIds. Re-showing it draws the band again from data already in
  // hand, no refetch.
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
    if (trackMetrics?.activityId === focusedActivityId && focusedActivityId !== null && !mapHiddenIds.has(focusedActivityId) && !focusedPending) {
      setTrackBands(map, trackMetrics.points);
    } else {
      clearTrackBands(map);
    }
  }, [map, trackMetrics, focusedActivityId, mapHiddenIds, focusedPending]);

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
  // to drain, then refetches both (useCoverageRefresh.ts). It also watches once on load, and
  // `coverageRendering` drives the notice that Fog/Heatmap may still be incomplete.
  const { watch: watchCoverage, rendering: coverageRendering } = useCoverageRefresh(map);

  // A finished import is a new track on the map and a new row in every §4.7 response, so all of
  // them refresh together — the header's Upload menu says when (hmt:imports-changed, below).
  const handleUploaded = useCallback(() => {
    if (map) refreshTrackLayer(map, activityQuery);
    reloadActivities();
    reloadDays();
    storyState.reload();
    // Deletes come through here too (handleActivitiesDeleted), so this covers both.
    watchCoverage();
  }, [map, activityQuery, reloadActivities, reloadDays, storyState.reload, watchCoverage]);

  // The Stories tab's inbox (FR-14.7). A copy that has arrived is a new Story and new activities,
  // whose Fog and Heatmap the copy job has queued — refreshed as an import is.
  const storiesReload = storiesList.reload;
  const handleCopyArrived = useCallback(() => {
    storiesReload();
    handleUploaded();
  }, [storiesReload, handleUploaded]);
  const storyInbox = useStoryInbox(panelTab === 'stories', handleCopyArrived);

  // The header's Upload menu (static/upload.js, served with the page's header) says when an
  // import it's following has finished, or a file's upload has landed — the same refresh.
  useEffect(() => {
    window.addEventListener('hmt:imports-changed', handleUploaded);
    return () => window.removeEventListener('hmt:imports-changed', handleUploaded);
  }, [handleUploaded]);

  // Files dropped on the map go to the header's Upload menu, which uploads them and shows their
  // progress. A demo account has no Upload menu, and the map takes no drop.
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

  // §4.7.5/§4.7.6: one or more deleted activities need the exact same refresh a finished
  // upload does (unlike editing type/description, deleting changes the slider's days too) — plus dropping every deleted id from any local selection
  // state that could otherwise still reference it. focusedActivityId is the one that actually
  // matters for correctness: left pointing at a now-deleted id, the pace-colored segments
  // effect would keep trying to fetch track-metrics for an activity that no
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

  // The toolbar's Edit button over its target (the checked group, else the focused row): opens
  // the Edit window.
  const openEditWindow = useCallback((group: Activity[]) => {
    setHoveredActivityId(null);
    setEditWindowIds(group.map((a) => a.id));
  }, []);

  // Opening and closing a Story. Both start the list's state afresh, as a new range does
  // (changeSelectedRange): the rows are a different set. Neither touches the date range.
  const resetListState = useCallback(() => {
    setCheckedActivityIds(new Set());
    setFocusedActivityId(null);
    setHiddenActivityIds(new Set());
    setExcludedTypes(new Set());
    setDistanceFilter(null);
  }, []);
  const enterStory = useCallback(
    (id: string, nav: StoryNav = 'push') => {
      // Fits the camera to the Story once its list lands (the fly effect above).
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
      // The list goes back to the date range as it was; the camera stays.
      flyToNextRangeRef.current = false;
      moveStoryUrl(null, nav);
    },
    [resetListState],
  );
  // The panel's tabs: leaving Stories closes its Story; opening it opens the newest one (the
  // effect below, once the list is in).
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

  // A Story just made with Add to story's "New story…" opens straight away, on the Stories tab —
  // whose list, fetched as the tab opens, has it.
  const openCreatedStory = useCallback((story: Story) => enterStory(story.id), [enterStory]);
  // Activities just added to an existing Story with Add to story: the list stays where it is, and
  // its reload brings the rows' Story badges up to date.
  // The server has moved the account's tile version, which only `watchCoverage` brings to this
  // page — without it, opening this Story later could draw its tracks from tiles cached before.
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
  // A deleted Story's activities stay, but their Story badges change. Deleting the open one
  // opens the next newest, in its place in the history; deleting the last leaves none open.
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

  // An activity taken out of the open Story with its row's × (FR-14.6): the Story's new numbers,
  // its list without the row, and its tracks redrawn at the tile version the removal moved to.
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
  // §4.7.7's track session, from the Edit window's Track tab. Flies there the same way a row
  // click does, then hands the map to TrackEditor until the window closes.
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
  const closeEditWindow = useCallback(
    ({ saved, trackApplied, photosSaved }: EditWindowResult) => {
      if (trackApplied && editingActivityId !== null) awaitingEditIdsRef.current.add(editingActivityId);
      setEditWindowIds(null);
      setEditingActivityId(null);
      // A track edit leaves the row Pending — the effects below poll until the reprocess lands.
      if (saved) reloadActivities();
      // The Photos tab's draft was written; the markers are the saved photos again.
      if (photosSaved) photoState.reload();
    },
    [editingActivityId, reloadActivities, photoState.reload],
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
  // The window can't outlive its activities — deleted from another tab, say, and gone from the
  // next reload. Without this the map would stay locked with no editor left to close it.
  useEffect(() => {
    if (editOpen && editWindowActivities === null && !activitiesLoading) {
      setEditWindowIds(null);
      setEditingActivityId(null);
    }
  }, [editOpen, editWindowActivities, activitiesLoading]);
  // The Track tab edits one activity's points, so it needs exactly one — with a finished track.
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

  // Photos belong to one activity, and need a track to sit on (FR-16.6).
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

  // While any row is pending, re-read the list every few seconds. Keyed on `activities`
  // itself, so each landed reload schedules the next and polling stops by itself once nothing
  // is pending any more.
  useEffect(() => {
    if (pendingIds.length === 0) return;
    const timer = window.setTimeout(reloadActivities, EDIT_PENDING_POLL_MS);
    return () => window.clearTimeout(timer);
  }, [pendingIds, reloadActivities]);

  // A row that stops being pending has new points, distance and duration: the drawn track,
  // the slider's days and (if it's focused) its bands all need the new version.
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
    reloadDays();
    storyState.reload();
    setTrackMetricsVersion((v) => v + 1);
  }, [pendingIds, map, activityQuery, reloadDays, storyState.reload, watchCoverage]);

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
      // Satellite imagery reads dark, so it takes the dark flavors' veil (style.ts's isDarkBase).
      ensureFogLayer(instance, beforeId, isDarkBase(flavor, satellite));
      ensureHeatmapLayer(instance, beforeId, isDarkBase(flavor, satellite));
      // Trails and tracks, then bike and shared paths, over the veil and the heat and under the
      // activity tracks (paths.ts, bikePaths.ts).
      raisePathLayers(instance, beforeId);
      ensureBikePathLayers(instance, beforeId, flavor);
      ensureTrackLayer(instance, beforeId, activityQuery);
      // A style swap brings the tracks source back with no feature-state, the focused track's
      // `selected` included.
      setSelectedTracks(instance, focusedActivityId !== null ? [focusedActivityId] : []);
      // After tracks, so it paints on top and fully overlays the one track it applies to —
      // see trackBands.ts's own doc comment for why this is a second layer rather than a
      // change to the shared one.
      ensureBandLayer(instance, beforeId);
      // Spots last, above everything including the basemap's labels (spots.ts).
      ensureSpotsLayer(instance, spotsShown);
      setMapMode(instance, mapMode, editingTrack);
      // The bike-path layers just added start hidden, and a style swap brings the imagery back
      // at whatever the swap's buildStyle was given.
      setBikePathsVisible(instance, paths);
      setSatelliteVisible(instance, satellite);
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

  // A theme change swaps the basemap style in place — camera kept, overlays re-added by the
  // styledata handler above. Declared after it on purpose: effects run in order, so a
  // reattach against the outgoing style happens before setStyle starts loading the new one,
  // never against a style still loading. diff: false, so the swap is a clean reload that
  // drops every custom source too, rather than a diff that might keep the fog raster at the
  // other theme's URL.
  const appliedFlavor = useRef(initial.flavor);
  useEffect(() => {
    if (!map || appliedFlavor.current === flavor) return;
    appliedFlavor.current = flavor;
    map.setStyle(buildStyle({ flavor, origin: basemapOrigin(), satellite: satelliteSource(), satelliteOn: satellite, lang }), {
      diff: false,
    });
    // Only a flavor change swaps the style; the Satellite button flips layers in place (below).
  }, [map, flavor]);

  // Mode changes outside of a styledata event (the toggle itself, not a theme swap) still
  // need to flip the layer's visibility — reattachOverlays only re-runs on styledata.
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
    setSatelliteVisible(map, satellite);
  }, [map, satellite]);

  // Keep the hash current. `moveend` rather than `move`: one rewrite per gesture,
  // not one per animation frame.
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

  // Every photo of the activity in view is on its route whenever the route is drawn selected
  // (FR-16.7): not while the Edit window's Track tab has the map, nor while the route itself is
  // hidden. The one the Photos tab is moving sits where its slider has it; one it is placing
  // before upload is drawn from its local thumbnail.
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
  // A marker's click (FR-16.7): one photo opens its popup. A group the map can separate — its
  // photos more than GROUP_SPREAD_M apart — zooms in to fit them, at most to z19, where that
  // spread is wider than a marker; one it can't (several taken at one spot) opens the popup on
  // its first photo, to step through the rest.
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
          // Inert while the Edit window is open: changing the range, the selection or a filter
          // underneath it would pull its activities out from under it.
          // display: contents keeps the wrapper out of the flex layout.
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
          className="map-root"
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
              {/* Its own group: layers switched on and off over any mode, not a fourth mode. */}
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

