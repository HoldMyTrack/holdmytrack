package dev.holdmytrack.android

import android.annotation.SuppressLint
import android.content.Intent
import android.content.res.Configuration
import android.graphics.RectF
import android.os.Bundle
import android.util.Log
import android.view.View
import android.widget.Button
import android.widget.TextView
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import androidx.core.view.AccessibilityDelegateCompat
import androidx.core.view.ViewCompat
import androidx.core.view.isVisible
import androidx.core.view.accessibility.AccessibilityNodeInfoCompat
import androidx.fragment.app.Fragment
import com.google.android.material.button.MaterialButton
import dev.holdmytrack.android.export.ExportViewfinder
import dev.holdmytrack.android.map.ActivityDays
import dev.holdmytrack.android.map.BoxUnion
import dev.holdmytrack.android.map.ChromeInsets
import dev.holdmytrack.android.map.CoverageWatch
import dev.holdmytrack.android.map.EditPreview
import dev.holdmytrack.android.map.SyncCandidatesOverlay
import dev.holdmytrack.android.map.TrackEditOverlay
import dev.holdmytrack.android.map.DateRange
import dev.holdmytrack.android.map.DateRangeSlider
import dev.holdmytrack.android.map.DefaultRange
import dev.holdmytrack.android.map.FrameRules
import dev.holdmytrack.android.map.MapChromeRules
import dev.holdmytrack.android.map.MapMode
import dev.holdmytrack.android.map.MapNotice
import dev.holdmytrack.android.map.MapModeButton
import dev.holdmytrack.android.map.MapOverlays
import dev.holdmytrack.android.map.LayersMenu
import dev.holdmytrack.android.map.LocateController
import dev.holdmytrack.android.map.MapLayersSwitch
import dev.holdmytrack.android.map.MapBikePaths
import dev.holdmytrack.android.map.MapPaths
import dev.holdmytrack.android.map.MapSatellite
import dev.holdmytrack.android.map.MapSpots
import dev.holdmytrack.android.map.PendingDiff
import dev.holdmytrack.android.map.SpotsController
import dev.holdmytrack.android.map.ZoomLevelNotice
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.ApiException
import dev.holdmytrack.android.net.TrackMetrics
import dev.holdmytrack.android.net.TrackPoint
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.panel.ActivitiesPanel
import dev.holdmytrack.android.panel.ActivityFacets
import dev.holdmytrack.android.panel.EditActivityWindow
import dev.holdmytrack.android.panel.PanelTab
import dev.holdmytrack.android.panel.PrivateLocationEditor
import dev.holdmytrack.android.panel.PanelState
import dev.holdmytrack.android.panel.SyncTab
import dev.holdmytrack.android.photos.PhotosController
import dev.holdmytrack.android.sync.Candidate
import dev.holdmytrack.android.sync.SyncProgress
import dev.holdmytrack.android.recording.RecordingController
import dev.holdmytrack.android.recording.RecordingService
import dev.holdmytrack.android.ui.LargeText
import dev.holdmytrack.android.recording.RecordedPoint
import dev.holdmytrack.android.settings.AppLanguage
import dev.holdmytrack.android.web.WebPageActivity
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
    private lateinit var notice: View
    private lateinit var noticeText: TextView
    private lateinit var noticeDetail: TextView
    private lateinit var noticeAction: Button

    /** Which notice is up, if any ([MapNotice]). */
    private var shownNotice: MapNotice? = null

    /** Posted when a session check starts, so "Checking your session…" appears only if the
     *  check is slow enough to notice; removed when it answers. */
    private val showChecking = Runnable {
        showNotice(MapNotice.CHECKING, getString(R.string.checking_session))
    }
    private lateinit var modeBar: View
    private lateinit var modeButtons: Map<MapMode, MaterialButton>
    private lateinit var layersMenu: LayersMenu
    private lateinit var mapRail: View
    private lateinit var layersPanel: View
    private lateinit var modeChip: TextView

    /** Whether the served style has satellite imagery; the Layers menu offers it only then. */
    private var satelliteAvailable = false

    /** The Activities sheet, the date range at its head. */
    private lateinit var sheet: View

    /** The toolbar while the sheet's rows are being checked, in the top row's place. */
    private lateinit var selectionBar: View
    private lateinit var panel: ActivitiesPanel
    private val panelState = PanelState()
    private lateinit var editWindow: EditActivityWindow
    private lateinit var privateEditor: PrivateLocationEditor

    /** Back closes an open sheet before it leaves the map. */
    private val collapseSheetOnBack = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = panel.setExpanded(false)
    }

    /** Back unchecks every row, ending selecting, before it does anything else on the map. */
    private val endSelectingOnBack = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = panel.endSelecting()
    }

    /** Back closes the Edit window before it leaves the map. */
    private val closeEditOnBack = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = editWindow.back()
    }

    /** The map image export's viewfinder (`export/ExportViewfinder`), from the rail's camera. */
    private lateinit var exportViewfinder: ExportViewfinder

    /** Back closes the export viewfinder, nothing captured, before anything else on the map. */
    private val closeExportOnBack = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = exportViewfinder.close()
    }

    /** Back closes the Private location editor, unsaved, before it leaves the map. */
    private val closePrivateOnBack = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = privateEditor.stop()
    }

    /** Fog and Heatmap fetched again once the server has re-rendered them after a delete or a
     *  reprocess (`map/CoverageWatch`), and the notice that they're still being updated. */
    private val coverageWatch = CoverageWatch(
        onRendering = { rendering ->
            coverageRendering = rendering
            renderCoverageNotice()
        },
        onRefetch = { style?.takeIf { overlaysAttached }?.let(MapOverlays::refreshCoverage) },
    )

    /** What the coverage watch last read: the account's Fog/Heatmap are still being worked on. */
    private var coverageRendering = false
    private lateinit var coverageNotice: View

    /** The selected activity's pace bands (`renderTrackMetrics`); null with nothing selected,
     *  and while a new selection's are loading. */
    private var trackMetrics: TrackMetrics? = null
    private var trackMetricsFor: String? = null

    /** A Sync history row's activity, to select once its range's list has it
     *  (`viewActivityOnMap`). */
    private var pendingFocusId: String? = null

    /** A View on map from the Sync screen ([viewOnMap]), the activity and its day, waiting for
     *  the session to be confirmed. */
    private var viewOnMapRequest: Pair<String, String>? = null

    /** The activity whose track the Edit window's Track tab is editing, while it is. */
    private var editingTrackId: String? = null

    /**
     * Activities whose track edit was saved here and whose reprocess hasn't been seen finishing,
     * as the web's `awaitingEditIdsRef`. Trap: the job often finishes before the reload after
     * the save answers, so the row is never seen Pending at all.
     */
    private val awaitingEditIds = HashSet<String>()

    /** The ids the last list showed Pending, to notice a reprocess finishing. */
    private var pendingIds: Set<String> = emptySet()

    /** Re-reads the list while any of it is Pending. */
    private val pendingPoll = Runnable { reloadList() }

    /** Something the Sync tab sent has landed, whichever tab shows: the list, its days and the
     *  tiles catch up. Until then its job is only queued, with no row to show Pending. */
    private val syncProgressListener = SyncProgress.Listener(onChange = {}, onLanded = { onSyncedFromPhone() })
    private lateinit var dateFooter: View
    private lateinit var zoomLevelNotice: ZoomLevelNotice
    private lateinit var dateSlider: DateRangeSlider
    private lateinit var activityDays: ActivityDays

    /** The date range the tracks are drawn for; null until the first page of activity days
     *  has set the default, when the tracks are the whole history. */
    private var selectedRange: DateRange? = null

    /** The Story open on the Stories tab (`docs/SPEC.md` FR-14.6). While one is, the tracks and
     *  the list are all its activities; [selectedRange] is kept for when it closes. */
    private var storyId: String? = null

    /** Whether the user picked [selectedRange]. Until then it is the default, re-derived
     *  whenever the activity days reload, so a first sync on an empty account moves it onto
     *  what arrived. */
    private var userChangedRange = false

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

    /** Guards against a second session check while one is in flight; every `onResume` calls
     *  `syncSession`. */
    private var verifying = false

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

    /** What the Sync tab has the map draw (`map/SyncCandidatesOverlay`), kept for a style
     *  reload, which drops the layers. */
    private var syncCandidates: List<Candidate> = emptyList()
    private var syncHighlight: String? = null

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
        if (!::privateEditor.isInitialized || isRecording()) return
        if (mode != MapMode.NORMAL) setMode(MapMode.NORMAL)
        privateEditor.start(id)
        privateEditor.onStyleReady()
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
        notice = findViewById(R.id.map_notice)
        noticeText = findViewById(R.id.map_notice_text)
        noticeDetail = findViewById(R.id.map_notice_detail)
        noticeAction = findViewById(R.id.map_notice_action)
        modeBar = findViewById(R.id.mode_bar)
        modeButtons = mapOf(
            MapMode.NORMAL to findViewById(R.id.mode_normal),
            MapMode.FOG to findViewById(R.id.mode_fog),
            MapMode.HEATMAP to findViewById(R.id.mode_heatmap),
        )
        // One choice of three: each reads as a radio button, "1 of 3" (map/MapModeButton).
        modeButtons.values.forEachIndexed { index, button -> (button as? MapModeButton)?.position = index }
        ViewCompat.setAccessibilityDelegate(modeBar, object : AccessibilityDelegateCompat() {
            override fun onInitializeAccessibilityNodeInfo(host: View, info: AccessibilityNodeInfoCompat) {
                super.onInitializeAccessibilityNodeInfo(host, info)
                info.setCollectionInfo(AccessibilityNodeInfoCompat.CollectionInfoCompat.obtain(1, modeButtons.size, false, AccessibilityNodeInfoCompat.CollectionInfoCompat.SELECTION_MODE_SINGLE))
            }
        })
        modeButtons.forEach { (value, button) ->
            button.setOnClickListener { setMode(value) }
            button.minWidth = ChromeInsets.minTouchTargetPx(button)
            button.minimumWidth = ChromeInsets.minTouchTargetPx(button)
        }
        mapRail = findViewById(R.id.map_rail)
        layersPanel = findViewById(R.id.layers_panel)
        modeChip = findViewById(R.id.mode_chip)
        layersMenu = LayersMenu(
            button = findViewById(R.id.layers_button),
            count = findViewById(R.id.layers_count),
            paths = { MapPaths.get(requireContext()) },
            spots = { MapSpots.get(requireContext()) },
            shown = { MapLayersSwitch.isOn(requireContext()) },
            onPaths = ::setPaths,
            onSpots = ::setSpots,
            onShown = ::setLayersShown,
            satellite = { if (satelliteAvailable) MapSatellite.isOn(requireContext()) else null },
            onSatellite = ::setSatellite,
        )
        spots.bind(view) { topChrome.bottom }
        photos.bind(view) { topChrome.bottom }
        recording.bind(view)

        locate.bind(view)
        exportViewfinder = ExportViewfinder(
            host = findViewById(R.id.map_root),
            map = { map.takeIf { style != null } },
            styleUrl = ::styleUrl,
            prepare = ::prepareExportStyle,
            darkBase = { style?.let(::darkBase) ?: isNight() },
            satelliteOn = { satelliteAvailable && MapSatellite.isOn(requireContext()) },
            onShown = ::renderExportShown,
        )
        findViewById<View>(R.id.export_button).setOnClickListener { exportViewfinder.toggle() }

        dateFooter = findViewById(R.id.date_footer)
        zoomLevelNotice = ZoomLevelNotice(findViewById(R.id.zoom_level_notice))
        coverageNotice = findViewById(R.id.coverage_notice)
        sheet = findViewById(R.id.activities_sheet)
        selectionBar = findViewById(R.id.selection_bar)
        val syncTab = SyncTab(
            findViewById(R.id.panel_sync_content),
            requireActivity() as AppCompatActivity,
            scope = { viewLifecycleOwner.lifecycleScope },
            onCandidates = ::drawSyncCandidates,
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
                updateNoticeVisibility()
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
        privateEditor = PrivateLocationEditor(
            findViewById(R.id.private_editor),
            map = { map },
            style = { style?.takeIf { overlaysAttached } },
            onEditorOpen = {
                closePrivateOnBack.isEnabled = true
                chrome.placeEditors()
                renderRail()
                renderDateFooter()
                host?.setBottomBarShown(false)
            },
            // Closing the editor gives the map back.
            onEditorClose = {
                closePrivateOnBack.isEnabled = false
                if (privateEditor.isShowing) privateEditor.stop()
                renderRail()
                renderDateFooter()
                host?.setBottomBarShown(true)
            },
            onChanged = ::onPrivateLocationsChanged,
            openAreaCenterY = {
                val editor = findViewById<View>(R.id.private_editor)
                val top = findViewById<View>(R.id.top_bar).bottom
                val bottom = if (editor.isVisible) editor.top else mapView.height - if (sheet.isVisible) panel.peekHeight else 0
                (top + bottom) / 2f
            },
        )
        editWindow = EditActivityWindow(
            findViewById(R.id.edit_window),
            findViewById(R.id.edit_bar),
            onStartTrack = ::startEditTrack,
            onDrawTrack = ::drawTrackEdit,
            onPickPhotos = photos::pick,
            onPhotoOverlay = photos::setOverlay,
            onTabChanged = photos::render,
            onClose = ::onEditClosed,
        )
        chrome = ChromeInsets(findViewById(R.id.map_root), findViewById(R.id.top_bar), topChrome, notice, sheet, panel) { map }
        chrome.restore(savedInstanceState)
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, collapseSheetOnBack)
        // Added after the sheet's, so it's asked first: Back ends selecting before it closes the sheet.
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, endSelectingOnBack)
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, closeEditOnBack)
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, closePrivateOnBack)
        // Last, so it's asked first.
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, closeExportOnBack)
        activityDays = ActivityDays(DateRangeSlider.WINDOW_DAYS, ::onActivityDaysChanged)
        dateSlider = DateRangeSlider(
            dateFooter,
            onPan = activityDays::panBy,
            onShift = activityDays::shift,
            canShift = activityDays::canShift,
        ) { range ->
            userChangedRange = true
            applyRange(range, fly = true)
        }
        savedInstanceState?.getString(STATE_RANGE_FROM)?.let { from ->
            val to = savedInstanceState.getString(STATE_RANGE_TO) ?: return@let
            selectedRange = DateRange(from, to)
            dateSlider.value = selectedRange
            userChangedRange = savedInstanceState.getBoolean(STATE_RANGE_CHOSEN)
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
        takeHandleDrags()

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
            spots.onMapReady(view, instance, shownSpots(MapSpots.get(requireContext())))
            instance.addOnCameraIdleListener { spots.onCameraIdle() }
            instance.addOnCameraIdleListener { zoomLevelNotice.onCameraIdle(instance.cameraPosition.zoom) }
            loadStyle()
        }
    }

    /** A press on a Private location's handle (`PrivateLocationEditor.onMapTouch`) or on a track
     *  point in Move point mode (`TrackEditor.onMapTouch`) takes the touch before the map pans.
     *  Every other touch, taps included, reaches the map. */
    @SuppressLint("ClickableViewAccessibility")
    private fun takeHandleDrags() {
        mapView.setOnTouchListener { _, event ->
            when {
                privateEditor.isShowing -> privateEditor.onMapTouch(event)
                editWindow.isOpen -> map?.let { editWindow.trackEditor.onMapTouch(event, it, resources.displayMetrics.density) } ?: false
                else -> false
            }
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
        instance.setStyle(Style.Builder().fromUri(styleUrl())) { loaded ->
            style = loaded
            hideNotice(MapNotice.MAP_FAILED)
            // The served style ships the path layers hidden; a fresh style needs the saved choice.
            MapPaths.apply(loaded, shownPaths(MapPaths.get(requireContext())))
            // Only a deployment with imagery serves it; without, the Layers menu has no Satellite.
            satelliteAvailable = MapSatellite.isAvailable(loaded)
            MapSatellite.apply(loaded, MapSatellite.isOn(requireContext()), mode)
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

    /**
     * Brings the map in line with the stored credential, on style load and on every resume. No
     * session goes to `SignInActivity`.
     *
     * A token restored from disk isn't trusted until the server confirms it: a revoked one would
     * otherwise show as a blank map, its 401s visible only in logcat. An unconfirmed email goes
     * to `VerifyEmailActivity`, since every tile and sync request would be a `403`.
     */
    private fun syncSession() {
        if (!Session.isSignedIn) {
            openSignIn()
            return
        }
        if (!Session.verified) {
            verifyStoredSession()
            return
        }
        if (!Session.emailVerified) {
            openVerifyEmail()
            return
        }
        // The first run, as on the web: an account with no Country goes to Settings first. The
        // Country decides the units, and nothing else asks for it.
        if (!Session.isDemo && Session.country.isEmpty()) {
            WebPageActivity.openOnboarding(requireContext())
            requireActivity().finish()
            return
        }

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
            MapOverlays.attach(loaded, mode, selectedRange, darkBase(loaded), isNight(), storyId)
            // The bike-path layers attach just added start hidden.
            MapPaths.apply(loaded, shownPaths(MapPaths.get(requireContext())))
            MapOverlays.setTrackFilter(loaded, panelState.mapHidden, panelState.highlighted)
            spots.attach(loaded, shownSpots(MapSpots.get(requireContext())))
            overlaysAttached = true
            spots.onStyleAttached()
            renderTrackMetrics()
            // A new style has none of the circles; draw them again if the editor has the map.
            privateEditor.onStyleReady()
            // Nor the Sync tab's lines, which the account's tracks still step aside for.
            drawSyncCandidates(syncCandidates, syncHighlight)
            MapOverlays.setSyncing(loaded, ::panel.isInitialized && panel.tab == PanelTab.SYNC, mode)
            if (isRecording()) MapOverlays.setRecording(loaded, true, mode)
            frameActivities()
            checkTileVersion()
        }
    }

    /**
     * The overlays attach at the stored tile version ([Session.tileVersion]), so cached tiles draw
     * at once. This reads the current version and refetches every tile only if it moved while the
     * app was away.
     */
    private fun checkTileVersion() {
        HoldMyTrackApi.coverageStatus { result ->
            val status = result.getOrNull() ?: return@coverageStatus
            // Still rendering: watched until done, with the notice meanwhile and a refetch at the end.
            if (status.rendering) coverageWatch.watch()
            if (status.tileVersion.isEmpty() || status.tileVersion == Session.tileVersion) return@coverageStatus
            Session.tileVersion = status.tileVersion
            style?.takeIf { overlaysAttached }?.let {
                MapOverlays.refreshCoverage(it)
                MapOverlays.refreshTracks(it, selectedRange)
                // Loading places or bike paths bumps the version too.
                MapSpots.refresh(it)
                MapBikePaths.refresh(it)
            }
        }
    }

    /**
     * The slider's window moved or reloaded. A reload re-derives the default range until the
     * user picks one: the five most recent activity days, through today (`docs/SPEC.md` FR-6.1).
     */
    private fun onActivityDaysChanged(reloaded: Boolean) {
        dateSlider.setDays(activityDays.visibleDays, activityDays.canPanEarlier, activityDays.canPanLater)
        if (reloaded && !userChangedRange) {
            val range = DefaultRange.of(activityDays.visibleDays.map { it.date }, activityDays.earliest, LocalDate.now().toString())
            if (range != selectedRange) applyRange(range, fly = false)
        }
        renderDateFooter()
    }

    /**
     * Draws the tracks for [range] and lists its activities. Only a picked range ([fly]) moves
     * the camera and clears the selection, group, hidden set and filters (`docs/SPEC.md` FR-6.6);
     * a re-derived default does neither.
     */
    private fun applyRange(range: DateRange, fly: Boolean) {
        selectedRange = range
        dateSlider.value = range
        if (fly) {
            panelState.resetForNewRange()
            applyTrackFilter()
        }
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackRange(it, range) }
        loadActivities(range, fly)
    }

    /** Reads the list again for the open Story, else the range. */
    private fun reloadList() = loadActivities(selectedRange, fly = false)

    /** Lists [range]'s activities, or the open Story's, in the panel; with [fly], frames every
     *  drawn one. */
    private fun loadActivities(range: DateRange?, fly: Boolean) {
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
                if (fly && !isRecording()) flyToActivities(activities.filter { !it.pending })
            }.onFailure { failure ->
                Log.w(TAG, "could not load the activity list", failure)
                panel.setError(getString(R.string.map_unreachable))
                // A failed read doesn't end the polling: a Pending row still has to be seen finishing.
                mapView.removeCallbacks(pendingPoll)
                if (pendingIds.isNotEmpty()) mapView.postDelayed(pendingPoll, PENDING_POLL_MS)
            }
        }
    }

    /**
     * Pending rows (a reprocess running, from here or another client): while any is, the list is
     * read again every [PENDING_POLL_MS]. A row starting or stopping Pending refetches the track
     * tiles and watches Fog and Heatmap until re-rendered; a finished one also reloads the days.
     */
    private fun trackPending(activities: List<Activity>) {
        val change = PendingDiff.of(pendingIds, activities.filter { it.pending }.mapTo(HashSet()) { it.id }, awaitingEditIds)
        awaitingEditIds.retainAll(change.awaiting)
        // The selected activity's reprocess landed: its metrics describe the old points.
        if (panelState.focused in change.landed) updateTrackMetrics(refetch = true)
        if (panelState.focused in change.left) updateTrackMetrics(refetch = true)
        pendingIds = change.now
        if (change.started || change.finished) {
            style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
            coverageWatch.watch()
        }
        if (change.finished) {
            activityDays.reload()
            panel.storiesTab.reloadOpen()
        }
        mapView.removeCallbacks(pendingPoll)
        if (change.now.isNotEmpty()) mapView.postDelayed(pendingPoll, PENDING_POLL_MS)
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
        userChangedRange = true
        panelState.resetForNewRange()
        applyTrackFilter()
        // The selection flies to the activity itself; the range's own fly would override it.
        applyRange(DateRange(day, day), fly = false)
    }

    /**
     * Opens Story [id]: the panel's state resets as for a new range, the tracks and the list
     * become the Story's activities, and the camera fits them.
     */
    private fun enterStory(id: String) {
        storyId = id
        panelState.resetForNewRange()
        applyTrackFilter()
        panel.showStories()
        panel.storiesTab.setOpen(id)
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackStory(it, id, null) }
        loadActivities(null, fly = true)
    }

    /** Closes the open Story: back to the range, the panel's state reset. The camera stays. */
    private fun exitStory() {
        storyId = null
        panel.storiesTab.setOpen(null)
        panelState.resetForNewRange()
        applyTrackFilter()
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackStory(it, null, selectedRange) }
        if (selectedRange != null) reloadList() else panel.setActivities(emptyList())
    }

    /** An activity taken out of the open Story: its track and row go, and its selection. The
     *  tile version moved, and the tiles follow. */
    private fun onRemovedFromStory(activityId: String) {
        if (panelState.focused == activityId) panel.clearFocus()
        photos.refresh(force = true)
        checkTileVersion()
        style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
        reloadList()
    }

    /** The toolbar's Edit: the window over its target, the panel held and inert under it, and
     *  the mode toggle away. */
    private fun openEditWindow(group: List<Activity>) {
        panel.dismissPopups()
        panel.hold(true)
        editWindow.open(group, ActivityFacets.typeFacets(panelState.activities, null))
        // The photos follow the window: its one activity's, which its Photos tab manages.
        photos.refresh(force = false)
        closeEditOnBack.isEnabled = true
        chrome.placeEditors()
        renderDateFooter()
        host?.setBottomBarShown(false)
        renderSelectionBar()
        renderModeBar()
        renderRail()
        renderTrackMetrics()
    }

    /** The Track tab opened: the other tracks and the bands away, and the camera on this one. */
    private fun startEditTrack(activity: Activity) {
        editingTrackId = activity.id
        style?.takeIf { overlaysAttached }?.let {
            MapOverlays.setEditingTrack(it, true, mode)
        }
        spots.setEditing(true)
        // Once the Track tab has laid out, so the fit leaves room under the window as it now is.
        findViewById<View>(R.id.edit_window).post { flyToActivities(listOf(activity)) }
    }

    /** The track editor's overlay: the points as edited, its knobs and what would go. */
    private fun drawTrackEdit(visible: List<TrackPoint>?, lo: Int, hi: Int, preview: EditPreview, split: Int?) {
        val loaded = style ?: return
        if (visible == null) TrackEditOverlay.clear(loaded) else TrackEditOverlay.set(loaded, visible, lo, hi, preview, split)
    }

    private fun onEditClosed(saved: Boolean, trackApplied: Boolean, photosSaved: Boolean) {
        photos.editClosed(photosSaved)
        editingTrackId?.let { id ->
            if (trackApplied) awaitingEditIds += id
            style?.let(TrackEditOverlay::clear)
            style?.takeIf { overlaysAttached }?.let { MapOverlays.setEditingTrack(it, false, mode) }
            spots.setEditing(false)
        }
        editingTrackId = null
        closeEditOnBack.isEnabled = false
        panel.hold(false)
        renderDateFooter()
        host?.setBottomBarShown(!privacyEditing())
        renderSelectionBar()
        renderModeBar()
        renderRail()
        renderTrackMetrics()
        if (saved) reloadList()
    }

    /** Whether the Private location editor is open over the map. */
    private fun privacyEditing() = ::privateEditor.isInitialized && privateEditor.isEditing

    /** The Private location editor has the map in Normal mode only: another mode or a recording
     *  closes it, unsaved draft and all. */
    private fun renderPrivacy() {
        if (!::privateEditor.isInitialized || !privateEditor.isShowing) return
        if (modeBarReady && !normalMode()) privateEditor.stop()
    }

    /**
     * A Private location saved or deleted: the activities it touches are reprocessed. The list is
     * read at once, and everything again once the server drains, since a batch that finishes
     * before the first read never shows a row Pending.
     */
    private fun onPrivateLocationsChanged() {
        reloadList()
        coverageWatch.watch {
            style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
            reloadList()
            activityDays.reload()
            updateTrackMetrics(refetch = true)
            photos.refresh(force = true)
            panel.storiesTab.reloadOpen()
        }
    }

    /** A Story copy arrived (`docs/SPEC.md` FR-14.8): new activities, refreshed as after an
     *  upload. The map has history now, so the empty notice goes. */
    private fun onStoryCopyArrived() {
        hideNotice(MapNotice.EMPTY)
        style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
        reloadList()
        activityDays.reload()
        coverageWatch.watch()
    }

    /** The Sync tab's lines over the map, or none as it leaves. */
    private fun drawSyncCandidates(candidates: List<Candidate>, highlight: String?) {
        syncCandidates = candidates
        syncHighlight = highlight
        val loaded = style?.takeIf { overlaysAttached } ?: return
        if (candidates.isEmpty()) SyncCandidatesOverlay.clear(loaded) else SyncCandidatesOverlay.set(loaded, candidates, highlight)
    }

    /** The Sync tab sent activities: the list, its days and the tiles catch up. */
    private fun onSyncedFromPhone() {
        activityDays.reload()
        reloadList()
        // Tracks refetch at once; the tile version moves only after the coverage render.
        style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
        checkTileVersion()
        if (shownNotice == MapNotice.EMPTY) {
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
        coverageWatch.watch()
        panel.storiesTab.reloadOpen()
        photos.refresh(force = true)
    }

    /** The mode toggle shows once the session allows, hidden while recording or editing. Layers
     *  stays while recording: the base map and paths are about where the recording goes. */
    private fun renderModeBar() {
        if (!modeBarReady) return
        modeBar.isVisible = MapChromeRules.modeBarShown(isRecording(), editWindow.isOpen, selectionShown())
        layersPanel.visibility = View.VISIBLE
        renderZoomLevelNotice()
        renderCoverageNotice()
        renderModeChip()
    }

    /** Names what Fog or Heatmap shows, since neither takes the date range. Not while recording,
     *  which draws neither. */
    private fun renderModeChip() {
        val text = when (mode) {
            MapMode.NORMAL -> null
            MapMode.FOG -> R.string.mode_chip_fog
            MapMode.HEATMAP -> R.string.mode_chip_heatmap
        }
        modeChip.isVisible = MapChromeRules.modeChipShown(mode, modeBarReady, isRecording(), ::exportViewfinder.isInitialized && exportViewfinder.isOpen)
        if (text != null) modeChip.setText(text)
    }

    /** Names Fog's and Heatmap's level in view; not while recording. */
    private fun renderZoomLevelNotice() {
        zoomLevelNotice.render(mode, active = modeBarReady && !isRecording())
    }

    /** "Your map is still being updated…" (`docs/SPEC.md` FR-4.2), in Fog and Heatmap while the
     *  coverage watch finds coverage rendering. */
    private fun renderCoverageNotice() {
        if (!::coverageNotice.isInitialized) return
        coverageNotice.isVisible = MapChromeRules.coverageNoticeShown(coverageRendering, mode, modeBarReady, isRecording())
    }

    /** The rail (Layers, Find my location), hidden while an editor is open over the map. */
    private fun renderRail() {
        val editing = editWindow.isOpen || privacyEditing()
        mapRail.isVisible = MapChromeRules.railShown(editing, selectionShown())
        if (editing) layersMenu.dismiss()
    }

    /** Applies the panel's hidden set, filters, Pending rows and selection to the track layers,
     *  the bands, the sheet's head and the selection bar. */
    private fun applyTrackFilter() {
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackFilter(it, panelState.mapHidden, panelState.highlighted) }
        updateTrackMetrics(refetch = false)
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

    /**
     * The selected activity's metrics: fetched when the selection moves, again with [refetch]
     * after its reprocess lands, and dropped when it clears.
     */
    private fun updateTrackMetrics(refetch: Boolean) {
        val focused = panelState.focused
        if (focused == null) {
            trackMetrics = null
            trackMetricsFor = null
            renderTrackMetrics()
            return
        }
        if (focused == trackMetricsFor && !refetch) {
            renderTrackMetrics()
            return
        }
        // A reprocess can move a photo along its track, or take its stretch away.
        if (refetch) photos.refresh(force = true)
        trackMetricsFor = focused
        if (!refetch) trackMetrics = null
        renderTrackMetrics()
        HoldMyTrackApi.trackMetrics(focused) { result ->
            if (panelState.focused != focused) return@trackMetrics
            trackMetrics = result.getOrNull()
            renderTrackMetrics()
        }
    }

    /**
     * The selected activity's pace bands, while its track is drawn: not hidden, filtered out or
     * Pending (its metrics would show the old points). Not on the Sync tab, whose picked candidate
     * has its own (`map/SyncCandidatesOverlay`).
     */
    private fun renderTrackMetrics() {
        val metrics = trackMetrics
        val loaded = style?.takeIf { overlaysAttached } ?: return
        val focused = panelState.focused
        val onSync = ::panel.isInitialized && panel.tab == PanelTab.SYNC
        if (metrics != null && focused == metrics.activityId && focused !in panelState.mapHidden && !onSync) {
            MapOverlays.setTrackBands(loaded, metrics.points)
            panel.setBandsShown(true)
        } else {
            MapOverlays.clearTrackBands(loaded)
            panel.setBandsShown(false)
        }
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
        if (!privateEditor.isShowing && !editWindow.isOpen && spots.tap(instance, point)) return true
        // The Sync tab's lines: one tapped is picked out on its list.
        if (::panel.isInitialized && panel.tab == PanelTab.SYNC && sheet.isVisible) {
            SyncCandidatesOverlay.keyAt(instance, instance.projection.toScreenLocation(point), resources.displayMetrics.density)
                ?.let(panel.syncTab::focus)
            return true
        }
        if (!normalMode()) return false
        // The Private location editor has the map to itself while it's open.
        if (privateEditor.isShowing) {
            privateEditor.onMapTap(point)
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
        renderTrackMetrics()
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
        // A recording has the map to itself; the export would draw what it hides.
        findViewById<View>(R.id.export_panel).visibility = if (active) View.GONE else View.VISIBLE
        if (active && ::exportViewfinder.isInitialized) exportViewfinder.close()
        renderModeChip()
        renderDateFooter()
        updateNoticeVisibility()
        // Like every history layer, the photos give the map to the live track.
        photos.render()

        val loaded = style ?: return
        if (overlaysAttached) MapOverlays.setRecording(loaded, active, mode)
        MapOverlays.updateLiveTrack(loaded, points)
    }

    private fun openSignIn() {
        SignInActivity.open(requireContext())
        requireActivity().finish()
    }

    private fun openVerifyEmail() {
        VerifyEmailActivity.open(requireContext())
        requireActivity().finish()
    }

    private fun verifyStoredSession() {
        if (verifying) return
        verifying = true
        notice.postDelayed(showChecking, CHECKING_DELAY_MS)
        HoldMyTrackApi.verifySession { result ->
            verifying = false
            notice.removeCallbacks(showChecking)
            hideNotice(MapNotice.CHECKING)
            result.onSuccess { profile ->
                Session.markVerified(profile)
                AppLanguage.followAccount(requireContext().applicationContext, profile.locale)
                hideNotice(MapNotice.UNREACHABLE)
            }.onFailure { failure ->
                // Only a 401 means the token is dead. Any other failure keeps it for the next
                // resume, and the notice says why the map is empty.
                if (failure is ApiException && failure.code == 401) {
                    Session.clear()
                } else {
                    Log.w(TAG, "could not verify the stored session", failure)
                    showNotice(
                        MapNotice.UNREACHABLE,
                        getString(R.string.map_unreachable),
                        failed = true,
                        action = R.string.retry,
                    ) { verifyStoredSession() }
                }
            }
            syncSession()
        }
    }

    /**
     * Shows [text], an optional [detail] and at most one action. A less important notice never
     * replaces a more important one ([MapNotice]).
     */
    private fun showNotice(
        kind: MapNotice,
        text: String,
        detail: String? = null,
        failed: Boolean = false,
        action: Int? = null,
        onAction: (() -> Unit)? = null,
    ) {
        if (!MapChromeRules.noticeReplaces(shownNotice, kind)) return
        shownNotice = kind
        noticeText.text = text
        noticeText.setTextColor(requireContext().getColor(if (failed) R.color.hmt_danger else R.color.hmt_ink))
        noticeDetail.text = detail
        noticeDetail.visibility = if (detail == null) View.GONE else View.VISIBLE
        noticeAction.visibility = if (action == null) View.GONE else View.VISIBLE
        if (action != null) noticeAction.setText(action)
        noticeAction.setOnClickListener { onAction?.invoke() }
        updateNoticeVisibility()
    }

    /** Takes [kind] down if it's the one up; any other notice stays. */
    private fun hideNotice(kind: MapNotice) {
        if (shownNotice != kind) return
        shownNotice = null
        updateNoticeVisibility()
    }

    /** The empty notice steps aside while recording and on the Sync tab it points to; failures
     *  stay up. */
    private fun updateNoticeVisibility() {
        val onSync = ::panel.isInitialized && panel.tab == PanelTab.SYNC
        notice.isVisible = MapChromeRules.noticeShown(shownNotice, isRecording(), onSync)
    }

    /**
     * At a large font size only the active mode keeps its label; the others show icons
     * (`ui/LargeText`), still named for TalkBack. The layout's label is kept in the button's tag.
     */
    private fun renderModeLabel(button: MaterialButton, active: Boolean) {
        val label = (button.tag as? CharSequence) ?: button.text.also { button.tag = it }
        button.contentDescription = label
        if (!LargeText.isOn(resources)) return
        button.text = if (active) label else ""
        button.iconPadding = if (active) resources.getDimensionPixelSize(R.dimen.hmt_space_6) else 0
    }

    private fun setMode(next: MapMode) {
        mode = next
        modeButtons.forEach { (value, button) ->
            val active = value == next
            button.isChecked = active
            renderModeLabel(button, active)
        }
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setMode(it, next) }
        style?.let { MapSatellite.apply(it, satelliteAvailable && MapSatellite.isOn(requireContext()), next) }
        renderZoomLevelNotice()
        renderCoverageNotice()
        renderModeChip()
        renderDateFooter()
        photos.refresh(force = false)
    }

    /** The Layers menu's Points of interest. Unticking the open popup's category closes it. */
    private fun setSpots(categories: List<MapSpots.Category>) = spots.setPicked(categories, shownSpots(categories))

    private fun setPaths(paths: MapPaths.Paths) {
        MapPaths.set(requireContext(), paths)
        style?.let { MapPaths.apply(it, shownPaths(paths)) }
    }

    /** The Layers menu's Show layers: shows or hides every pick, keeping them. A capture in
     *  progress carries on. */
    private fun setLayersShown(on: Boolean) {
        MapLayersSwitch.set(requireContext(), on)
        style?.let { MapPaths.apply(it, shownPaths(MapPaths.get(requireContext()))) }
        spots.show(shownSpots(MapSpots.get(requireContext())))
    }

    /** The picks drawn: all while Show layers is on, none while off. */
    private fun shownPaths(paths: MapPaths.Paths) = if (MapLayersSwitch.isOn(requireContext())) paths else MapPaths.NONE

    private fun shownSpots(categories: List<MapSpots.Category>) =
        if (MapLayersSwitch.isOn(requireContext())) categories else emptyList()

    private fun setSatellite(on: Boolean) {
        MapSatellite.set(requireContext(), on)
        val loaded = style ?: return
        MapSatellite.apply(loaded, on, mode)
        if (overlaysAttached) MapOverlays.setDarkVeil(loaded, darkBase(loaded))
    }

    /**
     * Makes the export's hidden map (`export/MapImageRenderer`) look like this one, with the calls
     * [loadStyle] and [syncSession] make. No Spots, photos or recording, as on the web.
     */
    private fun prepareExportStyle(loaded: Style) {
        val context = requireContext()
        MapPaths.apply(loaded, shownPaths(MapPaths.get(context)))
        MapSatellite.apply(loaded, satelliteAvailable && MapSatellite.isOn(context), mode)
        MapOverlays.attach(loaded, mode, selectedRange, darkBase(loaded), isNight(), storyId)
        MapPaths.apply(loaded, shownPaths(MapPaths.get(context)))
        MapOverlays.setTrackFilter(loaded, panelState.mapHidden, panelState.highlighted)
    }

    /** While the viewfinder shows, the map's chrome steps aside, since it would cover the picture. */
    private fun renderExportShown(shown: Boolean) {
        closeExportOnBack.isEnabled = shown
        val visibility = if (shown) View.INVISIBLE else View.VISIBLE
        topChrome.visibility = visibility
        sheet.visibility = visibility
        findViewById<View>(R.id.photo_markers).visibility = visibility
        renderModeChip()
        if (shown) {
            spots.closePopup()
            photos.closePopup()
            layersMenu.dismiss()
        }
        map?.uiSettings?.apply {
            isLogoEnabled = !shown
            isAttributionEnabled = !shown
            isCompassEnabled = !shown
        }
    }

    /** Whether the basemap reads dark (dark flavor or satellite), which picks Fog's veil and
     *  Heatmap's wash (`MapOverlays.setDarkVeil`). */
    private fun darkBase(style: Style): Boolean =
        isNight() || (MapSatellite.isOn(requireContext()) && MapSatellite.isAvailable(style))

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
                    showNotice(MapNotice.EMPTY, getString(R.string.map_empty), action = R.string.nav_sync) {
                        host?.showTab(MainActivity.Tab.SYNC)
                    }
                }
                return@latestActivityBounds
            }
            hideNotice(MapNotice.EMPTY)
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

    /**
     * The style URL: `light` or `dark` following the app's night mode (`AppTheme`), labels in
     * the app's language (`?lang=`, root `docs/SPEC.md` FR-13.1).
     */
    private fun styleUrl(): String {
        val flavor = if (isNight()) "dark" else "light"
        val lang = resources.configuration.locales[0].language
        return "${BuildConfig.API_BASE_URL}/v1/map/style/$flavor?lang=$lang"
    }

    private fun isNight(): Boolean =
        resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK == Configuration.UI_MODE_NIGHT_YES

    /** The style or its sources failed: a plain sentence, the origin and MapLibre's error in
     *  small print, and Try again. */
    private fun reportFailure(error: String) {
        Log.e(TAG, "map failed to load: $error")
        showNotice(
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
            checkTileVersion()
        }
        spots.resume()
        // An empty map is checked again on every return, so the notice goes and the camera frames
        // the history once something arrives.
        if (shownNotice == MapNotice.EMPTY) {
            framed = false
            frameActivities()
        }
        // Back from Health Connect's settings or a recording's Edit: the Sync tab reads again.
        if (::panel.isInitialized && !isHidden) panel.syncTab.resume()
    }

    /** The You tab over the map, or the map back: the Sync tab stops reading, or reads again. */
    override fun onHiddenChanged(hidden: Boolean) {
        super.onHiddenChanged(hidden)
        if (hidden && ::exportViewfinder.isInitialized) exportViewfinder.close()
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
        if (::mapView.isInitialized) mapView.removeCallbacks(pendingPoll)
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
            outState.putBoolean(STATE_RANGE_CHOSEN, userChangedRange)
        }
        if (::chrome.isInitialized) chrome.save(outState)
    }

    override fun onLowMemory() {
        super.onLowMemory()
        if (::mapView.isInitialized) mapView.onLowMemory()
    }

    override fun onDestroyView() {
        exportViewfinder.close()
        recording.dismissDialogs()
        layersMenu.dismiss()
        spots.stopCapture()
        mapView.onDestroy()
        dateSlider.release()
        coverageWatch.stop()
        super.onDestroyView()
    }

    companion object {
        private const val TAG = "HoldMyTrack"
        private const val MAX_FRAME_ZOOM = 15.0

        /** The least height of map a fit keeps between the top row and the sheet. */
        private const val MIN_FRAME_SPAN_DP = 96

        /** How far from a tap a track still counts as tapped, as on the web. */
        private const val TAP_TOLERANCE_DP = 14

        /** How often a list with Pending rows is read again. */
        private const val PENDING_POLL_MS = 2_000L
        private const val FRAME_DURATION_MS = 900

        /** How long a session check runs before the notice shows, so a fast one doesn't flash it. */
        private const val CHECKING_DELAY_MS = 600L

        private const val STATE_MODE = "mode"
        private const val STATE_FRAMED = "framed"
        private const val STATE_RANGE_FROM = "range_from"
        private const val STATE_RANGE_TO = "range_to"
        private const val STATE_RANGE_CHOSEN = "range_chosen"
    }
}
