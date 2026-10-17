package dev.holdmytrack.android

import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import androidx.fragment.app.Fragment
import androidx.lifecycle.lifecycleScope
import android.content.Intent
import android.graphics.RectF
import android.os.Bundle
import android.util.Log
import android.view.View
import dev.holdmytrack.android.export.ExportController
import dev.holdmytrack.android.map.BoxUnion
import dev.holdmytrack.android.map.CameraFramer
import dev.holdmytrack.android.map.ChromeInsets
import dev.holdmytrack.android.map.CoverageController
import dev.holdmytrack.android.map.DataRefresh
import dev.holdmytrack.android.map.DateRange
import dev.holdmytrack.android.map.LayersController
import dev.holdmytrack.android.map.LocateController
import dev.holdmytrack.android.map.MapChromeRules
import dev.holdmytrack.android.map.MapMode
import dev.holdmytrack.android.map.MapNotice
import dev.holdmytrack.android.map.MapOverlays
import dev.holdmytrack.android.map.MapSatellite
import dev.holdmytrack.android.map.MapSession
import dev.holdmytrack.android.map.ModeBar
import dev.holdmytrack.android.map.ModeBarState
import dev.holdmytrack.android.map.NoticePanel
import dev.holdmytrack.android.map.PaceBands
import dev.holdmytrack.android.map.RangeController
import dev.holdmytrack.android.map.SheetChrome
import dev.holdmytrack.android.map.SpotsController
import dev.holdmytrack.android.map.TrackEditOverlay
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.SessionGate
import dev.holdmytrack.android.panel.ActivitiesPanel
import dev.holdmytrack.android.panel.EditActivityWindow
import dev.holdmytrack.android.panel.EditController
import dev.holdmytrack.android.panel.PanelState
import dev.holdmytrack.android.panel.PanelTab
import dev.holdmytrack.android.panel.PrivateLocationEditor
import dev.holdmytrack.android.panel.SyncTab
import dev.holdmytrack.android.photos.PhotosController
import dev.holdmytrack.android.recording.RecordedPoint
import dev.holdmytrack.android.recording.RecordingController
import dev.holdmytrack.android.recording.RecordingService
import dev.holdmytrack.android.sync.SyncCandidatesController
import dev.holdmytrack.android.sync.SyncProgress
import org.maplibre.android.camera.CameraPosition
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style

/**
 * The map: the served basemap, the session the user layers need, and the Normal / Fog of War /
 * Heatmap toggle floating top-start, as on the web (`apps/web/src/map/MapView.tsx`).
 *
 * In Normal mode the Activities sheet (`panel/ActivitiesPanel`) lists the date range's
 * activities, the range defaulting to the five most recent activity days (`docs/SPEC.md`
 * FR-6.1). Tapping a track selects it; tapping empty map clears the selection. On the Stories
 * tab one Story is open, and the tracks and the list are its activities only ([RangeController.enterStory]).
 *
 * GPS recording is controlled from here: the record button in `MainActivity`'s bottom bar taps
 * to start, pause and resume, and a two-second hold stops (`RecordingService` does the rest; its
 * notification's Stop is confirmed here). While recording, the map shows only the live track
 * (`MapOverlays.setRecording`) and the camera follows the latest fix.
 *
 * `MainActivity` adds it only with a session. MapLibre's `MapView` lifecycle is forwarded from
 * this fragment's callbacks.
 */
class MapFragment : Fragment(R.layout.fragment_map) {

    private lateinit var mapView: MapView
    private lateinit var topChrome: View
    /** The notice under the top row. */
    private lateinit var notices: NoticePanel

    /** Whether the session lets the map show the user's layers. */
    private lateinit var gate: SessionGate
    /** The Normal / Fog / Heatmap toggle and what it says about the mode. */
    private lateinit var modes: ModeBar

    /** The Activities sheet, the date range at its head. */
    private lateinit var sheet: View
    private lateinit var sheetChrome: SheetChrome
    private lateinit var panel: ActivitiesPanel
    private val panelState = PanelState()
    /** The Edit window and the Private location editor along the bottom of the map. */
    private lateinit var edit: EditController
    private val editWindow: EditActivityWindow get() = edit.window

    /** The map image export's viewfinder, from the rail's camera. */
    private lateinit var export: ExportController

    /** Fog, Heatmap and every tile kept current with the account's renders and tile version. */
    private val coverage = CoverageController(
        style = { style?.takeIf { overlaysAttached } },
        range = { selectedRange },
        onRenderingChanged = { if (::modes.isInitialized) modes.renderCoverage() },
    )

    /** The selected activity's pace bands. */
    private val bands = PaceBands(
        panelState,
        style = { style?.takeIf { overlaysAttached } },
        onSyncTab = { ::panel.isInitialized && panel.tab == PanelTab.SYNC },
        onShown = { panel.setBandsShown(it) },
        // A reprocess can move a photo along its track, or take its stretch away.
        onRefetch = { photos.refresh(force = true) },
    )

    /** A View on map from the Sync screen ([viewOnMap]), the activity and its day, waiting for
     *  the session to be confirmed. */
    private var viewOnMapRequest: Pair<String, String>? = null

    /** Something the Sync tab sent has landed, whichever tab shows: the list, its days and the
     *  tiles catch up. Until then its job is only queued, with no row to show Pending. */
    private val syncProgressListener = SyncProgress.Listener(onChange = {}, onLanded = { onSyncedFromPhone() })
    /** The range or Story the list and tracks show, and whether the next list moves the camera. */
    private val session = MapSession(panelState)
    /** The slider, the default range and the list for the session's range or Story. */
    private lateinit var range: RangeController
    private lateinit var refresh: DataRefresh
    private lateinit var camera: CameraFramer
    private val selectedRange: DateRange? get() = session.range
    private val storyId: String? get() = session.storyId

    /** Set on every resume, since Sync or a recording may have added days; `syncSession`
     *  reloads them once the session allows. */
    private var daysStale = false

    private var map: MapLibreMap? = null
    private var style: Style? = null
    private var mode = MapMode.NORMAL

    /** The chrome's and MapLibre's controls' margins against the system bars and the panels. */
    private lateinit var chrome: ChromeInsets

    /** Whether the user layers are on the style (`syncSession`). */
    private var overlaysAttached = false

    /** Whether the map view is coming back from a recreation (rotation, theme, language) with
     *  the camera MapLibre saved, which the opening world view mustn't replace. */
    private var restoringCamera = false

    /** Whether `syncSession` has shown the mode toggle. Ending a recording mustn't show it
     *  before the session would have. */
    private var modeBarReady = false

    /** GPS recording: the record button, its status pill, the service binding and the camera's follow. */
    private val recording = RecordingController(
        this,
        onChanged = ::onRecordingChanged,
        styleLoaded = { style != null },
        map = { map },
        showMap = { host?.showMap() },
    )

    /** What the Sync tab has the map draw. */
    private val syncCandidates = SyncCandidatesController(style = { style?.takeIf { overlaysAttached } })

    /** The bottom bar's Sync came before the panel was built: it opens on Sync once it is. */
    private var pendingSync = false

    /** Photos on the map: the scope's markers, their popup and the Photos tab's picker. */
    private val photos = PhotosController(
        this,
        panelState,
        map = { map },
        editWindow = { editWindow },
        mode = { mode },
        storyId = { storyId },
        recording = ::isRecording,
        frame = { box, maxZoom -> camera.flyTo(box, maxZoom) },
        onPopupOpening = { spots.closePopup() },
    )

    /** Find my location's dot and tracking camera. */
    private val locate = LocateController(this, map = { map }, style = { style })

    /** Points of interest: the shown places, their badges and popup, and capture mode. */
    private val spots = SpotsController(this, map = { map }, style = { style?.takeIf { overlaysAttached } }, frame = { camera.flyTo(it) })

    /** The style's flavor and language, and the Layers menu's paths, places and satellite. */
    private val layers = LayersController(this, spots, style = { style }, attached = { overlaysAttached }, mode = { mode })

    /** The window this map is a tab of; null while the fragment is detached. */
    private val host get() = activity as? MainActivity

    /** The bottom bar's Stories: the panel's Stories tab. */
    fun showStories() {
        if (::panel.isInitialized) panel.showStories()
    }

    /** The bottom bar's Sync: the panel's Sync tab, in Normal mode, where the sheet is. */
    fun showSync() {
        if (!::panel.isInitialized) {
            pendingSync = true
            return
        }
        if (modeBarReady && mode != MapMode.NORMAL) setMode(MapMode.NORMAL)
        panel.showSync()
    }

    /**
     * The Privacy screen's Add a location ([PrivateLocationEditor.NEW]) or one of its rows: the
     * editor on the map, in Normal mode. Not while recording, which has the map.
     */
    fun editPrivateLocation(id: String) {
        if (!::edit.isInitialized || isRecording()) return
        if (mode != MapMode.NORMAL) setMode(MapMode.NORMAL)
        edit.privacy.start(id)
        edit.privacy.onStyleReady()
    }

    /** The bottom bar's Map: the panel's Activities tab, collapsed. */
    fun showActivities() {
        if (::panel.isInitialized) panel.showActivities()
    }

    /** Which panel tab shows, for the bottom bar's Stories mark. Null before the panel is built. */
    val panelTab: PanelTab? get() = if (::panel.isInitialized) panel.tab else null

    /** The fragment's own views, found the way an Activity finds its own. */
    private fun <T : View> findViewById(id: Int): T = requireView().findViewById(id)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        super.onViewCreated(view, savedInstanceState)

        topChrome = findViewById(R.id.top_chrome)
        notices = NoticePanel(
            findViewById(R.id.map_notice),
            recording = ::isRecording,
            onSync = { ::panel.isInitialized && panel.tab == PanelTab.SYNC },
        )
        gate = SessionGate(this, notices, onChecked = ::syncSession)
        modes = ModeBar(view, onPick = ::setMode) {
            ModeBarState(
                mode = mode,
                sessionReady = modeBarReady,
                recording = isRecording(),
                editOpen = ::edit.isInitialized && editWindow.isOpen,
                selectionShown = ::sheetChrome.isInitialized && sheetChrome.selectionShown(),
                exportOpen = ::export.isInitialized && export.isOpen,
                coverageRendering = coverage.rendering,
            )
        }
        layers.bind(view)
        spots.bind(view) { topChrome.bottom }
        photos.bind(view) { topChrome.bottom }
        recording.bind(view)

        locate.bind(view)
        sheet = findViewById(R.id.activities_sheet)
        export = ExportController(
            view,
            map = { map },
            styleLoaded = { style != null },
            style = { style },
            layers = layers,
            prepare = ::prepareExportStyle,
            chrome = listOf(topChrome, sheet, findViewById(R.id.photo_markers)),
            onShown = { shown ->
                modes.renderChip()
                if (shown) {
                    spots.closePopup()
                    photos.closePopup()
                    layers.dismiss()
                } else {
                    // The sheet comes back only where it belongs: not in Fog or Heatmap.
                    renderDateFooter()
                }
            },
        )
        val syncTab = SyncTab(
            findViewById(R.id.panel_sync_content),
            requireActivity() as AppCompatActivity,
            scope = { viewLifecycleOwner.lifecycleScope },
            onCandidates = syncCandidates::set,
            onFrame = { candidates -> BoxUnion.of(candidates.map { it.bbox }.filter { it.isNotEmpty() })?.let { camera.flyTo(it) } },
            onSynced = ::onSyncedFromPhone,
        )
        panel = ActivitiesPanel(
            sheet,
            findViewById(R.id.selection_bar),
            panelState,
            onMapChanged = ::applyTrackFilter,
            onFly = { camera.flyTo(it) },
            onEdit = ::openEditWindow,
            onDeleted = { refresh.afterDelete(it) },
            onTabChanged = { tab ->
                // Leaving the Stories tab closes its Story.
                if (tab != PanelTab.STORIES && storyId != null) range.exitStory()
                // The footer is the Activities tab's; renderDateFooter renders Privacy too.
                renderDateFooter()
                // The empty map's notice points to Sync, so it steps aside there.
                notices.render()
                // What's already synced steps aside for what's on offer.
                style?.takeIf { overlaysAttached }?.let { MapOverlays.setSyncing(it, tab == PanelTab.SYNC, mode) }
                // The bottom bar's Stories is this tab, so the bar follows it.
                host?.onPanelTabChanged(tab)
            },
            onOpenStory = { range.enterStory(it) },
            onCloseStory = { range.exitStory() },
            // A Story just made opens straight away, on the Stories tab.
            onStoryCreated = { story -> range.enterStory(story.id) },
            onStoriesChanged = {
                range.reloadList()
                // A Story's photos change with its members.
                photos.refresh(force = true)
            },
            onRemovedFromStory = { refresh.afterRemovedFromStory(it) },
            // The map has history now, so the empty notice goes.
            onStoryCopyArrived = {
                notices.hide(MapNotice.EMPTY)
                refresh.afterStoryCopy()
            },
            onStoriesWaiting = { host?.setStoriesWaiting(it) },
            onSheetChanged = { if (::sheetChrome.isInitialized) sheetChrome.onSheetChanged() },
            syncTab = syncTab,
        )
        camera = CameraFramer(view, panel, map = { map }, recording = ::isRecording, onEmpty = ::onEmptyChecked)
        range = RangeController(
            findViewById(R.id.date_footer),
            session,
            panelState,
            panel,
            style = { style?.takeIf { overlaysAttached } },
            bands = bands,
            coverage = coverage,
            editOpen = { ::edit.isInitialized && editWindow.isOpen },
            recording = ::isRecording,
            onSelectionReset = ::applyTrackFilter,
            onDaysChanged = ::renderDateFooter,
            fly = { camera.flyTo(it) },
        )
        refresh = DataRefresh(range, session, panel, panelState, style = { style?.takeIf { overlaysAttached } }, coverage, bands, photos)
        sheetChrome = SheetChrome(
            view,
            panel,
            range.days,
            normalMode = ::normalMode,
            sessionReady = { modeBarReady },
            mode = { mode },
            editOpen = { ::edit.isInitialized && editWindow.isOpen },
            privacyEditing = ::privacyEditing,
            onSheetMoved = { if (::chrome.isInitialized) chrome.applyAttributionMargin() },
            onSelectionBarChanged = ::renderModeBar,
            onRendered = {
                bands.render()
                renderPrivacy()
            },
            dismissLayers = layers::dismiss,
        )
        if (pendingSync) {
            pendingSync = false
            view.post { showSync() }
        }
        edit = EditController(
            view,
            panelState,
            panel = { panel },
            photos = photos,
            spots = spots,
            map = { map },
            style = { style },
            attached = { style?.takeIf { overlaysAttached } },
            mode = { mode },
            frame = { camera.flyTo(it) },
            openAreaCenterY = {
                val editor = findViewById<View>(R.id.private_editor)
                val top = findViewById<View>(R.id.top_bar).bottom
                val bottom = if (editor.isVisible) editor.top else mapView.height - if (sheet.isVisible) panel.peekHeight else 0
                (top + bottom) / 2f
            },
            onPrivateLocationsChanged = { refresh.afterPrivateLocations() },
            onWindowChanged = ::onEditWindowChanged,
            onPrivacyChanged = ::onPrivacyEditorChanged,
            onWindowClosed = ::onEditClosed,
        )
        chrome = ChromeInsets(findViewById(R.id.map_root), findViewById(R.id.top_bar), topChrome, notices.view, sheet, panel) { map }
        chrome.restore(savedInstanceState)
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, sheetChrome.collapseOnBack)
        // Added after the sheet's, so it's asked first: Back ends selecting before it closes the sheet.
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, sheetChrome.endSelectingOnBack)
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, edit.closeWindowOnBack)
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, edit.closePrivacyOnBack)
        // Last, so it's asked first.
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, export.backCallback)
        range.restore(savedInstanceState)
        // Carried through a recreation (a theme or language change), since the mode buttons
        // restore their own checked state and would otherwise show a mode the map isn't in.
        savedInstanceState?.getString(STATE_MODE)?.let { saved ->
            MapMode.entries.firstOrNull { it.name == saved }?.let { mode = it }
        }
        setMode(mode)

        chrome.install()
        if (savedInstanceState == null) {
            recording.handleIntent(requireActivity().intent)
            takeViewOnMap(requireActivity().intent)
        }

        mapView = findViewById(R.id.map_view)
        restoringCamera = savedInstanceState?.getBoolean(STATE_FRAMED) == true
        // The camera comes back with the view; framing it again would undo where the user was.
        if (restoringCamera) camera.framed = true
        mapView.onCreate(savedInstanceState)
        edit.takeHandleDrags(mapView)

        // Failures are reported here rather than only in logcat: a style or tile fetch that
        // 404s leaves a plausible-looking blank map behind, with nothing on screen to say so.
        mapView.addOnDidFailLoadingMapListener { error -> reportFailure(error) }

        mapView.getMapAsync { instance ->
            map = instance
            // A whole-world view is the honest starting camera until the session is verified
            // and the activity extent is known; `CameraFramer.frameLatest` replaces it with the user's
            // own, and leaves it for an account with no geometry yet. Not over a camera a
            // recreation brought back.
            if (!restoringCamera) {
                instance.cameraPosition = CameraPosition.Builder()
                    .target(LatLng(20.0, 0.0))
                    .zoom(1.0)
                    .build()
            }
            chrome.applyCompassMargin()
            chrome.applyAttributionMargin()
            mapView.post { chrome.enlargeAttributionTarget(mapView) }
            instance.addOnMapClickListener { point -> onMapTap(instance, point) }
            instance.addOnCameraMoveListener {
                spots.onCameraMove()
                photos.onCameraMove()
            }
            instance.addOnCameraIdleListener { photos.onCameraIdle() }
            spots.onMapReady(view, instance, layers.shownSpots())
            instance.addOnCameraIdleListener { spots.onCameraIdle() }
            instance.addOnCameraIdleListener { modes.onCameraIdle(instance.cameraPosition.zoom) }
            loadStyle()
        }
    }

    /**
     * Loads the served style and puts the live track and, once the session allows, the user
     * layers on it. "Try again" re-runs it after a failed load. MapLibre's attribution control
     * must stay enabled: it renders the ODbL credit the basemap requires.
     */
    private fun loadStyle() {
        val instance = map ?: return
        style = null
        overlaysAttached = false
        instance.setStyle(Style.Builder().fromUri(layers.styleUrl())) { loaded ->
            style = loaded
            notices.hide(MapNotice.MAP_FAILED)
            layers.onStyleLoaded(loaded)
            MapOverlays.attachLiveTrack(loaded)
            syncSession()
            recording.render()
        }
    }

    /** A new intent to the host (`MainActivity.onNewIntent`): the notification's Stop, or the
     *  Sync screen's View on map. */
    fun onNewIntent(intent: Intent) {
        recording.handleIntent(intent)
        takeViewOnMap(intent)
    }

    /** The Sync screen's View on map ([viewOnMap]): kept until the session is confirmed
     *  (`syncSession`); the opening camera is skipped, since this activity is the view. */
    private fun takeViewOnMap(intent: Intent) {
        // A relaunch from recents replays the intent; it has been handled.
        if (intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0) return
        val id = intent.getStringExtra(MainActivity.EXTRA_VIEW_ACTIVITY) ?: return
        val day = intent.getStringExtra(MainActivity.EXTRA_VIEW_DAY) ?: return
        viewOnMapRequest = id to day
        if (::camera.isInitialized) camera.framed = true
    }

    /** Brings the map in line with the stored credential ([SessionGate]), on style load and on every resume. */
    private fun syncSession() {
        if (!gate.passes()) return

        modeBarReady = true
        renderModeBar()
        if (daysStale) {
            daysStale = false
            range.days.reload()
            // Sync or a recording may have changed the range's activities and an open Story too.
            range.reloadList()
            panel.storiesTab.reloadOpen()
        }
        renderDateFooter()
        viewOnMapRequest?.let { (id, day) ->
            viewOnMapRequest = null
            viewActivityOnMap(id, day)
        }

        val loaded = style ?: return
        if (!overlaysAttached) {
            MapOverlays.attach(loaded, mode, selectedRange, layers.darkBase(loaded), layers.isNight(), storyId)
            // The bike-path layers attach just added start hidden.
            layers.applyPaths(loaded)
            MapOverlays.setTrackFilter(loaded, panelState.mapHidden, panelState.highlighted)
            spots.attach(loaded, layers.shownSpots())
            overlaysAttached = true
            spots.onStyleAttached()
            bands.render()
            // A new style has none of the circles; draw them again if the editor has the map.
            edit.privacy.onStyleReady()
            // Nor the Sync tab's lines, which the account's tracks still step aside for.
            syncCandidates.draw()
            MapOverlays.setSyncing(loaded, ::panel.isInitialized && panel.tab == PanelTab.SYNC, mode)
            if (isRecording()) MapOverlays.setRecording(loaded, true, mode)
            camera.frameLatest()
            coverage.checkTileVersion()
        }
    }

    /** A Sync history row's View on map: Normal mode, the collapsed Activities tab, and the
     *  activity selected and flown to ([RangeController.viewActivity]). */
    private fun viewActivityOnMap(activityId: String, day: String) {
        setMode(MapMode.NORMAL)
        // The expanded sheet would stay drawn over the very map the link is meant to show.
        panel.showActivities()
        range.viewActivity(activityId, day)
    }

    /** The toolbar's Edit ([EditController.open]). */
    private fun openEditWindow(group: List<Activity>) = edit.open(group)

    /** The Edit window opened or closed: the chrome steps aside for it or comes back. */
    private fun onEditWindowChanged(open: Boolean) {
        if (open) chrome.placeEditors() else range.offerDefault()
        renderDateFooter()
        host?.setBottomBarShown(!open && !privacyEditing())
        sheetChrome.renderSelectionBar()
        renderModeBar()
        sheetChrome.renderRail()
        bands.render()
    }

    /** The Private location editor opened or closed. */
    private fun onPrivacyEditorChanged(open: Boolean) {
        if (open) chrome.placeEditors()
        sheetChrome.renderRail()
        renderDateFooter()
        host?.setBottomBarShown(!open)
    }

    private fun onEditClosed(saved: Boolean, editedTrack: String?) {
        editedTrack?.let(range.pending::awaitEdit)
        if (saved) range.reloadList()
    }

    /** Whether the Private location editor is open over the map. */
    private fun privacyEditing() = ::edit.isInitialized && edit.privacyEditing

    /** The Private location editor has the map in Normal mode only: another mode or a recording
     *  closes it, unsaved draft and all. */
    private fun renderPrivacy() {
        if (!::edit.isInitialized || !edit.privacy.isShowing) return
        if (modeBarReady && !normalMode()) edit.privacy.stop()
    }

    /** The Sync tab sent activities; an empty map frames them once they're in. */
    private fun onSyncedFromPhone() {
        refresh.afterSync()
        if (notices.shown == MapNotice.EMPTY) camera.frameLatestAgain()
    }

    private fun renderModeBar() = modes.render()

    /** Applies the panel's hidden set, filters, Pending rows and selection to the track layers,
     *  the bands, the sheet's head and the selection bar. */
    private fun applyTrackFilter() {
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackFilter(it, panelState.mapHidden, panelState.highlighted) }
        bands.update(refetch = false)
        photos.refresh(force = false)
        if (::sheetChrome.isInitialized) renderDateFooter()
    }

    /**
     * A tap on the map in Normal mode: a track within [TAP_TOLERANCE_DP] (a 2.5dp line is too thin
     * to hit) is selected and flown to; empty map clears the selection. The sheet stays as it was.
     */
    private fun onMapTap(instance: MapLibreMap, point: LatLng): Boolean {
        // A spot's badge first, in every mode, unless an editor has the map; a tap elsewhere
        // closes its popup.
        if (!edit.privacy.isShowing && !editWindow.isOpen && spots.tap(instance, point)) return true
        // The Sync tab's lines: one tapped is picked out on its list.
        if (::panel.isInitialized && panel.tab == PanelTab.SYNC && sheet.isVisible) {
            syncCandidates.keyAt(instance, point, resources.displayMetrics.density)?.let(panel.syncTab::focus)
            return true
        }
        if (!normalMode()) return false
        // The Private location editor has the map to itself while it's open.
        if (edit.privacy.isShowing) {
            edit.privacy.onMapTap(point)
            return true
        }
        // The Edit window's group mustn't change under it; a tap only serves Delete point.
        if (editWindow.isOpen) {
            if (editWindow.trackEditor.deleteMode) {
                TrackEditOverlay.pointAt(instance, point, resources.displayMetrics.density)?.let(editWindow.trackEditor::dropPoint)
            }
            return true
        }
        val screen = instance.projection.toScreenLocation(point)
        val tolerance = TAP_TOLERANCE_DP * resources.displayMetrics.density
        val box = RectF(screen.x - tolerance, screen.y - tolerance, screen.x + tolerance, screen.y + tolerance)
        val id = instance.queryRenderedFeatures(box, MapOverlays.TRACKS_LAYER_ID)
            .firstNotNullOfOrNull { it.getStringProperty("id") }
        if (id != null && panelState.activities.any { it.id == id }) panel.trackTapped(id) else panel.clearFocus()
        return true
    }

    private fun renderDateFooter() = sheetChrome.render()

    private fun isRecording() = recording.isRecording

    /** Normal mode, session ready, not recording: when the sheet and its editors have the map. */
    private fun normalMode() = MapChromeRules.normalMode(modeBarReady, mode, isRecording())

    /** The map follows the recording: while one runs the live track has the map, and the mode
     *  toggle, Find my location, the export and every history layer step aside. */
    private fun onRecordingChanged(active: Boolean, points: List<RecordedPoint>) {
        renderModeBar()
        locate.setShown(!active)
        export.setAvailable(!active)
        modes.renderChip()
        renderDateFooter()
        notices.render()
        // Like every history layer, the photos give the map to the live track.
        photos.render()

        val loaded = style ?: return
        if (overlaysAttached) MapOverlays.setRecording(loaded, active, mode)
        MapOverlays.updateLiveTrack(loaded, points)
    }

    private fun setMode(next: MapMode) {
        mode = next
        modes.check(next)
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setMode(it, next) }
        style?.let { MapSatellite.apply(it, layers.satelliteOn, next) }
        modes.renderNotices()
        renderDateFooter()
        photos.refresh(force = false)
    }

    /**
     * Makes the export's hidden map (`export/MapImageRenderer`) look like this one, with the calls
     * [loadStyle] and [syncSession] make. No Spots, photos or recording, as on the web.
     */
    private fun prepareExportStyle(loaded: Style) {
        layers.applyPaths(loaded)
        MapSatellite.apply(loaded, layers.satelliteOn, mode)
        MapOverlays.attach(loaded, mode, selectedRange, layers.darkBase(loaded), layers.isNight(), storyId)
        layers.applyPaths(loaded)
        MapOverlays.setTrackFilter(loaded, panelState.mapHidden, panelState.highlighted)
    }

    /** What [CameraFramer.frameLatest] found. An account with no geometry stays at the world view,
     *  with a notice pointing at Sync, re-checked on every resume. Not for a demo, which has history
     *  and can't sync. */
    private fun onEmptyChecked(empty: Boolean) {
        if (!empty) {
            notices.hide(MapNotice.EMPTY)
        } else if (!Session.isDemo) {
            notices.show(MapNotice.EMPTY, getString(R.string.map_empty), action = R.string.nav_sync) {
                host?.showTab(MainActivity.Tab.SYNC)
            }
        }
    }

    /** The style or its sources failed: a plain sentence, the origin and MapLibre's error in
     *  small print, and Try again. */
    private fun reportFailure(error: String) {
        Log.e(TAG, "map failed to load: $error")
        notices.show(
            MapNotice.MAP_FAILED,
            getString(R.string.map_load_failed),
            detail = "${BuildConfig.API_BASE_URL} — $error",
            failed = true,
            action = R.string.retry,
        ) { loadStyle() }
    }

    // MapLibre's MapView holds a native renderer and a GL surface: every lifecycle callback below
    // is forwarded, since a missed one leaks the surface or crashes on rotation.
    override fun onStart() {
        super.onStart()
        mapView.onStart()
        SyncProgress.addListener(syncProgressListener)
        recording.start()
    }

    override fun onResume() {
        super.onResume()
        mapView.onResume()
        daysStale = true
        syncSession()
        if (overlaysAttached) {
            spots.loadCaptures(onlyNew = true)
            // Back from elsewhere (the web, Sync): new tiles if the version moved, and the notice
            // if the account's coverage is still being worked on.
            coverage.checkTileVersion()
        }
        spots.resume()
        // An empty map is checked again on every return, so the notice goes and the camera frames
        // the history once something arrives.
        if (notices.shown == MapNotice.EMPTY) camera.frameLatestAgain()
        // Back from Health Connect's settings or a recording's Edit: the Sync tab reads again.
        if (::panel.isInitialized && !isHidden) panel.syncTab.resume()
    }

    /** The You tab over the map, or the map back: the Sync tab stops reading, or reads again. */
    override fun onHiddenChanged(hidden: Boolean) {
        super.onHiddenChanged(hidden)
        if (hidden && ::export.isInitialized) export.close()
        if (!::panel.isInitialized) return
        if (hidden) panel.syncTab.pause() else panel.syncTab.resume()
    }

    override fun onPause() {
        spots.pause()
        mapView.onPause()
        super.onPause()
    }

    override fun onStop() {
        // Health Connect's routes can't be read in the background (`docs/IMPLEMENTATION.md` §4.0).
        if (::panel.isInitialized) panel.syncTab.pause()
        range.pending.stop()
        SyncProgress.removeListener(syncProgressListener)
        recording.stop()
        mapView.onStop()
        super.onStop()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        if (view != null) {
            mapView.onSaveInstanceState(outState)
            // The camera MapLibre just saved is where the user left it, once it's been framed.
            outState.putBoolean(STATE_FRAMED, camera.framed)
        }
        outState.putString(STATE_MODE, mode.name)
        if (::range.isInitialized) range.save(outState)
        if (::chrome.isInitialized) chrome.save(outState)
    }

    override fun onLowMemory() {
        super.onLowMemory()
        if (::mapView.isInitialized) mapView.onLowMemory()
    }

    override fun onDestroyView() {
        export.close()
        recording.dismissDialogs()
        layers.dismiss()
        spots.stopCapture()
        mapView.onDestroy()
        range.release()
        coverage.stop()
        super.onDestroyView()
    }

    companion object {
        private const val TAG = "HoldMyTrack"

        /** How far from a tap a track still counts as tapped, as on the web. */
        private const val TAP_TOLERANCE_DP = 14

        private const val STATE_MODE = "mode"
        private const val STATE_FRAMED = "framed"
    }
}
