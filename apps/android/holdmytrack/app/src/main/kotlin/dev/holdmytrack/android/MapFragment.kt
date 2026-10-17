package dev.holdmytrack.android

import android.content.Intent
import android.graphics.RectF
import android.os.Bundle
import android.util.Log
import android.view.View
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import androidx.core.view.isVisible
import androidx.fragment.app.Fragment
import dev.holdmytrack.android.export.ExportController
import dev.holdmytrack.android.map.ActivityDays
import dev.holdmytrack.android.map.BoxUnion
import dev.holdmytrack.android.map.ChromeInsets
import dev.holdmytrack.android.map.TrackEditOverlay
import dev.holdmytrack.android.map.DateRange
import dev.holdmytrack.android.map.DateRangeSlider
import dev.holdmytrack.android.map.DefaultRange
import dev.holdmytrack.android.map.FrameRules
import dev.holdmytrack.android.map.MapChromeRules
import dev.holdmytrack.android.map.MapMode
import dev.holdmytrack.android.map.MapNotice
import dev.holdmytrack.android.map.MapOverlays
import dev.holdmytrack.android.map.MapSession
import dev.holdmytrack.android.panel.EditController
import dev.holdmytrack.android.map.CoverageController
import dev.holdmytrack.android.map.PendingTracker
import dev.holdmytrack.android.map.PaceBands
import dev.holdmytrack.android.map.ModeBarState
import dev.holdmytrack.android.map.ModeBar
import dev.holdmytrack.android.map.LayersController
import dev.holdmytrack.android.map.LocateController
import dev.holdmytrack.android.map.MapSatellite
import dev.holdmytrack.android.map.NoticePanel
import dev.holdmytrack.android.map.SpotsController
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.SessionGate
import dev.holdmytrack.android.panel.ActivitiesPanel
import dev.holdmytrack.android.panel.EditActivityWindow
import dev.holdmytrack.android.panel.PanelTab
import dev.holdmytrack.android.panel.PrivateLocationEditor
import dev.holdmytrack.android.panel.PanelState
import dev.holdmytrack.android.panel.SyncTab
import dev.holdmytrack.android.photos.PhotosController
import dev.holdmytrack.android.sync.SyncCandidatesController
import dev.holdmytrack.android.sync.SyncProgress
import dev.holdmytrack.android.recording.RecordingController
import dev.holdmytrack.android.recording.RecordingService
import dev.holdmytrack.android.recording.RecordedPoint
import org.maplibre.android.camera.CameraPosition
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.geometry.LatLngBounds
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style
import java.time.LocalDate

/**
 * The map: the served basemap, the session the user layers need, and the Normal / Fog of War /
 * Heatmap toggle floating top-start, as on the web (`apps/web/src/map/MapView.tsx`).
 *
 * In Normal mode the Activities sheet (`panel/ActivitiesPanel`) lists the date range's
 * activities, the range defaulting to the five most recent activity days (`docs/SPEC.md`
 * FR-6.1). Tapping a track selects it; tapping empty map clears the selection. On the Stories
 * tab one Story is open, and the tracks and the list are its activities only ([enterStory]).
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
    private lateinit var mapRail: View

    /** The Activities sheet, the date range at its head. */
    private lateinit var sheet: View

    /** The toolbar while the sheet's rows are being checked, in the top row's place. */
    private lateinit var selectionBar: View
    private lateinit var panel: ActivitiesPanel
    private val panelState = PanelState()
    /** The Edit window and the Private location editor along the bottom of the map. */
    private lateinit var edit: EditController
    private val editWindow: EditActivityWindow get() = edit.window

    /** Back closes an open sheet before it leaves the map. */
    private val collapseSheetOnBack = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = panel.setExpanded(false)
    }

    /** Back unchecks every row, ending selecting, before it does anything else on the map. */
    private val endSelectingOnBack = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = panel.endSelecting()
    }

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

    /** A Sync history row's activity, to select once its range's list has it
     *  (`viewActivityOnMap`). */
    private var pendingFocusId: String? = null

    /** A View on map from the Sync screen ([viewOnMap]), the activity and its day, waiting for
     *  the session to be confirmed. */
    private var viewOnMapRequest: Pair<String, String>? = null

    /** Reprocesses running, and the list read again while any is. */
    private val pending = PendingTracker(host = { if (::mapView.isInitialized) mapView else null }, reload = { reloadList() })

    /** Something the Sync tab sent has landed, whichever tab shows: the list, its days and the
     *  tiles catch up. Until then its job is only queued, with no row to show Pending. */
    private val syncProgressListener = SyncProgress.Listener(onChange = {}, onLanded = { onSyncedFromPhone() })
    private lateinit var dateFooter: View
    private lateinit var dateSlider: DateRangeSlider
    private lateinit var activityDays: ActivityDays

    /** The range or Story the list and tracks show, and whether the next list moves the camera. */
    private val session = MapSession(panelState)
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

    /** Whether the camera has been framed on the activities this session. Once only: framing on
     *  every resume would fight the user for the camera. */
    private var framed = false

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
        frame = { box, maxZoom -> flyTo(box, maxZoom) },
        onPopupOpening = { spots.closePopup() },
    )

    /** Find my location's dot and tracking camera. */
    private val locate = LocateController(this, map = { map }, style = { style })

    /** Points of interest: the shown places, their badges and popup, and capture mode. */
    private val spots = SpotsController(this, map = { map }, style = { style?.takeIf { overlaysAttached } }, frame = { flyTo(it) })

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
                selectionShown = selectionShown(),
                exportOpen = ::export.isInitialized && export.isOpen,
                coverageRendering = coverage.rendering,
            )
        }
        mapRail = findViewById(R.id.map_rail)
        layers.bind(view)
        spots.bind(view) { topChrome.bottom }
        photos.bind(view) { topChrome.bottom }
        recording.bind(view)

        locate.bind(view)
        dateFooter = findViewById(R.id.date_footer)
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
        selectionBar = findViewById(R.id.selection_bar)
        val syncTab = SyncTab(
            findViewById(R.id.panel_sync_content),
            requireActivity() as AppCompatActivity,
            scope = { viewLifecycleOwner.lifecycleScope },
            onCandidates = syncCandidates::set,
            onFrame = { candidates -> BoxUnion.of(candidates.map { it.bbox }.filter { it.isNotEmpty() })?.let { flyTo(it) } },
            onSynced = ::onSyncedFromPhone,
        )
        panel = ActivitiesPanel(
            sheet,
            selectionBar,
            panelState,
            onMapChanged = ::applyTrackFilter,
            onFly = ::flyToActivities,
            onEdit = ::openEditWindow,
            onDeleted = ::onActivitiesDeleted,
            onTabChanged = { tab ->
                // Leaving the Stories tab closes its Story.
                if (tab != PanelTab.STORIES && storyId != null) exitStory()
                // The footer is the Activities tab's; renderDateFooter renders Privacy too.
                renderDateFooter()
                // The empty map's notice points to Sync, so it steps aside there.
                notices.render()
                // What's already synced steps aside for what's on offer.
                style?.takeIf { overlaysAttached }?.let { MapOverlays.setSyncing(it, tab == PanelTab.SYNC, mode) }
                // The bottom bar's Stories is this tab, so the bar follows it.
                host?.onPanelTabChanged(tab)
            },
            onOpenStory = ::enterStory,
            onCloseStory = ::exitStory,
            // A Story just made opens straight away, on the Stories tab.
            onStoryCreated = { story -> enterStory(story.id) },
            onStoriesChanged = {
                reloadList()
                // A Story's photos change with its members.
                photos.refresh(force = true)
            },
            onRemovedFromStory = ::onRemovedFromStory,
            onStoryCopyArrived = ::onStoryCopyArrived,
            onStoriesWaiting = { host?.setStoriesWaiting(it) },
            onSheetChanged = ::onSheetChanged,
            syncTab = syncTab,
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
            frame = ::flyToActivities,
            openAreaCenterY = {
                val editor = findViewById<View>(R.id.private_editor)
                val top = findViewById<View>(R.id.top_bar).bottom
                val bottom = if (editor.isVisible) editor.top else mapView.height - if (sheet.isVisible) panel.peekHeight else 0
                (top + bottom) / 2f
            },
            onPrivateLocationsChanged = ::onPrivateLocationsChanged,
            onWindowChanged = ::onEditWindowChanged,
            onPrivacyChanged = ::onPrivacyEditorChanged,
            onWindowClosed = ::onEditClosed,
        )
        chrome = ChromeInsets(findViewById(R.id.map_root), findViewById(R.id.top_bar), topChrome, notices.view, sheet, panel) { map }
        chrome.restore(savedInstanceState)
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, collapseSheetOnBack)
        // Added after the sheet's, so it's asked first: Back ends selecting before it closes the sheet.
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, endSelectingOnBack)
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, edit.closeWindowOnBack)
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, edit.closePrivacyOnBack)
        // Last, so it's asked first.
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, export.backCallback)
        activityDays = ActivityDays(DateRangeSlider.WINDOW_DAYS, ::onActivityDaysChanged)
        dateSlider = DateRangeSlider(
            dateFooter,
            onPan = activityDays::panBy,
            onShift = activityDays::shift,
            canShift = activityDays::canShift,
        ) { range ->
            session.pickRange(range)
            showRange(reset = true)
        }
        savedInstanceState?.getString(STATE_RANGE_FROM)?.let { from ->
            val to = savedInstanceState.getString(STATE_RANGE_TO) ?: return@let
            session.restore(DateRange(from, to), savedInstanceState.getBoolean(STATE_RANGE_CHOSEN))
            dateSlider.value = selectedRange
        }
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
        if (restoringCamera) framed = true
        mapView.onCreate(savedInstanceState)
        edit.takeHandleDrags(mapView)

        // Failures are reported here rather than only in logcat: a style or tile fetch that
        // 404s leaves a plausible-looking blank map behind, with nothing on screen to say so.
        mapView.addOnDidFailLoadingMapListener { error -> reportFailure(error) }

        mapView.getMapAsync { instance ->
            map = instance
            // A whole-world view is the honest starting camera until the session is verified
            // and the activity extent is known; `frameActivities` replaces it with the user's
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
        framed = true
    }

    /** Brings the map in line with the stored credential ([SessionGate]), on style load and on every resume. */
    private fun syncSession() {
        if (!gate.passes()) return

        modeBarReady = true
        renderModeBar()
        if (daysStale) {
            daysStale = false
            activityDays.reload()
            // Sync or a recording may have changed the range's activities and an open Story too.
            reloadList()
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
            frameActivities()
            coverage.checkTileVersion()
        }
    }

    /** The slider's window moved or reloaded; a reload re-derives the default range. */
    private fun onActivityDaysChanged(reloaded: Boolean) {
        dateSlider.setDays(activityDays.visibleDays, activityDays.canPanEarlier, activityDays.canPanLater)
        if (reloaded) offerDefaultRange()
        renderDateFooter()
    }

    /** The five most recent activity days, through today (`docs/SPEC.md` FR-6.1), unless the user
     *  picked a range; held while the Edit window is open, and offered again when it closes. */
    private fun offerDefaultRange() {
        if (!activityDays.ready) return
        val range = DefaultRange.of(activityDays.visibleDays.map { it.date }, activityDays.earliest, LocalDate.now().toString())
        if (session.offerDefault(range, editOpen = editWindow.isOpen)) showRange(reset = false)
    }

    /** Draws the tracks for the session's range and lists its activities; with [reset], the
     *  panel's emptied selection reaches the map too (`MapSession.pickRange`). */
    private fun showRange(reset: Boolean) {
        val range = selectedRange ?: return
        dateSlider.value = range
        if (reset) applyTrackFilter()
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackRange(it, range) }
        reloadList()
    }

    /** Lists the open Story's activities, else the range's, in the panel; frames every drawn one
     *  when the session asked for a flight. */
    private fun reloadList() {
        val range = selectedRange
        val story = storyId
        if (story == null && range == null) return
        panel.setLoading()
        HoldMyTrackApi.activities(range?.from.takeIf { story == null }, range?.to.takeIf { story == null }, story) { result ->
            // A later pick, or another Story, owns the list now.
            if (story != storyId || (story == null && range != selectedRange)) return@activities
            result.onSuccess { activities ->
                panel.setActivities(activities)
                trackPending(activities)
                pendingFocusId?.takeIf { id -> activities.any { it.id == id } }?.let { id ->
                    pendingFocusId = null
                    panel.focusFromMap(id)
                }
                // A recording started since owns the camera.
                if (session.consumeFly() && !isRecording()) flyToActivities(activities.filter { !it.pending })
            }.onFailure { failure ->
                Log.w(TAG, "could not load the activity list", failure)
                session.consumeFly()
                panel.setError(getString(R.string.map_unreachable))
                // A failed read doesn't end the polling: a Pending row still has to be seen finishing.
                pending.readFailed()
            }
        }
    }

    /**
     * Pending rows ([PendingTracker]): a row starting or stopping Pending refetches the track tiles
     * and watches Fog and Heatmap until re-rendered; a finished one also reloads the days.
     */
    private fun trackPending(activities: List<Activity>) {
        val change = pending.track(activities)
        // The selected activity's reprocess landed: its metrics describe the old points.
        if (panelState.focused in change.landed) bands.update(refetch = true)
        if (panelState.focused in change.left) bands.update(refetch = true)
        if (change.started || change.finished) {
            style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
            coverage.watch()
        }
        if (change.finished) {
            activityDays.reload()
            panel.storiesTab.reloadOpen()
        }
    }

    /**
     * A Sync history row's View on map: Normal mode, the collapsed Activities tab, and the
     * activity selected and flown to. When its [day] (its local date where it was recorded) is
     * outside the range, the range becomes that day, as if picked, and the selection waits for
     * that list.
     */
    private fun viewActivityOnMap(activityId: String, day: String) {
        setMode(MapMode.NORMAL)
        // The expanded sheet would stay drawn over the very map the link is meant to show.
        panel.showActivities()
        val range = selectedRange
        if (range != null && day >= range.from && day <= range.to && panelState.activities.any { it.id == activityId }) {
            panel.focusFromMap(activityId)
            return
        }
        pendingFocusId = activityId
        session.viewDay(day)
        showRange(reset = true)
    }

    /**
     * Opens Story [id]: the panel's state resets as for a new range, the tracks and the list
     * become the Story's activities, and the camera fits them.
     */
    private fun enterStory(id: String) {
        session.enterStory(id)
        applyTrackFilter()
        panel.showStories()
        panel.storiesTab.setOpen(id)
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackStory(it, id, null) }
        reloadList()
    }

    /** Closes the open Story: back to the range, the panel's state reset. The camera stays. */
    private fun exitStory() {
        session.exitStory()
        panel.storiesTab.setOpen(null)
        applyTrackFilter()
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackStory(it, null, selectedRange) }
        if (selectedRange != null) reloadList() else panel.setActivities(emptyList())
    }

    /** An activity taken out of the open Story: its track and row go, and its selection. The
     *  tile version moved, and the tiles follow. */
    private fun onRemovedFromStory(activityId: String) {
        if (panelState.focused == activityId) panel.clearFocus()
        photos.refresh(force = true)
        coverage.checkTileVersion()
        style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
        reloadList()
    }

    /** The toolbar's Edit ([EditController.open]). */
    private fun openEditWindow(group: List<Activity>) = edit.open(group)

    /** The Edit window opened or closed: the chrome steps aside for it or comes back. */
    private fun onEditWindowChanged(open: Boolean) {
        if (open) chrome.placeEditors() else offerDefaultRange()
        renderDateFooter()
        host?.setBottomBarShown(!open && !privacyEditing())
        renderSelectionBar()
        renderModeBar()
        renderRail()
        bands.render()
    }

    /** The Private location editor opened or closed. */
    private fun onPrivacyEditorChanged(open: Boolean) {
        if (open) chrome.placeEditors()
        renderRail()
        renderDateFooter()
        host?.setBottomBarShown(!open)
    }

    private fun onEditClosed(saved: Boolean, editedTrack: String?) {
        editedTrack?.let(pending::awaitEdit)
        if (saved) reloadList()
    }

    /** Whether the Private location editor is open over the map. */
    private fun privacyEditing() = ::edit.isInitialized && edit.privacyEditing

    /** The Private location editor has the map in Normal mode only: another mode or a recording
     *  closes it, unsaved draft and all. */
    private fun renderPrivacy() {
        if (!::edit.isInitialized || !edit.privacy.isShowing) return
        if (modeBarReady && !normalMode()) edit.privacy.stop()
    }

    /**
     * A Private location saved or deleted: the activities it touches are reprocessed. The list is
     * read at once, and everything again once the server drains, since a batch that finishes
     * before the first read never shows a row Pending.
     */
    private fun onPrivateLocationsChanged() {
        reloadList()
        coverage.watch {
            style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
            reloadList()
            activityDays.reload()
            bands.update(refetch = true)
            photos.refresh(force = true)
            panel.storiesTab.reloadOpen()
        }
    }

    /** A Story copy arrived (`docs/SPEC.md` FR-14.8): new activities, refreshed as after an
     *  upload. The map has history now, so the empty notice goes. */
    private fun onStoryCopyArrived() {
        notices.hide(MapNotice.EMPTY)
        style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
        reloadList()
        activityDays.reload()
        coverage.watch()
    }

    /** The Sync tab sent activities: the list, its days and the tiles catch up. */
    private fun onSyncedFromPhone() {
        activityDays.reload()
        reloadList()
        // Tracks refetch at once; the tile version moves only after the coverage render.
        style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
        coverage.checkTileVersion()
        if (notices.shown == MapNotice.EMPTY) {
            framed = false
            frameActivities()
        }
    }

    /** The toolbar's Delete finished: tracks, list and days read again, and Fog and Heatmap
     *  watched until re-rendered. Deleted ids leave the selection with the reload. */
    private fun onActivitiesDeleted(ids: List<String>) {
        if (ids.isEmpty()) return
        style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
        reloadList()
        activityDays.reload()
        coverage.watch()
        panel.storiesTab.reloadOpen()
        photos.refresh(force = true)
    }

    private fun renderModeBar() = modes.render()

    /** The rail (Layers, Find my location), hidden while an editor is open over the map. */
    private fun renderRail() {
        val editing = editWindow.isOpen || privacyEditing()
        mapRail.isVisible = MapChromeRules.railShown(editing, selectionShown())
        if (editing) layers.dismiss()
    }

    /** Applies the panel's hidden set, filters, Pending rows and selection to the track layers,
     *  the bands, the sheet's head and the selection bar. */
    private fun applyTrackFilter() {
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackFilter(it, panelState.mapHidden, panelState.highlighted) }
        bands.update(refetch = false)
        photos.refresh(force = false)
        if (::panel.isInitialized) renderDateFooter()
    }

    /** Whether rows are being checked where the selection bar can show: Normal mode, the
     *  Activities tab, no Edit window. */
    private fun selectionShown(): Boolean =
        ::panel.isInitialized && MapChromeRules.selectionShown(sheet.isVisible, panel.selecting, panel.tab, editWindow.isOpen)

    /** The selection bar replaces the top row while [selectionShown]. */
    private fun renderSelectionBar() {
        val shown = selectionShown()
        endSelectingOnBack.isEnabled = shown
        if (selectionBar.isVisible == shown) return
        selectionBar.visibility = if (shown) View.VISIBLE else View.GONE
        if (!shown) panel.dismissPopups()
        renderModeBar()
        renderRail()
    }

    /** Frames every one of [activities] that has a track. */
    private fun flyToActivities(activities: List<Activity>) {
        flyTo(BoxUnion.of(activities.mapNotNull { it.bbox }) ?: return)
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

    /** The sheet came to rest or its head changed height: the attribution follows its head, and
     *  Back closes it while it's open. */
    private fun onSheetChanged() {
        if (::chrome.isInitialized) chrome.applyAttributionMargin()
        collapseSheetOnBack.isEnabled = sheet.isVisible && panel.expanded && !editWindow.isOpen
    }

    /** The sheet is Normal mode's only (`docs/SPEC.md` FR-4.2), hidden while recording, and waits
     *  for the session. The range at its head is the Activities tab's, waits for the first page of
     *  days, and is absent for an account with no activity, where the empty notice speaks. */
    private fun renderDateFooter() {
        val normal = normalMode()
        // The selected activity's card takes the range's place at the head.
        dateFooter.isVisible = MapChromeRules.dateFooterShown(normal, panel.tab, panel.showsCard, activityDays.ready, activityDays.earliest != null)
        // An editor over the map hides the sheet. The Sync tab stays while recording: what's on
        // the phone can be sent mid-walk.
        sheet.isVisible = MapChromeRules.sheetShown(normal, modeBarReady, mode, panel.tab, editWindow.isOpen || privacyEditing())
        if (!normal) panel.dismissPopups()
        onSheetChanged()
        renderSelectionBar()
        bands.render()
        renderPrivacy()
    }

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

    /**
     * Frames the account's most recent activity, once per session (`docs/SPEC.md` FR-4.5). The
     * whole history could zoom out to the world.
     *
     * An account with no geometry stays at the world view, with a notice pointing at Sync,
     * re-checked on every resume (`onResume`). Not for a demo, which has history and can't sync.
     */
    private fun frameActivities() {
        if (framed) return
        framed = true
        HoldMyTrackApi.latestActivityBounds { result ->
            if (result.isFailure) return@latestActivityBounds
            val box = result.getOrNull()
            if (box == null) {
                if (!Session.isDemo) {
                    notices.show(MapNotice.EMPTY, getString(R.string.map_empty), action = R.string.nav_sync) {
                        host?.showTab(MainActivity.Tab.SYNC)
                    }
                }
                return@latestActivityBounds
            }
            notices.hide(MapNotice.EMPTY)
            // Reopening the app mid-recording: the camera belongs to the live track.
            if (isRecording()) return@latestActivityBounds
            flyTo(box)
        }
    }

    /**
     * Flies to fit [box] (`[west, south, east, north]`, east past 180 across the antimeridian).
     * The zoom is capped: a tiny extent would land past the basemap's z14 data, on grey.
     */
    private fun flyTo(box: DoubleArray, maxZoom: Double = MAX_FRAME_ZOOM) {
        val instance = map ?: return
        val bounds = LatLngBounds.from(box[3], box[2], box[1], box[0])
        // Into the map left showing between the chrome row and the panel, not under either.
        val banner = findViewById<View>(R.id.capture_banner)
        val root = findViewById<View>(R.id.map_root)
        val editor = listOf(R.id.edit_window, R.id.private_editor).map { findViewById<View>(it) }.firstOrNull { it.isVisible }
        // Above where the sheet rests or is heading, not mid-drag: View on map collapses it and
        // flies at once.
        val covered = when {
            editor != null -> root.height - editor.top
            sheet.isVisible -> root.height - panel.restingTop(root.height)
            else -> 0
        }
        val padding = FrameRules.padding(
            rootHeight = root.height,
            chromeBottom = findViewById<View>(R.id.top_bar).bottom,
            bannerBottom = if (banner.isVisible) topChrome.top + banner.bottom else null,
            coveredBelow = covered,
            minSpanPx = (MIN_FRAME_SPAN_DP * resources.displayMetrics.density).toInt(),
        )
        val fitted = instance.getCameraForLatLngBounds(bounds, padding) ?: return
        val target = CameraPosition.Builder(fitted)
            .zoom(minOf(fitted.zoom, maxZoom))
            .build()
        instance.animateCamera(CameraUpdateFactory.newCameraPosition(target), FRAME_DURATION_MS)
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
        if (notices.shown == MapNotice.EMPTY) {
            framed = false
            frameActivities()
        }
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
        pending.stop()
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
            outState.putBoolean(STATE_FRAMED, framed)
        }
        outState.putString(STATE_MODE, mode.name)
        // The panel comes back on its Activities tab; an open Story never changed the range.
        selectedRange?.let {
            outState.putString(STATE_RANGE_FROM, it.from)
            outState.putString(STATE_RANGE_TO, it.to)
            outState.putBoolean(STATE_RANGE_CHOSEN, session.userPicked)
        }
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
        dateSlider.release()
        coverage.stop()
        super.onDestroyView()
    }

    companion object {
        private const val TAG = "HoldMyTrack"
        private const val MAX_FRAME_ZOOM = 15.0

        /** The least height of map a fit keeps between the top row and the sheet. */
        private const val MIN_FRAME_SPAN_DP = 96

        /** How far from a tap a track still counts as tapped, as on the web. */
        private const val TAP_TOLERANCE_DP = 14
        private const val FRAME_DURATION_MS = 900

        private const val STATE_MODE = "mode"
        private const val STATE_FRAMED = "framed"
        private const val STATE_RANGE_FROM = "range_from"
        private const val STATE_RANGE_TO = "range_to"
        private const val STATE_RANGE_CHOSEN = "range_chosen"
    }
}
