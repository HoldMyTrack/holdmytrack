import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from 'react';
import 'maplibre-gl/dist/maplibre-gl.css';
import { activityQuery as queryFor } from './activityQuery';
import { flyToBBox, unionBBox } from './bbox';
import { satelliteSource } from './config';
import { defaultRange } from './defaultRange';
import type { MapMode } from './mapMode';
import { loadOverlays, saveOverlays, type Overlays } from './overlays';
import { initialSession, mapSessionReducer } from './session';
import { invertedChecks, mapHiddenIds as hiddenOnMap, toolbarTarget } from './selection';
import { storiesTabParam, storyParam } from './storyUrl';
import type { SpotCategory } from './spots';
import { useCoverageRefresh } from './useCoverageRefresh';
import { useDataRefresh } from './useDataRefresh';
import { useEditWindow } from './useEditWindow';
import { useExportFlow } from './useExportFlow';
import { useFileDrop } from './useFileDrop';
import { useHashSync, useInitialView } from './useInitialView';
import { useListFlight } from './useListFlight';
import { useMapInstance } from './useMapInstance';
import { useMapLayers } from './useMapLayers';
import { usePendingPoll } from './usePendingPoll';
import { usePhotoLayer } from './usePhotoLayer';
import { useSelectionLayers } from './useSelectionLayers';
import { useSpots } from './useSpots';
import { useStoryNav } from './useStoryNav';
import { useTrackMetrics } from './useTrackMetrics';
import type { Activity } from '../api';
import { useAuth } from '../auth/AuthContext';
import { distanceBounds, passesFilters, typeFacets, type DistanceRange } from '../ui/activityFacets';
import { ActivitiesPanel, type PanelTab } from '../ui/ActivitiesPanel';
import { EditActivityWindow, type EditWindowResult } from '../ui/EditActivityWindow';
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

  // The row or track under the pointer, from either surface, so one effect owns the map's hover
  // feature-state. A preview only: it never moves the camera.
  const [hoveredActivityId, setHoveredActivityId] = useState<string | null>(null);

  // Bumped when a reprocess finishes, so the bands refetch against the new points.
  const [trackMetricsVersion, setTrackMetricsVersion] = useState(0);
  const bumpMetrics = useCallback(() => setTrackMetricsVersion((v) => v + 1), []);

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

  const storyState = useStory(storyId);
  const storiesList = useStories(panelTab === 'stories');

  const toggleActivityChecked = useCallback((id: string) => dispatch({ type: 'checkToggled', id }), []);
  const toolbarTargetIds = useMemo(() => toolbarTarget(checkedActivityIds, focusedActivityId), [checkedActivityIds, focusedActivityId]);
  const toggleGroupVisibility = useCallback(() => dispatch({ type: 'visibilityToggled' }), []);
  // A toggle: the type stays in the dropdown either way, so it can be turned back on without Reset filters.
  const toggleType = useCallback((activityType: string) => dispatch({ type: 'typeToggled', activityType }), []);
  const resetActivityFilters = useCallback(() => dispatch({ type: 'filtersReset' }), []);
  const setDistanceFilter = useCallback((filter: DistanceRange | null) => dispatch({ type: 'distanceSet', filter }), []);
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
  //
  // Held while the Edit window is open: an import landing newer days would slide the default past
  // the edited activity and close the window. It catches up when the window closes.
  useEffect(() => {
    if (session.userPicked || !daysReady || editOpen) return;
    dispatch({ type: 'defaultRange', range: defaultRange(visibleDays, earliest, today) });
    // visibleDays isn't a dependency; see above.
  }, [session.userPicked, daysReady, earliest, historyGeneration, today, editOpen]);

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

  // Focuses one activity and flies to it, unless it's hidden (an empty fit is a no-op). With the
  // toolbar's Focus on map, the only fly to an activity.
  const focusActivity = useCallback(
    (id: string) => {
      dispatch({ type: 'focused', id });
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
      dispatch({ type: 'dayViewed', day });
    },
    [mapMode, changeMapMode, selectedRange, focusActivity, revealDay],
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
  const clearFocus = useCallback(() => dispatch({ type: 'focused', id: null }), []);
  // The master checkbox while fully checked: empties the group; the focus stays.
  const clearSelection = useCallback(() => dispatch({ type: 'checkedSet', ids: [] }), []);
  // The master checkbox while unchecked or partial: checks every row listed, so never a row the
  // filters hide. No fly; Focus on map does that.
  const selectAll = useCallback(() => dispatch({ type: 'checkedSet', ids: filteredActivities.map((a) => a.id) }), [filteredActivities]);
  // Inverts the check over the listed rows; a checked row the filters hide is dropped. No fly.
  const invertSelection = useCallback(
    () => dispatch({ type: 'checkedSet', ids: invertedChecks(filteredActivities, checkedActivityIds) }),
    [filteredActivities, checkedActivityIds],
  );
  // Focus on map: flies to the toolbar's target, hidden tracks excluded. The one way to look at a checked group.
  const showSelected = useCallback(() => {
    const visible = activities.filter((a) => toolbarTargetIds.has(a.id) && !mapHiddenIds.has(a.id));
    fitToSelection(visible);
  }, [activities, toolbarTargetIds, mapHiddenIds, fitToSelection]);

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

  // A row that leaves Pending has new points: the track, the days, the open Story and the bands
  // refresh. The camera stays (FR-5.15). Fog and Heatmap go through the coverage watch, because the
  // server clears Pending just before its last render; a row that enters Pending starts the same watch.
  const awaitEdit = usePendingPoll(pendingIds, {
    reload: reloadActivities,
    onStarted: () => {
      refresh.refreshTracks();
      watchCoverage();
    },
    onFinished: () => {
      refresh.refreshTracks();
      // Includes the Country/Region tiers: an edit can un-visit a region.
      watchCoverage();
      reloadDays();
      storyState.reload();
      bumpMetrics();
    },
  });

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
  const editWindowActivities = edit.activities;
  const editTrackUnavailable = edit.trackBlocker === null ? null : t(edit.trackBlocker);
  const editPhotosUnavailable = edit.photosBlocker === null ? null : t(edit.photosBlocker);
  const setEditTab = edit.setTab;
  const photoState = photoLayer.photos;
  const { storiesPanel, changePanelTab, openCreatedStory, handleStoryAdded } = stories;
  const handlePrivateLocationsChanged = refresh.afterPrivateLocations;

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
