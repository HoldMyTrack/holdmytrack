import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from 'react';
import 'maplibre-gl/dist/maplibre-gl.css';
import { activityQuery as queryFor } from './activityQuery';
import { flyToBBox, unionBBox } from './bbox';
import { defaultRange } from './defaultRange';
import type { MapMode } from './mapMode';
import { initialSession, mapSessionReducer } from './session';
import { toolbarTarget } from './selection';
import { initialPanelTab, storyParam } from './storyUrl';
import type { SpotCategory } from './spots';
import { useCoverageRefresh } from './useCoverageRefresh';
import { useDataRefresh } from './useDataRefresh';
import { useEditWindow } from './useEditWindow';
import { useExportFlow } from './useExportFlow';
import { useFileDrop } from './useFileDrop';
import { useHashSync, useInitialView } from './useInitialView';
import { useListView } from './useListView';
import { useListFlight } from './useListFlight';
import { useMapInstance } from './useMapInstance';
import { useMapLayers } from './useMapLayers';
import { useOverlays } from './useOverlays';
import { usePendingPoll } from './usePendingPoll';
import { usePhotoLayer } from './usePhotoLayer';
import { useSelectionActions } from './useSelectionActions';
import { useSelectionLayers } from './useSelectionLayers';
import { useSpots } from './useSpots';
import { useStoryNav } from './useStoryNav';
import { useTrackMetrics } from './useTrackMetrics';
import { useViewOnMap } from './useViewOnMap';
import type { Activity } from '../api';
import { useAuth } from '../auth/AuthContext';
import { ActivitiesPanel, type PanelTab } from '../ui/ActivitiesPanel';
import { EditActivityWindow, type EditWindowResult } from '../ui/EditActivityWindow';
import { MapControls } from '../ui/MapControls';
import { MapExport } from '../ui/MapExport';
import { PhotoPopup } from '../ui/PhotoPopup';
import { SpotPopup } from '../ui/SpotPopup';
import { todayLocal, type DateRange } from '../ui/dateMath';
import { useUnitSystem } from '../ui/units';
import { useActivityDays } from '../ui/useActivityDays';
import { useActivityList } from '../ui/useActivityList';
import { useStories } from '../ui/useStories';
import { useStory } from '../ui/useStory';
import { t } from '../i18n';

export interface MapViewProps {
  /** Mount with the Activities panel on its Privacy tab: `/?private-locations` (App.tsx). */
  initialPrivateLocationsOpen?: boolean;
  /** Mount on one activity, its day selected and it focused: `/?activity=&day=`, the /sync page's "View on map". */
  initialActivity?: { id: string; day: string } | null;
}

/**
 * The map page: the map and its layers, the Activities panel beside it, and the Edit window, the
 * export and the popups over it. It composes the map's state (session.ts) with each feature's
 * hook; the hooks are called in the order their effects must run.
 */
export function MapView({ initialPrivateLocationsOpen = false, initialActivity = null }: MapViewProps) {
  // A demo account is read-only (docs/SPEC.md FR-2.1); `'email' in user` tells a DemoUser from an AuthUser.
  const { user } = useAuth();
  const isDemo = !('email' in user);

  const { initial, flavor, pinned } = useInitialView();

  const container = useRef<HTMLDivElement>(null);
  // The range or Story shown, the selection over it and the mode (session.ts).
  const [session, dispatch] = useReducer(mapSessionReducer, null, () => initialSession(storyParam(window.location.search)));
  const { range: selectedRange, storyId, mode: mapMode } = session;
  const { checked: checkedActivityIds, focused: focusedActivityId, hidden: hiddenActivityIds, excludedTypes, distanceFilter } = session.selection;
  const { overlays, change: changeOverlays, paths, satelliteAvailable, satellite } = useOverlays();

  const today = useMemo(() => todayLocal(), []);

  // The row or track under the pointer, from either surface, so one effect owns the map's hover
  // feature-state. A preview only: it never moves the camera.
  const [hoveredActivityId, setHoveredActivityId] = useState<string | null>(null);

  // Bumped when a reprocess finishes, so the bands refetch against the new points.
  const [trackMetricsVersion, setTrackMetricsVersion] = useState(0);
  const bumpMetrics = useCallback(() => setTrackMetricsVersion((v) => v + 1), []);

  // The panel's tab lives here so `/?private-locations` (FR-8.1) and `/?story=` (FR-14.6) can open
  // on it, and so it survives the panel unmounting in Fog and Heatmap.
  const [panelTab, setPanelTab] = useState<PanelTab>(() =>
    initialPanelTab(window.location.search, initialActivity !== null, initialPrivateLocationsOpen),
  );

  const storyState = useStory(storyId);
  const storiesList = useStories(panelTab === 'stories');

  const toolbarTargetIds = useMemo(() => toolbarTarget(checkedActivityIds, focusedActivityId), [checkedActivityIds, focusedActivityId]);
  const changeSelectedRange = useCallback((range: DateRange) => dispatch({ type: 'rangePicked', range }), []);

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

  const edit = useEditWindow(activities, activitiesLoading);
  const editOpen = edit.open;
  const editingTrack = edit.editingId !== null;
  const spotsShown = useMemo<readonly SpotCategory[]>(
    () => (editingTrack || !overlays.enabled ? [] : overlays.spots),
    [editingTrack, overlays.enabled, overlays.spots],
  );

  // The default range (defaultRange.ts) is re-derived on every real refetch of days
  // (`historyGeneration`) until the user picks a range (session.ts ignores it after). Trap: keying
  // on `visibleDays` would also re-pick on Earlier/Later paging, and settling once would freeze a
  // zero-history account (every demo) on `{today, today}` past its first upload.
  // Held while the Edit window is open; it catches up when the window closes.
  useEffect(() => {
    if (session.userPicked || !daysReady) return;
    dispatch({ type: 'defaultRange', range: defaultRange(visibleDays, earliest, today), editOpen });
    // visibleDays isn't a dependency; see above.
  }, [session.userPicked, daysReady, earliest, historyGeneration, today, editOpen]);

  const { pendingIds, distanceBounds: activityDistanceBounds, facets, listed: filteredActivities, mapHiddenIds } = useListView(activities, session.selection, dispatch);

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
    editWindowIds: edit.ids,
    focusedId: focusedActivityId,
    storyId,
    version: `${trackMetricsVersion}|${storyMembers}`,
    editOpen,
    trackTabShown: editOpen && edit.tab === 'track',
    mapHiddenIds,
  });

  // Flies to the combined bounds of the given activities; one activity is its own bbox.
  const fitToSelection = useCallback(
    (selection: Activity[]) => {
      if (!map) return;
      const flyBounds = unionBBox(selection.flatMap((a) => (a.bbox ? [a.bbox] : [])));
      if (flyBounds) flyToBBox(map, flyBounds);
    },
    [map],
  );

  // A mode change never moves the camera.
  const changeMapMode = useCallback((mode: MapMode) => dispatch({ type: 'modeChanged', mode }), []);
  const select = useSelectionActions(dispatch, activities, filteredActivities, checkedActivityIds, toolbarTargetIds, mapHiddenIds, fitToSelection);
  const focusActivity = select.focus;
  const clearFocus = select.clearFocus;
  useViewOnMap(map, initialActivity, { mode: mapMode, range: selectedRange, activities }, dispatch, focusActivity, revealDay);

  useListFlight(map, {
    activities,
    loadedKey: activitiesLoadedKey,
    storyId,
    flyRequest: session.flyRequest,
    onFlown: () => dispatch({ type: 'flyConsumed' }),
    mapHiddenIds,
    urlView: initial.hash.view != null,
    arrivingOnActivity: initialActivity !== null,
    daysReady,
    earliest,
    country: user.country,
    fitTo: fitToSelection,
  });

  const spots = useSpots(map, spotsShown);
  useSelectionLayers(map, {
    focusedId: focusedActivityId,
    hoveredId: hoveredActivityId,
    mapHiddenIds,
    editOpen,
    onFocus: focusActivity,
    onHover: setHoveredActivityId,
    onClickAway: clearFocus,
  });
  const bands = useTrackMetrics(map, focusedActivityId, trackMetricsVersion, mapHiddenIds, pendingIds);

  // Fog and Heatmap are re-rendered by queued jobs, so after an upload or delete this waits for the
  // account's jobs to drain and then refetches both (useCoverageRefresh.ts). `coverageRendering`
  // drives the notice that they may be incomplete.
  const { watch: watchCoverage, rendering: coverageRendering } = useCoverageRefresh(map);
  const refresh = useDataRefresh(map, {
    activityQuery,
    reloadActivities,
    reloadDays,
    reloadStory: storyState.reload,
    watchCoverage,
    bumpMetrics,
  });

  const fileDrop = useFileDrop(!isDemo);

  // Deleted activities: the upload refresh (the slider's days change too), once per batch, and
  // their ids dropped from the selection. The focus matters most: left on a deleted id, the bands
  // would keep fetching its metrics.
  const handleActivitiesDeleted = useCallback(
    (ids: string[]) => {
      refresh.afterImport();
      dispatch({ type: 'activitiesRemoved', ids });
    },
    [refresh.afterImport],
  );

  const stories = useStoryNav({
    storyId,
    dispatch,
    panelTab,
    setPanelTab,
    stories: storiesList,
    story: storyState,
    reloadActivities,
    watchCoverage,
    refreshTracks: refresh.refreshTracks,
    afterImport: refresh.afterImport,
    onActivityRemoved: (id) => setHoveredActivityId((hovered) => (hovered === id ? null : hovered)),
  });

  const awaitEdit = usePendingPoll(pendingIds, { reload: reloadActivities, onStarted: refresh.pendingStarted, onFinished: refresh.pendingFinished });

  const openEditWindow = useCallback(
    (group: Activity[]) => {
      setHoveredActivityId(null);
      edit.openFor(group);
    },
    [edit.openFor],
  );
  // The Track tab's session (§4.7.7): flies to the activity, then TrackEditor owns the map until the window closes.
  const startEditTrack = useCallback(
    (activity: Activity) => {
      setHoveredActivityId(null);
      edit.startTrack(activity.id);
      fitToSelection([activity]);
    },
    [edit.startTrack, fitToSelection],
  );
  const closeEditWindow = useCallback(
    ({ saved, trackApplied, photosSaved }: EditWindowResult) => {
      const editedTrack = edit.close();
      if (trackApplied && editedTrack !== null) awaitEdit(editedTrack);
      // A track edit leaves the row Pending; the poll runs until the reprocess lands.
      if (saved) reloadActivities();
      if (photosSaved) photoLayer.photos.reload();
    },
    [edit.close, awaitEdit, reloadActivities, photoLayer.photos.reload],
  );

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
              onToggleType={select.toggleType}
              distanceBounds={activityDistanceBounds}
              distanceFilter={distanceFilter}
              onChangeDistance={select.setDistanceFilter}
              onResetFilters={select.resetFilters}
              checked={checkedActivityIds}
              targetIds={toolbarTargetIds}
              focusedId={focusedActivityId}
              hoveredId={hoveredActivityId}
              onToggle={select.toggleChecked}
              onFocus={focusActivity}
              onClearFocus={clearFocus}
              onHoverActivity={setHoveredActivityId}
              onClear={select.clearChecked}
              onSelectAll={select.checkAll}
              onInvertSelection={select.invertChecked}
              onShowSelected={select.showTarget}
              hiddenIds={hiddenActivityIds}
              onToggleGroupVisibility={select.toggleGroupVisibility}
              onActivitiesDeleted={handleActivitiesDeleted}
              onEdit={openEditWindow}
              onStoryCreated={stories.openCreatedStory}
              onStoryAdded={stories.handleStoryAdded}
              stories={stories.storiesPanel}
              tab={panelTab}
              onTabChange={stories.changePanelTab}
              map={map}
              onPrivateLocationsChanged={refresh.afterPrivateLocations}
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
          {map && <MapExport map={map} container={container} exporter={exporter} />}
          {map && edit.activities && (
            <EditActivityWindow
              map={map}
              activities={edit.activities}
              knownTypes={facets}
              trackUnavailable={edit.trackBlocker === null ? null : t(edit.trackBlocker)}
              onStartTrack={startEditTrack}
              photosUnavailable={edit.photosBlocker === null ? null : t(edit.photosBlocker)}
              photos={{ photos: photoLayer.photos.photos, error: photoLayer.photos.error, onOverlay: photoLayer.setOverlay }}
              onTabChange={edit.setTab}
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
            <MapControls
              map={map}
              mode={mapMode}
              onModeChange={changeMapMode}
              overlays={overlays}
              onOverlaysChange={changeOverlays}
              flavor={flavor}
              satelliteAvailable={satelliteAvailable}
              coverageRendering={coverageRendering}
              spotsShown={spotsShown}
            />
          )}
          {map && spots.openSpot && <SpotPopup map={map} spot={spots.openSpot} capturedAt={spots.capturedAt(spots.openSpot.id)} onClose={spots.closeSpot} />}
        </div>
      </div>
    </div>
  );
}
