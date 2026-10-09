package dev.holdmytrack.android

import android.Manifest
import android.annotation.SuppressLint
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.RectF
import android.os.Bundle
import android.os.IBinder
import android.util.Log
import android.content.res.ColorStateList
import android.os.SystemClock
import android.view.HapticFeedbackConstants
import android.view.View
import android.view.ViewGroup.MarginLayoutParams
import android.view.WindowInsets
import android.widget.Button
import android.widget.Chronometer
import android.widget.TextView
import android.widget.Toast
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContract
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.health.connect.client.PermissionController
import androidx.lifecycle.lifecycleScope
import androidx.core.view.AccessibilityDelegateCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.isVisible
import androidx.core.view.accessibility.AccessibilityNodeInfoCompat
import androidx.fragment.app.Fragment
import com.google.android.material.button.MaterialButton
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.progressindicator.LinearProgressIndicator
import dev.holdmytrack.android.map.ActivityDays
import dev.holdmytrack.android.map.BoxUnion
import dev.holdmytrack.android.map.CaptureMode
import dev.holdmytrack.android.map.Geo
import dev.holdmytrack.android.map.CoverageWatch
import dev.holdmytrack.android.map.EditPreview
import dev.holdmytrack.android.map.SyncCandidatesOverlay
import dev.holdmytrack.android.map.TrackEditOverlay
import dev.holdmytrack.android.map.DateRange
import dev.holdmytrack.android.map.DateRangeSlider
import dev.holdmytrack.android.map.MapMode
import dev.holdmytrack.android.map.MapModeButton
import dev.holdmytrack.android.map.MapOverlays
import dev.holdmytrack.android.map.LayersMenu
import dev.holdmytrack.android.map.MapLayersSwitch
import dev.holdmytrack.android.map.MapPaths
import dev.holdmytrack.android.map.MapSatellite
import dev.holdmytrack.android.map.MapSpots
import dev.holdmytrack.android.map.PhotoMarkerItem
import dev.holdmytrack.android.map.PhotoMarkerOverlay
import dev.holdmytrack.android.map.PhotoMarkers
import dev.holdmytrack.android.map.PhotoPopup
import dev.holdmytrack.android.map.ShowInArea
import dev.holdmytrack.android.map.SpotPopup
import dev.holdmytrack.android.map.ZoomLevelNotice
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.Photo
import dev.holdmytrack.android.net.ApiException
import dev.holdmytrack.android.net.TrackMetrics
import dev.holdmytrack.android.net.TrackPoint
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Spot
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.panel.ActivitiesPanel
import dev.holdmytrack.android.panel.ActivityFacets
import dev.holdmytrack.android.panel.EditActivityWindow
import dev.holdmytrack.android.panel.PanelFormat
import dev.holdmytrack.android.panel.PanelTab
import dev.holdmytrack.android.panel.PrivateLocationEditor
import dev.holdmytrack.android.panel.PanelState
import dev.holdmytrack.android.panel.SyncTab
import dev.holdmytrack.android.sync.Candidate
import dev.holdmytrack.android.recording.RecordButton
import dev.holdmytrack.android.recording.RecordingFormat
import dev.holdmytrack.android.recording.RecordingService
import dev.holdmytrack.android.ui.LargeText
import dev.holdmytrack.android.recording.RecordingState
import dev.holdmytrack.android.recording.db.LiveRecordingJournal
import dev.holdmytrack.android.settings.AppLanguage
import dev.holdmytrack.android.settings.SettingsActivity
import org.maplibre.android.camera.CameraPosition
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.geometry.LatLngBounds
import org.maplibre.android.location.LocationComponentActivationOptions
import org.maplibre.android.location.modes.CameraMode
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style
import java.time.LocalDate

/**
 * The map, and everything that hangs off it: the served basemap, the session the user layers
 * need, and the Normal / Fog of War / Heatmap toggle between them. The toggle floats over the
 * map top-start, rather than living in a bar of its own, mirroring the web client's own on-map
 * mode control (`apps/web/src/map/MapView.tsx`).
 *
 * Along the bottom in Normal mode, the web's phone layout: the Activities panel
 * (`panel/ActivitiesPanel`), a sheet listing the range's activities, on the date range the
 * tracks are drawn for — the web's phone footer (`map/DateRangeSlider`), defaulting to the five
 * most recent activity days as the web does (`docs/SPEC.md` FR-6.1). Tapping a track selects
 * it in the panel, and tapping empty map clears the selection. On the panel's Stories tab one
 * Story is open, and the tracks, the list and the date range are its activities only
 * ([enterStory]).
 *
 * Also the one place GPS recording is controlled from in the app: the record button raised in
 * the middle of `MainActivity`'s bottom bar (tap to start, tap to pause/resume, hold for two
 * seconds to stop —
 * `RecordingService` does the rest, and its notification offers the same controls, its Stop
 * confirmed here first). While a recording is in
 * progress the map shows only that recording's live track: the mode toggle and every history
 * layer are hidden (`MapOverlays.setRecording`), and the camera follows the latest fix.
 *
 * Hosted by `MainActivity`, which never adds it without a session ([MainActivity.onCreate]).
 * MapLibre's `MapView` lifecycle is forwarded from this fragment's own callbacks.
 */
class MapFragment : Fragment(R.layout.fragment_map) {

    private lateinit var mapView: MapView
    private lateinit var topChrome: View
    private lateinit var notice: View
    private lateinit var noticeText: TextView
    private lateinit var noticeDetail: TextView
    private lateinit var noticeAction: Button

    /** Which notice is up, if any — see [Notice]. */
    private var shownNotice: Notice? = null

    /** Posted when a session check starts, so "Checking your session…" appears only if the
     *  check is slow enough to notice; removed when it answers. */
    private val showChecking = Runnable {
        showNotice(Notice.CHECKING, getString(R.string.checking_session))
    }
    private lateinit var modeBar: View
    private lateinit var modeButtons: Map<MapMode, MaterialButton>
    private lateinit var layersMenu: LayersMenu
    private lateinit var spotPopup: SpotPopup
    private lateinit var photoMarkers: PhotoMarkers
    private lateinit var photoPopup: PhotoPopup
    private lateinit var captureMode: CaptureMode
    /** Created with the map instance it reads the camera of. */
    private var showInArea: ShowInArea? = null
    private lateinit var recordButton: RecordButton
    private lateinit var mapRail: View
    private lateinit var layersPanel: View
    private lateinit var modeChip: TextView

    /** Whether the served style has satellite imagery — the Layers menu offers it only then. */
    private var satelliteAvailable = false
    private lateinit var recordingStatus: View
    private lateinit var recordingStatusDot: View
    private lateinit var recordingStatusTime: Chronometer
    private lateinit var recordingStatusDetail: TextView
    private lateinit var recordingStatusHold: LinearProgressIndicator

    /** Whether a hold on the record button is counting down, which the status pill shows in
     *  place of its detail line. */
    private var holdingToStop = false
    private lateinit var locateButton: MaterialButton

    /** Find my location's panel — what hides while recording, so no empty panel is left. */
    private lateinit var locatePanel: View

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

    /** The selected activity's `GET /v1/activities/track-metrics`, its pace bands
     *  (`renderTrackMetrics`); null with nothing selected, and while a new selection's is on
     *  its way. */
    private var trackMetrics: TrackMetrics? = null
    private var trackMetricsFor: String? = null

    /** A Sync history row's activity, to select once its range's list has it
     *  (`viewActivityOnMap`). */
    private var pendingFocusId: String? = null

    /** A View on map from the Sync screen ([viewOnMap]) — the activity and its day —
     *  waiting for the session to be confirmed. */
    private var viewOnMapRequest: Pair<String, String>? = null

    /** The activity whose track the Edit window's Track tab is editing, while it is. */
    private var editingTrackId: String? = null

    /**
     * Activities whose track edit was saved from here and whose reprocess hasn't been seen
     * finishing yet — the web's `awaitingEditIdsRef`. Seeing a row go Pending and back isn't
     * enough on its own: the job is often done before the reload that follows the save even
     * answers, so the row is never seen Pending at all.
     */
    private val awaitingEditIds = HashSet<String>()

    /** The ids the last list showed Pending — how a reprocess finishing is noticed. */
    private var pendingIds: Set<String> = emptySet()

    /** Re-reads the list while any of it is Pending, the web's `EDIT_PENDING_POLL_MS`. */
    private val pendingPoll = Runnable { reloadList() }
    private lateinit var dateFooter: View
    private lateinit var zoomLevelNotice: ZoomLevelNotice
    private lateinit var dateSlider: DateRangeSlider
    private lateinit var activityDays: ActivityDays

    /** The date range the tracks are drawn for; null until the first page of activity days
     *  has set the default, when the tracks are the whole history. */
    private var selectedRange: DateRange? = null

    /** The Story open on the Stories tab (`docs/SPEC.md` FR-14.6), or null: while one is, the
     *  tracks and the list are all of its activities, whatever [selectedRange] is — which
     *  opening a Story leaves alone, so closing it goes straight back. */
    private var storyId: String? = null

    /** Whether the user has picked [selectedRange] themselves. Until then it is the default,
     *  re-derived whenever the activity days reload — so a first sync on an empty account
     *  moves it onto what arrived, as the web's does. */
    private var userChangedRange = false

    /** Set on every resume, since Sync or a recording may have added days; `syncSession`
     *  reloads them once the session allows. The default range follows until the user picks
     *  one. */
    private var daysStale = false

    private var map: MapLibreMap? = null
    private var style: Style? = null
    private var mode = MapMode.NORMAL

    /** The top inset (status bar height), applied to the floating chrome and to MapLibre's own
     *  compass — see `insetSystemBars` and `applyCompassMargin`. Read before either the inset
     *  or the map instance is necessarily available yet, so both paths call the latter once
     *  they have what they need. */
    private var systemBarInsetTop = 0

    /** The compass's own default top margin, captured once so repeated inset callbacks (e.g.
     *  a rotation) add the status bar height on top of it rather than compounding it. */
    private var compassBaseMarginTop = 0
    private var compassBaseMarginCaptured = false

    /** The logo's and attribution's own bottom margins, captured once, like the compass's —
     *  and carried through `onSaveInstanceState`, since MapLibre restores its UI settings with
     *  the lift already applied, and capturing those again would lift them twice. */
    private var logoBaseMarginBottom = 0
    private var attributionBaseMarginBottom = 0
    private var attributionBaseCaptured = false

    /** Whether the user layers are currently on the style — see `syncSession`. */
    private var overlaysAttached = false

    /** Whether this session's activity extent has already framed the camera. Once per session:
     *  re-framing on every resume would fight the user for control of the camera. */
    private var framed = false

    /** Whether the map view is coming back from a recreation — a rotation, a theme or language
     *  change — with the camera MapLibre saved, which the opening world view mustn't replace. */
    private var restoringCamera = false

    /** Guards against a second `GET /v1/auth/me` while the first is still in flight — every
     *  `onResume` calls `syncSession`, and returning from the Profile screen is one. */
    private var verifying = false

    /** Whether `syncSession` has got as far as showing the mode toggle — recording hides it,
     *  and ending a recording must not show it before the session would have. */
    private var modeBarReady = false

    /** Bound from `onStart` to `onStop` to watch the recording; null between the two, or
     *  before the bind answers. Null reads as [RecordingState.IDLE]. */
    private var recorder: RecordingService? = null
    private var recorderBound = false

    /** Whether the camera has already flown in to the current recording's first fix; later
     *  fixes only pan, so the user's chosen zoom sticks. Reset when a recording ends. */
    private var followingRecording = false

    /** The prompt for a recording a process death left behind, while it's up — every bind
     *  asks again, and returning to the map mustn't stack a second one on it. */
    private var leftoverDialog: AlertDialog? = null

    /** The notification's Stop confirmation, while it's up; and whether one is owed to a Stop
     *  that arrived before the bind answered, to be asked once it does. */
    private var stopDialog: AlertDialog? = null
    private var pendingStopConfirm = false

    private val recorderConnection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName, binder: IBinder) {
            val service = (binder as RecordingService.LocalBinder).service
            recorder = service
            service.onChange = ::renderRecording
            renderRecording()
            if (pendingStopConfirm) {
                pendingStopConfirm = false
                confirmStop()
            }
            if (service.state == RecordingState.IDLE) service.findLeftover(::offerLeftover)
        }

        override fun onServiceDisconnected(name: ComponentName) {
            recorder = null
            renderRecording()
        }
    }

    /** Asked for on the first tap of the record button, not up front: location is what
     *  recording needs, and asking at the moment of recording is when the reason is obvious.
     *  Notifications ride along — without them the recording runs invisibly, with no Pause or
     *  Stop in the shade — but only location is required to start. */
    private val permissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions(),
    ) {
        if (hasPermission(Manifest.permission.ACCESS_FINE_LOCATION)) {
            startRecording()
        } else {
            Toast.makeText(requireContext(), R.string.recording_needs_location, Toast.LENGTH_LONG).show()
        }
    }

    /** Asked for on a spot's Capture, when location isn't granted yet. Capture counts only
     *  fixes accurate to 25 m, so it needs precise location; approximate alone isn't enough. */
    private val capturePermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions(),
    ) {
        val spot = pendingCapture
        pendingCapture = null
        if (hasPermission(Manifest.permission.ACCESS_FINE_LOCATION)) {
            spot?.let(captureMode::start)
        } else {
            Toast.makeText(requireContext(), R.string.spots_capture_needs_location, Toast.LENGTH_LONG).show()
        }
    }

    /** The spot whose Capture is waiting on the permission prompt. */
    private var pendingCapture: Spot? = null

    /** Asked for on the first tap of Find my location. Approximate is enough to show the
     *  user roughly where they are, so either grant counts. */
    private val locatePermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions(),
    ) {
        if (hasLocationPermission()) {
            showMyLocation()
        } else {
            Toast.makeText(requireContext(), R.string.locate_needs_location, Toast.LENGTH_LONG).show()
        }
    }

    /** The Sync tab's Health Connect permissions (`sync/HealthConnectCard`). */
    private val healthPermissionLauncher = registerForActivityResult(
        @Suppress("UNCHECKED_CAST")
        PermissionController.createRequestPermissionResultContract()
            as ActivityResultContract<Set<String>, Set<String>>,
    ) { if (::panel.isInitialized) panel.syncTab.permissionsAnswered() }

    /** What the Sync tab has the map draw (`map/SyncCandidatesOverlay`), kept for a style
     *  reload, which drops the layers. */
    private var syncCandidates: List<Candidate> = emptyList()
    private var syncHighlight: String? = null

    /** The bottom bar's Sync came before the panel was built: it opens on Sync once it is. */
    private var pendingSync = false

    /** The Photos tab's Add photos: Android's photo picker, any number of images, with no
     *  storage permission. What's picked goes to the tab to prepare and place. */
    private val photoPickLauncher = registerForActivityResult(ActivityResultContracts.PickMultipleVisualMedia()) { uris ->
        if (editWindow.isOpen) editWindow.photosTab.add(uris)
    }

    /**
     * Whose photos the map shows (`docs/SPEC.md` FR-16.7), the web's `photoScope` — in Normal
     * mode only: the one activity the Edit window is open on, whose Photos tab manages them (none
     * for a group); else the selected activity's, the route a person is looking at; else the open
     * Story's, the whole trip's pictures along its days. As `"activity:<id>"` or `"story:<id>"`.
     */
    private var photoScope: String? = null

    /** The scope's saved photos, in route order, and why they couldn't be read. */
    private var photos: List<Photo> = emptyList()
    private var photosError: String? = null
    private var photosLoaded = false
    private var photoGeneration = 0

    /** The Photos tab's unsaved changes as they alter the markers, while it shows. */
    private var photoOverlay: PhotoMarkerOverlay? = null

    /** The window this map is a tab of — null while the fragment is detached. */
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
     * The Privacy screen's Add a location on the map ([PrivateLocationEditor.NEW]) or one of its
     * rows: the editor on the map, in Normal mode, the circles drawn. Not while recording, which
     * has the map.
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

    /** Which of the panel's tabs shows — the bottom bar marks Stories while it's that one. Null
     *  before the panel is built. */
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
            button.minWidth = minTouchTargetPx()
            button.minimumWidth = minTouchTargetPx()
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
        spotPopup = SpotPopup(findViewById(R.id.spot_popup), { topChrome.bottom }, ::onCaptureTap)
        photoMarkers = PhotoMarkers(findViewById(R.id.photo_markers), ::openPhotoMarker)
        photoPopup = PhotoPopup(findViewById(R.id.photo_popup), { topChrome.bottom }) { renderPhotoMarkers() }
        captureMode = CaptureMode(
            requireActivity() as AppCompatActivity, findViewById(R.id.capture_banner), { map }, { style?.takeIf { overlaysAttached } },
            frame = ::flyTo,
            onCaptured = spotPopup::renderCaptured,
        )
        // The host's, in the middle of its bottom bar, since the recording is reachable from
        // every tab; this fragment drives it, since the recording is the map's.
        recordButton = requireActivity().findViewById(R.id.record_button)
        recordButton.setOnClickListener { onRecordTap() }
        recordButton.onHoldComplete = ::stopRecording
        recordButton.onHoldProgress = ::renderHoldProgress
        recordingStatus = findViewById(R.id.recording_status)
        findViewById<View>(R.id.recording_status_stop).setOnClickListener { confirmStop() }
        recordingStatusDot = findViewById(R.id.recording_status_dot)
        recordingStatusTime = findViewById(R.id.recording_status_time)
        recordingStatusDetail = findViewById(R.id.recording_status_detail)
        recordingStatusHold = findViewById(R.id.recording_status_hold)
        // The notification's own format rather than the Chronometer's, so the two agree.
        recordingStatusTime.setOnChronometerTickListener {
            it.text = RecordingFormat.duration(SystemClock.elapsedRealtime() - it.base)
        }

        locateButton = findViewById(R.id.locate_button)
        locatePanel = findViewById(R.id.locate_panel)
        locateButton.setOnClickListener { onLocateTap() }

        dateFooter = findViewById(R.id.date_footer)
        zoomLevelNotice = ZoomLevelNotice(findViewById(R.id.zoom_level_notice))
        coverageNotice = findViewById(R.id.coverage_notice)
        sheet = findViewById(R.id.activities_sheet)
        selectionBar = findViewById(R.id.selection_bar)
        val syncTab = SyncTab(
            findViewById(R.id.panel_sync_content),
            requireActivity() as AppCompatActivity,
            scope = { viewLifecycleOwner.lifecycleScope },
            requestPermissions = healthPermissionLauncher::launch,
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
                refreshPhotos(force = true)
            },
            onRemovedFromStory = ::onRemovedFromStory,
            onStoryCopyArrived = ::onStoryCopyArrived,
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
                placeEditWindow()
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
            onPickPhotos = {
                photoPickLauncher.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
            },
            onPhotoOverlay = { overlay ->
                photoOverlay = overlay
                renderPhotoMarkers()
            },
            onTabChanged = ::renderPhotoMarkers,
            onClose = ::onEditClosed,
        )
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, collapseSheetOnBack)
        // Added after the sheet's, so it's asked first: Back ends selecting before it closes the sheet.
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, endSelectingOnBack)
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, closeEditOnBack)
        requireActivity().onBackPressedDispatcher.addCallback(viewLifecycleOwner, closePrivateOnBack)
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
        if (savedInstanceState?.containsKey(STATE_LOGO_MARGIN) == true) {
            logoBaseMarginBottom = savedInstanceState.getInt(STATE_LOGO_MARGIN)
            attributionBaseMarginBottom = savedInstanceState.getInt(STATE_ATTRIBUTION_MARGIN)
            attributionBaseCaptured = true
        }
        // Carried through a recreation (a theme or language change), since the mode buttons
        // restore their own checked state and would otherwise show a mode the map isn't in.
        savedInstanceState?.getString(STATE_MODE)?.let { saved ->
            MapMode.entries.firstOrNull { it.name == saved }?.let { mode = it }
        }
        setMode(mode)

        insetSystemBars()
        if (savedInstanceState == null) {
            handleStopIntent(requireActivity().intent)
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
            applyCompassMargin()
            applyAttributionMargin()
            mapView.post { enlargeAttributionTarget() }
            instance.addOnMapClickListener { point -> onMapTap(instance, point) }
            instance.addOnCameraMoveListener {
                spotPopup.place()
                photoMarkers.place()
                photoPopup.place()
            }
            // What overlaps depends on the zoom: the photo groups again once the camera rests.
            instance.addOnCameraIdleListener { photoMarkers.regroup() }
            showInArea = ShowInArea(findViewById(R.id.show_in_area), instance) { spots ->
                MapSpots.setInArea(style?.takeIf { overlaysAttached }, spots)
            }.apply { setCategories(shownSpots(MapSpots.get(requireContext()))) }
            instance.addOnCameraIdleListener { showInArea?.onCameraIdle() }
            instance.addOnCameraIdleListener { zoomLevelNotice.onCameraIdle(instance.cameraPosition.zoom) }
            loadStyle()
        }
    }

    /** Dragging a Private location's handle (`PrivateLocationEditor.onMapTouch`), or a track point in the
     *  editor's Move point mode (`TrackEditor.onMapTouch`), takes the touch before the map can
     *  pan with it. Only a press on the handle or a point is taken; every other touch, taps
     *  included, still reaches the map, which performs its own clicks. */
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

    /** [MIN_TOUCH_TARGET_DP] in pixels, rounded up. */
    private fun minTouchTargetPx() = kotlin.math.ceil(MIN_TOUCH_TARGET_DP * resources.displayMetrics.density).toInt()

    /**
     * MapLibre's attribution "i" is a 21dp view — under the 48dp touch target, and the one
     * control on the map that isn't the app's own. It has no id, so it's found by the content
     * description MapLibre gives it and padded out to 48dp on its right and top only: the view
     * grows into the empty map beside and above the icon, and the icon stays exactly where
     * MapLibre put it. The margins are left alone — `applyAttributionMargin` owns them for the
     * date footer, and moving the view back through them to pad evenly put the icon over the
     * MapLibre logo once the two met.
     */
    private fun enlargeAttributionTarget() {
        val found = ArrayList<View>()
        mapView.findViewsWithText(
            found,
            getString(org.maplibre.android.R.string.maplibre_attributionsIconContentDescription),
            View.FIND_VIEWS_WITH_CONTENT_DESCRIPTION,
        )
        val icon = found.firstOrNull() ?: return
        val target = minTouchTargetPx()
        val padX = (target - icon.width).coerceAtLeast(0)
        val padY = (target - icon.height).coerceAtLeast(0)
        if (padX == 0 && padY == 0) return
        icon.setPadding(0, padY, padX, 0)
    }

    /**
     * Loads the served style, and on load puts the live track and (once the session allows)
     * the user layers on it. Also what "Try again" re-runs after a failed load: a new style
     * starts with none of the app's own layers, so they are attached afresh.
     *
     * Attribution is not decoration here — the Protomaps basemap is an ODbL Produced Work, and
     * MapLibre's own attribution control renders the credit the style's source already
     * carries, so it must stay enabled.
     */
    private fun loadStyle() {
        val instance = map ?: return
        style = null
        overlaysAttached = false
        instance.setStyle(Style.Builder().fromUri(styleUrl())) { loaded ->
            style = loaded
            hideNotice(Notice.MAP_FAILED)
            // The served style ships the path layers hidden; a fresh style needs the saved choice.
            MapPaths.apply(loaded, shownPaths(MapPaths.get(requireContext())))
            // Only a deployment with imagery serves it; without, the Layers menu has no Satellite.
            satelliteAvailable = MapSatellite.isAvailable(loaded)
            MapSatellite.apply(loaded, MapSatellite.isOn(requireContext()))
            MapOverlays.attachLiveTrack(loaded)
            syncSession()
            renderRecording()
        }
    }

    /** A new intent to the host (`MainActivity.onNewIntent`): the notification's Stop, or the
     *  Sync screen's View on map. */
    fun onNewIntent(intent: Intent) {
        handleStopIntent(intent)
        takeViewOnMap(intent)
    }

    /** The Sync screen's View on map ([viewOnMap]): kept until the session is confirmed
     *  (`syncSession`), and the opening camera skipped, since this activity is the view. */
    private fun takeViewOnMap(intent: Intent) {
        // A relaunch from recents replays the intent; it has been handled.
        if (intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0) return
        val id = intent.getStringExtra(MainActivity.EXTRA_VIEW_ACTIVITY) ?: return
        val day = intent.getStringExtra(MainActivity.EXTRA_VIEW_DAY) ?: return
        viewOnMapRequest = id to day
        framed = true
    }

    /** The notification's Stop comes through here rather than straight to `RecordingService`
     *  (`RecordingService.buildNotification`): the Save screen that follows a stop needs an app
     *  window in the foreground to open over, and the stop is confirmed first ([confirmStop]).
     *  Ignored when relaunched from recents, which replays the last intent — that would stop a
     *  recording started since. */
    private fun handleStopIntent(intent: Intent) {
        if (intent.action != RecordingService.ACTION_STOP) return
        if (intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0) return
        confirmStop()
    }

    /** The notification's Stop asks before it stops: one tap in the shade is easy to make by
     *  mistake, where the map's own Stop takes a two-second hold ([RecordButton]) that is its
     *  own confirmation. Before the bind answers there's nothing to show yet — no state, no
     *  stats — so it's asked from `onServiceConnected` instead. */
    private fun confirmStop() {
        val service = recorder
        if (service == null) {
            pendingStopConfirm = true
            return
        }
        if (service.state == RecordingState.IDLE || stopDialog?.isShowing == true || requireActivity().isFinishing) return
        val stats = service.currentStats()
        stopDialog = MaterialAlertDialogBuilder(requireContext(), R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(R.string.recording_stop_confirm_title)
            .setMessage(
                getString(
                    R.string.recording_stop_confirm_message,
                    RecordingFormat.duration(stats.elapsedMs),
                    RecordingFormat.distance(resources, stats.distanceM),
                ),
            )
            .setPositiveButton(R.string.recording_stop) { _, _ -> stopRecording() }
            .setNegativeButton(R.string.recording_keep, null)
            .setOnDismissListener { stopDialog = null }
            .show()
    }

    /**
     * Keeps the floating chrome clear of the status bar, and of the gesture navigation pill —
     * which `MainActivity`'s bottom bar takes as its own, so the bottom inset given here is 0.
     *
     * Not optional at this target SDK: from API 35 the system draws every app edge to edge and
     * ignores the old opt-out. The top row (modes, Find my location) sits in a `layout_margin`ed
     * `LinearLayout`, which has no `fitsSystemWindows` of its own, so without this it renders
     * underneath the status bar's own icons — found exactly that way, the compass included,
     * both behind the clock and battery indicator on a real device. The map itself is left
     * alone on purpose: it should fill the whole screen, and MapLibre keeps its own attribution
     * and logo out of the corners anyway.
     *
     * The insets are added to the padding/margin each view was laid out with rather than
     * replacing it, and the listener returns them unconsumed so nothing else that wants them
     * misses out.
     */
    private fun insetSystemBars() {
        val topBar: View = findViewById(R.id.top_bar)
        val barTopMargin = (topBar.layoutParams as MarginLayoutParams).topMargin
        val mapRoot = findViewById<View>(R.id.map_root)
        mapRoot.setOnApplyWindowInsetsListener { _, insets ->
            val bars = insets.getInsets(WindowInsets.Type.systemBars())
            (topBar.layoutParams as MarginLayoutParams).topMargin = barTopMargin + bars.top
            topBar.requestLayout()
            systemBarInsetTop = bars.top
            applyCompassMargin()
            insets
        }
        // The attribution sits above an editor along the bottom, whose height is only known once
        // it's laid out.
        for (id in listOf(R.id.edit_window, R.id.private_editor)) {
            findViewById<View>(id).addOnLayoutChangeListener { _, _, _, _, _, _, _, _, _ -> applyAttributionMargin() }
        }
        // The compass sits below the top row and any notice under it, whose heights are only
        // known once they're laid out; the expanded sheet stops under the row likewise. The
        // attribution follows the sheet's head (onSheetChanged).
        topChrome.addOnLayoutChangeListener { _, _, _, _, _, _, _, _, _ ->
            applyCompassMargin()
            placeEditWindow()
            panel.setExpandedOffset(topBar.bottom + resources.getDimensionPixelSize(R.dimen.hmt_space_8), mapRoot.height)
        }
        mapRoot.addOnLayoutChangeListener { _, _, top, _, bottom, _, oldTop, _, oldBottom ->
            if (bottom - top != oldBottom - oldTop) {
                panel.setExpandedOffset(topBar.bottom + resources.getDimensionPixelSize(R.dimen.hmt_space_8), bottom - top)
            }
        }
    }

    /**
     * MapLibre's own compass control defaults to top-end with a small fixed margin, unaware of
     * the status bar — found sitting directly behind the clock/battery indicator on a real
     * device — and of the top row, whose rail of Layers and Find my location shares that
     * corner. Called from both `insetSystemBars` and `getMapAsync` because whichever of the
     * inset callback and the map-ready callback fires second is the one that actually has
     * everything it needs.
     */
    private fun applyCompassMargin() {
        val instance = map ?: return
        val settings = instance.uiSettings
        if (!compassBaseMarginCaptured) {
            compassBaseMarginTop = settings.compassMarginTop
            compassBaseMarginCaptured = true
        }
        // Below the top row (the modes and the rail), which already sits below the status bar,
        // and the notice while one is up; before the row is laid out, below the status bar at
        // least.
        val belowTopBar = maxOf(
            findViewById<View>(R.id.top_bar).bottom,
            if (notice.isVisible) notice.bottom else 0,
        )
        settings.setCompassMargins(
            settings.compassMarginLeft,
            compassBaseMarginTop + maxOf(systemBarInsetTop, belowTopBar),
            settings.compassMarginRight,
            settings.compassMarginBottom,
        )
    }

    /**
     * MapLibre's logo and attribution sit bottom-start, where the panel and the date-range
     * footer are — and the attribution is the basemap's ODbL credit, which must stay visible
     * (`FR-2.4`). So while those are up both move above them, above the *collapsed* panel
     * whatever the panel's state, as the web keeps them (an expanded panel covers them, as it
     * covers the map); the rest of the time they keep their own margins, which already clear
     * the gesture bar.
     */
    private fun applyAttributionMargin() {
        val instance = map ?: return
        val settings = instance.uiSettings
        if (!attributionBaseCaptured) {
            logoBaseMarginBottom = settings.logoMarginBottom
            attributionBaseMarginBottom = settings.attributionMarginBottom
            attributionBaseCaptured = true
        }
        // Above an editor open along the bottom, which has the screen meanwhile.
        val root = findViewById<View>(R.id.map_root)
        val editor = listOf(R.id.edit_window, R.id.private_editor).map { findViewById<View>(it) }.firstOrNull { it.isVisible }
        val above = when {
            editor != null -> root.height - editor.top
            sheet.isVisible -> panel.peekHeight
            else -> 0
        }
        settings.setLogoMargins(
            settings.logoMarginLeft,
            settings.logoMarginTop,
            settings.logoMarginRight,
            logoBaseMarginBottom + above,
        )
        settings.setAttributionMargins(
            settings.attributionMarginLeft,
            settings.attributionMarginTop,
            settings.attributionMarginRight,
            attributionBaseMarginBottom + above,
        )
    }

    /**
     * Brings the map in line with the credential the app holds — called both when the style
     * finishes loading and on every resume. A session that has gone away by then (the stored
     * token was revoked) sends the user back to `SignInActivity` rather than leaving a map with
     * nothing of theirs on it.
     *
     * A token restored from disk is not trusted until the server confirms it. An expired or
     * revoked one would otherwise surface as a wall of 401s on tile requests, which show up
     * only in logcat: on screen it would look like an ordinary blank map. The same check says
     * whether the account's email is confirmed; one that isn't goes to `VerifyEmailActivity`,
     * since every tile and sync request would be a `403` it has no way to explain.
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
        // The first run, as on the web: a real account that has never saved Settings (no
        // Country) goes there before the map — its Country decides the units the map's own
        // screens show, and nothing else would ever ask for it.
        if (!Session.isDemo && Session.country.isEmpty()) {
            SettingsActivity.openOnboarding(requireContext())
            requireActivity().finish()
            return
        }

        modeBarReady = true
        renderModeBar()
        if (daysStale) {
            daysStale = false
            activityDays.reload()
            // Sync or a recording may have changed the range's activities too, not just which
            // days have any — and an open Story's numbers.
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
            MapOverlays.attach(loaded, mode, selectedRange, darkBase(loaded), storyId)
            MapOverlays.setTrackFilter(loaded, panelState.mapHidden, panelState.highlighted)
            // Last, so the places are over everything else, labels included.
            MapSpots.attach(loaded, requireContext(), shownSpots(MapSpots.get(requireContext())))
            overlaysAttached = true
            loadSpotCaptures()
            captureMode.onStyleAttached()
            renderTrackMetrics()
            // A new style has none of the circles; draw them again if the editor has the map.
            privateEditor.onStyleReady()
            // Nor the Sync tab's lines.
            drawSyncCandidates(syncCandidates, syncHighlight)
            if (isRecording()) MapOverlays.setRecording(loaded, true, mode)
            frameActivities()
            checkTileVersion()
        }
    }

    /** The account's captured spots (`docs/SPEC.md` FR-15.6), for the badges and the popup —
     *  when the overlays attach and on every return to the app. Kept as they were on a failure. */
    private fun loadSpotCaptures() {
        HoldMyTrackApi.spotCaptures { result ->
            val captures = result.getOrNull() ?: return@spotCaptures
            MapSpots.setCaptured(style?.takeIf { overlaysAttached }, captures.associate { it.spotId to it.capturedAt })
            spotPopup.renderCaptured()
        }
    }

    /**
     * The overlays were just attached at the tile version kept from last time
     * ([Session.tileVersion]), so what MapLibre already has draws straight away. This reads the
     * current one and, only if it moved while the app was away (an upload on the web, a sync),
     * fetches every tile again at it.
     */
    private fun checkTileVersion() {
        HoldMyTrackApi.coverageStatus { result ->
            val status = result.getOrNull() ?: return@coverageStatus
            // Still being worked on (an import on the web, a sync): watched until it's done, which
            // shows the notice and refetches at the end.
            if (status.rendering) coverageWatch.watch()
            if (status.tileVersion.isEmpty() || status.tileVersion == Session.tileVersion) return@coverageStatus
            Session.tileVersion = status.tileVersion
            style?.takeIf { overlaysAttached }?.let {
                MapOverlays.refreshCoverage(it)
                MapOverlays.refreshTracks(it, selectedRange)
                // Loading places bumps the version too.
                MapSpots.refresh(it)
            }
        }
    }

    /**
     * The date-range slider's window has moved or reloaded. A reload — the first page, or a
     * fresh one on returning to the map — re-derives the default range until the user has
     * picked one: the five most recent activity days, through today (`docs/SPEC.md` FR-6.1).
     */
    private fun onActivityDaysChanged(reloaded: Boolean) {
        dateSlider.setDays(activityDays.visibleDays, activityDays.canPanEarlier, activityDays.canPanLater)
        if (reloaded && !userChangedRange) {
            val today = LocalDate.now().toString()
            val from = activityDays.visibleDays.takeLast(DEFAULT_RANGE_DAYS).firstOrNull()?.date
                ?: activityDays.earliest
                ?: today
            val range = DateRange(from, today)
            if (range != selectedRange) applyRange(range, fly = false)
        }
        renderDateFooter()
    }

    /**
     * Draws the tracks for [range] and lists its activities in the panel. Only a range the
     * user picked moves the camera — onto that range's activities, as the web does
     * (`docs/SPEC.md` FR-6.6) — never the default re-deriving itself after a sync; and only a
     * picked range clears the panel's selection, group, hidden set and filters, which were
     * built against the old one.
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

    /** The panel's list read again for what it shows now — the open Story, else the range. */
    private fun reloadList() = loadActivities(selectedRange, fly = false)

    /** `GET /v1/activities` for [range] — or, while a Story is open, for all of that Story,
     *  whatever the range — into the panel, and, with [fly], the camera onto every drawn one. */
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
                // A Pending row still has to be seen finishing: a dropped read doesn't end
                // the polling, it only waits for the next one.
                mapView.removeCallbacks(pendingPoll)
                if (pendingIds.isNotEmpty()) mapView.postDelayed(pendingPoll, PENDING_POLL_MS)
            }
        }
    }

    /**
     * Pending rows (a reprocess still running — a track edit, a Private location change, from
     * here or from another client): while any is, the list is read again every 2 seconds, and
     * a row that starts or stops being Pending has the track tiles fetched again, and Fog and
     * Heatmap watched until their re-render is done — the web's Pending effects in
     * `MapView.tsx`. A finished one also changes the days that have activity.
     */
    private fun trackPending(activities: List<Activity>) {
        val now = activities.filter { it.pending }.mapTo(HashSet()) { it.id }
        val started = now.any { it !in pendingIds }
        var finished = pendingIds.any { it !in now }
        // An edit saved from here counts as finished the first time a list shows it not
        // Pending, whether or not one ever showed it Pending.
        awaitingEditIds.filter { it !in now }.forEach { id ->
            awaitingEditIds -= id
            finished = true
            if (id == panelState.focused) updateTrackMetrics(refetch = true)
        }
        // The selected activity's reprocess landed: its metrics describe the old points.
        if (panelState.focused?.let { it in pendingIds && it !in now } == true) updateTrackMetrics(refetch = true)
        pendingIds = now
        if (started || finished) {
            style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
            coverageWatch.watch()
        }
        if (finished) {
            activityDays.reload()
            panel.storiesTab.reloadOpen()
        }
        mapView.removeCallbacks(pendingPoll)
        if (now.isNotEmpty()) mapView.postDelayed(pendingPoll, PENDING_POLL_MS)
    }

    /**
     * A Sync history row's View on map — the web's `viewActivityOnMap`: Normal mode and the
     * collapsed Activities tab, then the activity selected and flown to, as a row tap does. When
     * its [day] (its local date where it was recorded, the day the server files it under) is
     * outside the range, the range becomes that one day, as if picked, and the selection waits
     * for that range's list.
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
     * Opens Story [id] on the map — the web's `enterStory`: the panel's state starts afresh, as
     * for a new range, since the rows are a different set; the tracks and the list become all
     * of the Story's activities, whatever the range, and the camera fits them. Also how Add to
     * story's new Story lands on the tab.
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

    /**
     * Closes the open Story — the web's `exitStory`: back to the range, which opening it never
     * changed, with the panel's state afresh. The camera stays.
     */
    private fun exitStory() {
        storyId = null
        panel.storiesTab.setOpen(null)
        panelState.resetForNewRange()
        applyTrackFilter()
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackStory(it, null, selectedRange) }
        if (selectedRange != null) reloadList() else panel.setActivities(emptyList())
    }

    /** An activity taken out of the open Story: its track leaves the map and its row the list;
     *  a selection on it goes. The tile version moved with the change, and the tiles follow. */
    private fun onRemovedFromStory(activityId: String) {
        if (panelState.focused == activityId) panel.clearFocus()
        refreshPhotos(force = true)
        checkTileVersion()
        style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
        reloadList()
    }

    /** The toolbar's Edit: the window over its target, the panel held down and made inert
     *  under it, and the mode toggle away — the window edits Normal mode's rows. */
    private fun openEditWindow(group: List<Activity>) {
        panel.dismissPopups()
        panel.hold(true)
        editWindow.open(group, ActivityFacets.typeFacets(panelState.activities, null))
        // The photos follow the window: its one activity's, which its Photos tab manages.
        refreshPhotos(force = false)
        closeEditOnBack.isEnabled = true
        placeEditWindow()
        renderDateFooter()
        host?.setBottomBarShown(false)
        renderSelectionBar()
        renderModeBar()
        renderRail()
        renderTrackMetrics()
    }

    /** The Track tab opened: the other tracks and the bands away, and the camera on this one,
     *  the way a row tap flies — the web's `startEditTrack`. */
    private fun startEditTrack(activity: Activity) {
        editingTrackId = activity.id
        style?.takeIf { overlaysAttached }?.let {
            MapOverlays.setEditingTrack(it, true, mode)
            MapSpots.setEditing(it, true)
        }
        spotPopup.close()
        captureMode.stop()
        // Once the Track tab has laid out, so the fit leaves room under the window as it now is.
        findViewById<View>(R.id.edit_window).post { flyToActivities(listOf(activity)) }
    }

    /** The track editor's overlay: the points as edited, its knobs and what would go. */
    private fun drawTrackEdit(visible: List<TrackPoint>?, lo: Int, hi: Int, preview: EditPreview, split: Int?) {
        val loaded = style ?: return
        if (visible == null) TrackEditOverlay.clear(loaded) else TrackEditOverlay.set(loaded, visible, lo, hi, preview, split)
    }

    private fun onEditClosed(saved: Boolean, trackApplied: Boolean, photosSaved: Boolean) {
        photoOverlay = null
        // Back to the selection's or the Story's photos — read again when any was written.
        refreshPhotos(force = photosSaved)
        editingTrackId?.let { id ->
            if (trackApplied) awaitingEditIds += id
            style?.let(TrackEditOverlay::clear)
            style?.takeIf { overlaysAttached }?.let {
                MapOverlays.setEditingTrack(it, false, mode)
                MapSpots.setEditing(it, false)
            }
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

    /** The Edit window and the Private location editor sit along the bottom of the map, which
     *  reaches the screen's edge while either is open — the bottom bar is gone — so their margin
     *  clears the gesture bar. */
    private fun placeEditWindow() {
        val insets = ViewCompat.getRootWindowInsets(requireView())
        val gesture = insets?.getInsets(WindowInsetsCompat.Type.navigationBars())?.bottom ?: 0
        val bottom = resources.getDimensionPixelSize(R.dimen.hmt_space_10) + gesture
        for (id in listOf(R.id.edit_window, R.id.private_editor)) {
            val card = findViewById<View>(id)
            val params = card.layoutParams as MarginLayoutParams
            if (params.bottomMargin != bottom) {
                params.bottomMargin = bottom
                card.layoutParams = params
            }
        }
    }

    /** Whether the Private location editor is open over the map. */
    private fun privacyEditing() = ::privateEditor.isInitialized && privateEditor.isEditing

    /** The Private location editor has the map in Normal mode only: another mode, or a
     *  recording started, gives it back — circles and any unsaved draft gone. */
    private fun renderPrivacy() {
        if (!::privateEditor.isInitialized || !privateEditor.isShowing) return
        if (modeBarReady && !normalMode()) privateEditor.stop()
    }

    /**
     * A Private location saved or deleted — the web's `handlePrivateLocationsChanged`: the
     * activities it touches go Pending and are reprocessed, so the list is read again at once,
     * and once the server has drained its work, everything is: the tracks, the list, the days,
     * the selected activity's metrics — a batch that finished before the first read never
     * showed a row Pending at all.
     */
    private fun onPrivateLocationsChanged() {
        reloadList()
        coverageWatch.watch {
            style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
            reloadList()
            activityDays.reload()
            updateTrackMetrics(refetch = true)
            refreshPhotos(force = true)
            panel.storiesTab.reloadOpen()
        }
    }

    /** The toolbar's Delete finished: the tracks, the list and the days read again, and Fog
     *  and Heatmap watched until their re-render lands — the web's `handleActivitiesDeleted`.
     *  Deleted ids drop out of the selection with the reload. */
    /** A copy of a Story someone sent has arrived (`docs/SPEC.md` FR-14.8): new activities on
     *  the map and in the list, and the Fog and Heatmap the copy queued, as after an upload. The
     *  map has history now, so its "nothing on your map yet" notice goes. */
    private fun onStoryCopyArrived() {
        hideNotice(Notice.EMPTY)
        style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
        reloadList()
        activityDays.reload()
        coverageWatch.watch()
    }

    /** The Sync tab's lines, drawn over the map — or none, as it leaves. */
    private fun drawSyncCandidates(candidates: List<Candidate>, highlight: String?) {
        syncCandidates = candidates
        syncHighlight = highlight
        val loaded = style?.takeIf { overlaysAttached } ?: return
        if (candidates.isEmpty()) SyncCandidatesOverlay.clear(loaded) else SyncCandidatesOverlay.set(loaded, candidates, highlight)
    }

    /** The Sync tab sent activities: the range's list, its days and the tiles catch up, as on a
     *  return to the map. */
    private fun onSyncedFromPhone() {
        activityDays.reload()
        reloadList()
        checkTileVersion()
        if (shownNotice == Notice.EMPTY) {
            framed = false
            frameActivities()
        }
    }

    private fun onActivitiesDeleted(ids: List<String>) {
        if (ids.isEmpty()) return
        style?.takeIf { overlaysAttached }?.let { MapOverlays.refreshTracks(it, selectedRange) }
        reloadList()
        activityDays.reload()
        coverageWatch.watch()
        panel.storiesTab.reloadOpen()
        refreshPhotos(force = true)
    }

    /** The mode toggle: shown once the session allows, away while recording or editing. Layers
     *  shows once the session allows too, and stays while recording: the base map and the
     *  paths are about where the recording is going, not about the modes. */
    private fun renderModeBar() {
        if (!modeBarReady) return
        modeBar.visibility = if (isRecording() || editWindow.isOpen || selectionShown()) View.GONE else View.VISIBLE
        layersPanel.visibility = View.VISIBLE
        renderZoomLevelNotice()
        renderCoverageNotice()
        renderModeChip()
    }

    /** What Fog or Heatmap shows, since neither takes the date range: once the session allows,
     *  and not while recording, which draws neither. */
    private fun renderModeChip() {
        val text = when (mode) {
            MapMode.NORMAL -> null
            MapMode.FOG -> R.string.mode_chip_fog
            MapMode.HEATMAP -> R.string.mode_chip_heatmap
        }
        modeChip.isVisible = text != null && modeBarReady && !isRecording()
        if (text != null) modeChip.setText(text)
    }

    /** Names Fog's and Heatmap's level in view — once the session allows, and not while
     *  recording, which draws neither. */
    private fun renderZoomLevelNotice() {
        zoomLevelNotice.render(mode, active = modeBarReady && !isRecording())
    }

    /** "Your map is still being updated…" (`docs/SPEC.md` FR-4.2 behavior 6): in Fog and Heatmap,
     *  while the coverage watch's last read found the account's coverage still being worked on. */
    private fun renderCoverageNotice() {
        if (!::coverageNotice.isInitialized) return
        val shown = coverageRendering && mode != MapMode.NORMAL && modeBarReady && !isRecording()
        coverageNotice.visibility = if (shown) View.VISIBLE else View.GONE
    }

    /** Layers and Find my location, on the rail at the top row's end: away while the Edit window
     *  or the Private location editor is open, since both open over the top of the map. */
    private fun renderRail() {
        val editing = editWindow.isOpen || privacyEditing()
        mapRail.visibility = if (editing || selectionShown()) View.GONE else View.VISIBLE
        if (editing) layersMenu.dismiss()
    }

    /** The panel's hidden set, filters, Pending rows and selection, onto the track layers —
     *  and the selected activity's bands with them; and the sheet's head and the selection bar,
     *  which follow the selection and the checking. */
    private fun applyTrackFilter() {
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackFilter(it, panelState.mapHidden, panelState.highlighted) }
        updateTrackMetrics(refetch = false)
        refreshPhotos(force = false)
        if (::panel.isInitialized) renderDateFooter()
    }

    /** Whether the sheet's rows are being checked where the selection bar can show: Normal mode,
     *  the Activities tab, no Edit window over the map. */
    private fun selectionShown(): Boolean =
        ::panel.isInitialized && sheet.isVisible && panel.selecting && panel.tab == PanelTab.ACTIVITIES && !editWindow.isOpen

    /** The selection bar in the top row's place while [selectionShown], the mode toggle and the
     *  rail stepping aside for it. */
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
     * The selected activity's metrics — fetched when the selection moves to another activity,
     * or again with [refetch] once its reprocess has landed (the web's `trackMetricsVersion`),
     * and dropped the moment it clears.
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
        if (refetch) refreshPhotos(force = true)
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
     * The selected activity's pace bands on the map while it's also drawn there — not hidden,
     * filtered out or Pending, whose metrics describe the points from before its reprocess
     * (the web's `focusedPending`).
     */
    private fun renderTrackMetrics() {
        val metrics = trackMetrics
        val loaded = style?.takeIf { overlaysAttached } ?: return
        val focused = panelState.focused
        if (metrics != null && focused == metrics.activityId && focused !in panelState.mapHidden) {
            MapOverlays.setTrackBands(loaded, metrics.points)
            panel.setBandsShown(true)
        } else {
            MapOverlays.clearTrackBands(loaded)
            panel.setBandsShown(false)
        }
    }

    /**
     * The photos for whatever the map now shows ([photoScope]) — read when the scope moves to
     * another activity or Story, or with [force] when they may have changed under it (a save
     * from the Photos tab, a reprocess, a Story's members). A new scope starts empty rather than
     * showing the last one's photos while it loads.
     */
    private fun refreshPhotos(force: Boolean) {
        val single = editWindow.single
        val next = when {
            mode != MapMode.NORMAL -> null
            editWindow.isOpen -> single?.let { "activity:${it.id}" }
            panelState.focused != null -> "activity:${panelState.focused}"
            storyId != null -> "story:$storyId"
            else -> null
        }
        if (next == photoScope && !force) {
            if (photosLoaded && single != null) editWindow.setPhotos(photos, photosError)
            renderPhotoMarkers()
            return
        }
        if (next != photoScope) {
            photos = emptyList()
            photosError = null
            photosLoaded = false
            photoPopup.close()
        }
        photoScope = next
        val gen = ++photoGeneration
        renderPhotoMarkers()
        if (next == null) return
        val (kind, id) = next.split(':', limit = 2)
        HoldMyTrackApi.photos(activity = id.takeIf { kind == "activity" }, story = id.takeIf { kind == "story" }) { result ->
            if (gen != photoGeneration) return@photos
            photos = result.getOrDefault(emptyList())
            photosLoaded = true
            photosError = result.exceptionOrNull()?.let { it.message?.takeIf { m -> m.isNotBlank() } ?: getString(R.string.map_unreachable) }
            if (photosError != null) Log.w(TAG, "could not load photos", result.exceptionOrNull())
            if (single != null && editWindow.single?.id == single.id) editWindow.setPhotos(photos, photosError)
            photoPopup.retain(photos)
            renderPhotoMarkers()
        }
    }

    /**
     * The photo markers (`docs/SPEC.md` FR-16.7): the scope's photos on their tracks, with the
     * Photos tab's unsaved changes merged in while it shows — not while the Track tab has the
     * map, nor for a route hidden on the map.
     */
    private fun renderPhotoMarkers() {
        if (!::photoMarkers.isInitialized) return
        val scope = photoScope
        val activityScope = scope?.removePrefix("activity:")?.takeIf { scope.startsWith("activity:") }
        val shown = scope != null && !isRecording() && !editWindow.showingTrack && activityScope !in panelState.mapHidden
        var items = if (!shown) emptyList() else photos.mapNotNull { p ->
            val lat = p.lat ?: return@mapNotNull null
            val lon = p.lon ?: return@mapNotNull null
            if (p.activityId in panelState.mapHidden) return@mapNotNull null
            PhotoMarkerItem(p.id, lon, lat, p.caption, thumbPath = p.thumbUrl)
        }
        val overlay = photoOverlay
        if (shown && overlay != null) {
            val upserts = overlay.upserts.associateBy { it.id }
            val kept = items.filter { it.id !in overlay.hidden }.map { upserts[it.id] ?: it }
            val keptIds = kept.mapTo(HashSet()) { it.id }
            items = kept + overlay.upserts.filter { it.id !in keptIds }
        }
        if (!shown) photoPopup.close()
        photoMarkers.set(map, items, overlay?.activeId ?: photoPopup.showingId, overlay?.activeId)
    }

    /**
     * A marker's tap (FR-16.7): one photo opens its popup. A group the map can separate — its
     * photos more than [PHOTO_GROUP_SPREAD_M] apart — zooms in to fit them, at most to z19, where
     * that spread is wider than a marker; one it can't (several taken at one spot) opens the
     * popup on its first photo, to step through the rest. Only saved photos open: one the Photos
     * tab hasn't uploaded has nothing to show yet.
     */
    private fun openPhotoMarker(ids: List<String>) {
        val instance = map ?: return
        val located = ids.mapNotNull { id -> photos.firstOrNull { it.id == id }?.takeIf { it.lat != null && it.lon != null } }
        val spread = located.maxOfOrNull { a -> located.maxOf { b -> Geo.haversineM(a.lat!!, a.lon!!, b.lat!!, b.lon!!) } } ?: 0.0
        if (located.size > 1 && spread > PHOTO_GROUP_SPREAD_M) {
            flyTo(
                doubleArrayOf(located.minOf { it.lon!! }, located.minOf { it.lat!! }, located.maxOf { it.lon!! }, located.maxOf { it.lat!! }),
                maxZoom = PHOTO_GROUP_MAX_ZOOM,
            )
            return
        }
        if (located.isEmpty()) return
        spotPopup.close()
        photoPopup.show(instance, located)
    }

    /** Frames every one of [activities] that has a track — nothing, for none. */
    private fun flyToActivities(activities: List<Activity>) {
        flyTo(BoxUnion.of(activities.mapNotNull { it.bbox }) ?: return)
    }

    /**
     * A tap on the map in Normal mode: a track under it — within 14dp, the web's touch
     * tolerance (`docs/SPEC.md` §19), since a 2.5dp line is too thin to hit exactly — is
     * selected in the panel and flown to; empty map clears the selection. The panel's own
     * collapsed or expanded state is left as it was.
     */
    private fun onMapTap(instance: MapLibreMap, point: LatLng): Boolean {
        // A spot's badge first, in every mode: it opens the spot's popup, and a tap anywhere
        // else closes it. Not while the Private location editor or the Edit window has the map.
        if (!privateEditor.isShowing && !editWindow.isOpen) {
            val spot = MapSpots.spotAt(instance, instance.projection.toScreenLocation(point), resources.displayMetrics.density)
            if (spot != null) {
                spotPopup.show(instance, spot)
                return true
            }
            spotPopup.close()
        }
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
        // The Edit window's group mustn't change underneath it; the one thing a tap does then
        // is Delete point's.
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
        applyAttributionMargin()
        collapseSheetOnBack.isEnabled = sheet.isVisible && panel.expanded && !editWindow.isOpen
    }

    /** The sheet is Normal mode's alone, as on the web (Fog and Heatmap ignore the range and
     *  select nothing — `docs/SPEC.md` FR-4.2), and steps aside while recording. It waits for
     *  the session; the range at its head also for the first page of days, and an account with
     *  no activity at all has nothing to pick from — the empty notice speaks for it instead. The
     *  range is the Activities tab's alone: Stories takes none (`docs/SPEC.md` FR-6), and has
     *  its title at the head instead. */
    private fun renderDateFooter() {
        val normal = normalMode()
        // The selected activity's card takes the range's place at the head.
        val footer = normal && panel.tab == PanelTab.ACTIVITIES && !panel.showsCard && activityDays.ready && activityDays.earliest != null
        dateFooter.visibility = if (footer) View.VISIBLE else View.GONE
        // An editor over the map has the screen, the sheet stepping aside for it. The Sync tab
        // stays while recording: what's on the phone can be sent with a new walk under way.
        val editing = editWindow.isOpen || privacyEditing()
        val syncing = modeBarReady && mode == MapMode.NORMAL && panel.tab == PanelTab.SYNC
        sheet.visibility = if ((normal || syncing) && !editing) View.VISIBLE else View.GONE
        if (!normal) panel.dismissPopups()
        onSheetChanged()
        renderSelectionBar()
        renderTrackMetrics()
        renderPrivacy()
    }

    private fun isRecording() = (recorder?.state ?: RecordingState.IDLE) != RecordingState.IDLE

    /** Normal mode with the session ready and no recording: when the sheet, its tabs and their
     *  editors are the map's. */
    private fun normalMode() = modeBarReady && mode == MapMode.NORMAL && !isRecording()

    private fun hasPermission(permission: String) =
        ContextCompat.checkSelfPermission(requireContext(), permission) == PackageManager.PERMISSION_GRANTED

    /** Idle: start (asking for permissions first if they're missing). Recording or paused:
     *  toggle between the two. Nothing here ever asks for a name or a type — see
     *  `RecordingService`'s class doc for where those come from. Pausing says how to stop
     *  instead: a tap is the obvious guess at "stop", and it only pauses. Every tap is felt —
     *  pause and resume as a toggle's off and on, start as [startRecording]'s confirm — so a
     *  tap that registered is never in doubt. */
    private fun onRecordTap() {
        // A start from Sync or You goes to the map, where the recording is drawn.
        if (!isRecording()) host?.showMap()
        if (isRecording()) {
            if (recorder?.state == RecordingState.RECORDING) {
                recordButton.performHapticFeedback(HapticFeedbackConstants.TOGGLE_OFF)
                Toast.makeText(requireContext(), R.string.record_hold_to_stop, Toast.LENGTH_SHORT).show()
            } else {
                recordButton.performHapticFeedback(HapticFeedbackConstants.TOGGLE_ON)
            }
            requireContext().startService(RecordingService.intent(requireContext(), RecordingService.ACTION_TOGGLE))
            return
        }
        val missing = listOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.POST_NOTIFICATIONS)
            .filterNot(::hasPermission)
        if (missing.isEmpty()) startRecording() else permissionLauncher.launch(missing.toTypedArray())
    }

    /** A two-second hold stops (`RecordButton`) — deliberately not a plain tap, so a stray
     *  touch can pause a recording but never end it. `RecordingService` then opens the Save
     *  screen over the map. */
    private fun stopRecording() {
        requireContext().startService(RecordingService.intent(requireContext(), RecordingService.ACTION_STOP))
    }

    /** Felt and seen: a confirm haptic and a short pop of the button, ahead of the red ring,
     *  pulse and status pill that [renderRecording] brings once the service reports in. */
    private fun startRecording() {
        recordButton.performHapticFeedback(HapticFeedbackConstants.CONFIRM)
        recordButton.animate().cancel()
        recordButton.animate().scaleX(POP_SCALE).scaleY(POP_SCALE).setDuration(POP_HALF_MS).withEndAction {
            recordButton.animate().scaleX(1f).scaleY(1f).setDuration(POP_HALF_MS)
        }
        ContextCompat.startForegroundService(requireContext(), RecordingService.intent(requireContext(), RecordingService.ACTION_START))
    }

    /** A recording the process died in the middle of (`RecordingService.findLeftover`): not
     *  cancellable, since dismissing it would only bring it back on the next bind. Resume comes
     *  back paused; Save goes to the Save screen as a Stop would; Discard confirms first, as the
     *  Save screen's does, since the journal is the only copy. */
    private fun offerLeftover(leftover: LiveRecordingJournal.Leftover) {
        if (leftoverDialog?.isShowing == true || requireActivity().isFinishing) return
        leftoverDialog = MaterialAlertDialogBuilder(requireContext())
            .setTitle(R.string.recording_leftover_title)
            .setMessage(
                getString(
                    R.string.recording_leftover_message,
                    RecordingFormat.duration(leftover.movingMs),
                    RecordingFormat.distance(resources, leftover.distanceM),
                ),
            )
            .setCancelable(false)
            .setPositiveButton(R.string.recording_leftover_resume) { _, _ ->
                if (recorder?.resumeLeftover(leftover) != true) {
                    Toast.makeText(requireContext(), R.string.recording_needs_location, Toast.LENGTH_LONG).show()
                }
            }
            .setNeutralButton(R.string.recording_leftover_save) { _, _ -> recorder?.saveLeftover(leftover) }
            .setNegativeButton(R.string.recording_discard) { _, _ -> confirmDiscardLeftover(leftover) }
            .show()
    }

    private fun confirmDiscardLeftover(leftover: LiveRecordingJournal.Leftover) {
        leftoverDialog = MaterialAlertDialogBuilder(requireContext(), R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(R.string.recording_discard_confirm_title)
            .setMessage(R.string.recording_discard_confirm_message)
            .setCancelable(false)
            .setPositiveButton(R.string.recording_discard) { _, _ -> recorder?.discardLeftover(leftover) }
            .setNegativeButton(android.R.string.cancel) { _, _ ->
                // Still showing while its own button's callback runs, so the guard would
                // otherwise keep the prompt from coming back.
                leftoverDialog = null
                offerLeftover(leftover)
            }
            .show()
    }

    /** A spot's Capture: capture mode on it (FR-2.8), once precise location is granted. */
    private fun onCaptureTap(spot: Spot) {
        spotPopup.close()
        if (hasPermission(Manifest.permission.ACCESS_FINE_LOCATION)) {
            captureMode.start(spot)
        } else {
            pendingCapture = spot
            capturePermissionLauncher.launch(
                arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION),
            )
        }
    }

    private fun hasLocationPermission() =
        hasPermission(Manifest.permission.ACCESS_FINE_LOCATION) || hasPermission(Manifest.permission.ACCESS_COARSE_LOCATION)

    private fun onLocateTap() {
        if (hasLocationPermission()) {
            showMyLocation()
        } else {
            locatePermissionLauncher.launch(
                arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION),
            )
        }
    }

    /**
     * Turns on MapLibre's own position dot and puts the camera in tracking mode, which flies to
     * the first fix (or the last known one) and then follows it until the user pans — the same
     * behavior as the web map's geolocate control. Activated lazily, on the first tap, so a
     * user who never asks is never located.
     */
    @SuppressLint("MissingPermission") // Only reached once hasLocationPermission() holds.
    private fun showMyLocation() {
        val instance = map ?: return
        val loaded = style ?: return
        val location = instance.locationComponent
        if (!location.isLocationComponentActivated) {
            location.activateLocationComponent(LocationComponentActivationOptions.Builder(requireContext(), loaded).build())
        }
        location.isLocationComponentEnabled = true
        location.setCameraMode(
            CameraMode.TRACKING,
            FRAME_DURATION_MS.toLong(),
            maxOf(instance.cameraPosition.zoom, LOCATE_ZOOM),
            null,
            null,
            null,
        )
    }

    /** Brings the button, the mode toggle, the map's layers, the live track and the camera in
     *  line with the recording's state — called on bind, on every state change and fix the
     *  service reports, and once the style has loaded. */
    private fun renderRecording() {
        val state = recorder?.state ?: RecordingState.IDLE
        val active = state != RecordingState.IDLE
        val (icon, background, description) = when (state) {
            RecordingState.IDLE -> Triple(R.drawable.ic_record_start, R.drawable.bg_record_button, R.string.record_button_start)
            RecordingState.RECORDING -> Triple(R.drawable.ic_pause, R.drawable.bg_record_button_recording, R.string.record_button_pause)
            RecordingState.PAUSED -> Triple(R.drawable.ic_play, R.drawable.bg_record_button_paused, R.string.record_button_resume)
        }
        recordButton.setImageResource(icon)
        // Pause and play are white Lucide strokes, tinted for the button's surface disc; the
        // idle red dot keeps its own colour.
        recordButton.imageTintList = when (state) {
            RecordingState.IDLE -> null
            RecordingState.RECORDING -> ColorStateList.valueOf(requireContext().getColor(R.color.hmt_record))
            RecordingState.PAUSED -> ColorStateList.valueOf(requireContext().getColor(R.color.hmt_record_paused))
        }
        recordButton.setBackgroundResource(background)
        recordButton.contentDescription = getString(description)
        recordButton.holdEnabled = active
        recordButton.pulsing = state == RecordingState.RECORDING
        sizeRecordButton(active)
        if (!active) stopDialog?.dismiss()
        val points = if (active) recorder?.points().orEmpty() else emptyList()
        renderRecordingStatus(state, hasFix = points.isNotEmpty())
        renderModeBar()
        locatePanel.visibility = if (active) View.GONE else View.VISIBLE
        renderModeChip()
        renderDateFooter()
        updateNoticeVisibility()
        // Like every history layer, the photos give the map to the live track.
        renderPhotoMarkers()

        val loaded = style ?: return
        if (overlaysAttached) MapOverlays.setRecording(loaded, active, mode)
        MapOverlays.updateLiveTrack(loaded, points)

        if (!active) {
            followingRecording = false
            return
        }
        val instance = map ?: return
        val last = points.lastOrNull() ?: return
        val target = LatLng(last.lat, last.lon)
        if (!followingRecording) {
            followingRecording = true
            // The recording's own follow (below) owns the camera now, not Find my location's.
            if (instance.locationComponent.isLocationComponentActivated) {
                instance.locationComponent.cameraMode = CameraMode.NONE
            }
            val zoom = maxOf(instance.cameraPosition.zoom, RECORDING_ZOOM)
            instance.animateCamera(CameraUpdateFactory.newLatLngZoom(target, zoom), FRAME_DURATION_MS)
        } else {
            instance.easeCamera(CameraUpdateFactory.newLatLng(target))
        }
    }

    /** 56dp idle, 60dp while a recording is in progress — a bigger target for the hold that
     *  stops it, with less of it under the finger. Centred over the bottom bar's middle slot
     *  either way (`activity_main.xml`). */
    private fun sizeRecordButton(active: Boolean) {
        val density = resources.displayMetrics.density
        val size = ((if (active) RECORD_BUTTON_ACTIVE_DP else RECORD_BUTTON_IDLE_DP) * density).toInt()
        val params = recordButton.layoutParams
        if (params.width == size) return
        params.width = size
        params.height = size
        recordButton.layoutParams = params
    }

    /** The pill beside the record button, while a recording is in progress: a red dot (amber
     *  paused), the moving time — ticking on its own while recording, frozen while paused — and
     *  the distance, or "Waiting for GPS…" until the first fix arrives. A hold on the button
     *  takes the detail line over ([renderHoldProgress]). */
    private fun renderRecordingStatus(state: RecordingState, hasFix: Boolean) {
        val service = recorder
        if (state == RecordingState.IDLE || service == null) {
            recordingStatusTime.stop()
            recordingStatus.visibility = View.GONE
            return
        }
        recordingStatus.visibility = View.VISIBLE
        val recording = state == RecordingState.RECORDING
        val stats = service.currentStats()
        recordingStatusTime.base = SystemClock.elapsedRealtime() - stats.elapsedMs
        if (recording) {
            recordingStatusTime.start()
        } else {
            // Stopped, nothing ticks after setBase's own redraw in the Chronometer's format.
            recordingStatusTime.stop()
            recordingStatusTime.text = RecordingFormat.duration(stats.elapsedMs)
        }
        recordingStatusDot.backgroundTintList =
            ColorStateList.valueOf(requireContext().getColor(if (recording) R.color.hmt_record else R.color.hmt_record_paused))
        if (holdingToStop) return
        val distance = RecordingFormat.distance(resources, stats.distanceM)
        recordingStatusDetail.text = when {
            !recording -> getString(R.string.recording_status_paused, distance)
            !hasFix -> getString(R.string.recording_status_waiting)
            else -> distance
        }
    }

    /** The hold's countdown in the pill too, where the finger on the button can't hide it. */
    private fun renderHoldProgress(progress: Float) {
        val holding = progress > 0f
        recordingStatusHold.progress = (progress * recordingStatusHold.max).toInt()
        if (holding == holdingToStop) return
        holdingToStop = holding
        recordingStatusHold.visibility = if (holding) View.VISIBLE else View.GONE
        if (holding) {
            recordingStatusDetail.setText(R.string.recording_status_holding)
        } else {
            renderRecordingStatus(recorder?.state ?: RecordingState.IDLE, hasFix = recorder?.points()?.isNotEmpty() == true)
        }
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
            hideNotice(Notice.CHECKING)
            result.onSuccess { profile ->
                Session.markVerified(profile)
                AppLanguage.followAccount(requireContext().applicationContext, profile.locale)
                hideNotice(Notice.UNREACHABLE)
            }.onFailure { failure ->
                // Only a 401 means the token itself is dead. Anything else — no network, a
                // stopped dev stack — says nothing about the credential, so it survives and
                // gets re-checked on the next resume rather than silently signing the user out;
                // meanwhile the map says why none of their layers is on it.
                if (failure is ApiException && failure.code == 401) {
                    Session.clear()
                } else {
                    Log.w(TAG, "could not verify the stored session", failure)
                    showNotice(
                        Notice.UNREACHABLE,
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
     * What the map can have to say, most important first: when two apply, the earlier one
     * stays up. They share the one panel under the chrome row rather than stacking.
     */
    private enum class Notice { MAP_FAILED, UNREACHABLE, CHECKING, EMPTY }

    /**
     * The map's notice panel, after the web's on-map banner: [text], an optional smaller
     * [detail], and at most one action. A less important notice never replaces a more
     * important one that is up (see [Notice]).
     */
    private fun showNotice(
        kind: Notice,
        text: String,
        detail: String? = null,
        failed: Boolean = false,
        action: Int? = null,
        onAction: (() -> Unit)? = null,
    ) {
        val current = shownNotice
        if (current != null && current.ordinal < kind.ordinal) return
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
    private fun hideNotice(kind: Notice) {
        if (shownNotice != kind) return
        shownNotice = null
        updateNoticeVisibility()
    }

    /** "Nothing on your map yet" has nothing to say while a recording has the map, or on the
     *  Sync tab it points to, so it steps aside then and comes back after; the failures stay up
     *  regardless. */
    private fun updateNoticeVisibility() {
        val kind = shownNotice
        val onSync = ::panel.isInitialized && panel.tab == PanelTab.SYNC
        notice.visibility = when {
            kind == null -> View.GONE
            kind == Notice.EMPTY && (isRecording() || onSync) -> View.GONE
            else -> View.VISIBLE
        }
    }

    /**
     * At a large font size the three labels don't fit beside the rail, so only the active mode
     * keeps its label and the other two are their icons (`ui/LargeText`); each is still named
     * for TalkBack. The label is the button's own text from the layout, kept in its tag.
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
        renderZoomLevelNotice()
        renderCoverageNotice()
        renderModeChip()
        renderDateFooter()
        refreshPhotos(force = false)
    }

    /** The Layers menu's Points of interest. Unticking the open popup's category closes it. */
    private fun setSpots(categories: List<MapSpots.Category>) {
        MapSpots.set(requireContext(), categories)
        showSpots(shownSpots(categories))
        captureMode.spot?.let { target -> if (categories.none { it.wire == target.category }) captureMode.stop() }
    }

    private fun setPaths(paths: MapPaths.Paths) {
        MapPaths.set(requireContext(), paths)
        style?.let { MapPaths.apply(it, shownPaths(paths)) }
    }

    /** The Layers menu's Show layers: shows or hides every pick at once, keeping them. A capture in progress
     *  carries on, since hiding the places is about the view, not about going to one. */
    private fun setLayersShown(on: Boolean) {
        MapLayersSwitch.set(requireContext(), on)
        style?.let { MapPaths.apply(it, shownPaths(MapPaths.get(requireContext()))) }
        showSpots(shownSpots(MapSpots.get(requireContext())))
    }

    /** What the map draws of the picks: all of them while Show layers is on, none while off. */
    private fun shownPaths(paths: MapPaths.Paths) = if (MapLayersSwitch.isOn(requireContext())) paths else MapPaths.NONE

    private fun shownSpots(categories: List<MapSpots.Category>) =
        if (MapLayersSwitch.isOn(requireContext())) categories else emptyList()

    /** Draws these places, and closes the popup of one that's no longer drawn. */
    private fun showSpots(shown: List<MapSpots.Category>) {
        style?.takeIf { overlaysAttached }?.let { MapSpots.setCategories(it, shown) }
        spotPopup.spot?.let { open -> if (shown.none { it.wire == open.category }) spotPopup.close() }
        showInArea?.setCategories(shown)
    }

    private fun setSatellite(on: Boolean) {
        MapSatellite.set(requireContext(), on)
        val loaded = style ?: return
        MapSatellite.apply(loaded, on)
        if (overlaysAttached) MapOverlays.setDarkVeil(loaded, darkBase(loaded))
    }

    /** Whether the basemap reads dark — the dark flavor, or satellite imagery over either —
     *  which picks Fog's veil and Heatmap's wash (`MapOverlays.setDarkVeil`), the web's `isDarkBase`. */
    private fun darkBase(style: Style): Boolean =
        isNight() || (MapSatellite.isOn(requireContext()) && MapSatellite.isAvailable(style))

    /**
     * Moves the camera onto the account's most recent activity, once per session — the web's
     * opening view (`docs/SPEC.md` FR-4.5), not the whole history, which for an account with
     * scattered recent history is a near-world view that reads as broken.
     *
     * An account with no geometry yet is left at the world view, which is the truthful thing to
     * show for a history that is empty — with a notice saying so and pointing at Sync,
     * re-checked on every resume until something arrives (`onResume`). Not for a demo account,
     * which always has history and can't sync.
     */
    private fun frameActivities() {
        if (framed) return
        framed = true
        HoldMyTrackApi.latestActivityBounds { result ->
            if (result.isFailure) return@latestActivityBounds
            val box = result.getOrNull()
            if (box == null) {
                if (!Session.isDemo) {
                    showNotice(Notice.EMPTY, getString(R.string.map_empty), action = R.string.nav_sync) {
                        host?.showTab(MainActivity.Tab.SYNC)
                    }
                }
                return@latestActivityBounds
            }
            hideNotice(Notice.EMPTY)
            // Reopening the app mid-recording: the camera belongs to the live track.
            if (isRecording()) return@latestActivityBounds
            flyTo(box)
        }
    }

    /**
     * Flies the camera to fit [box] (`[west, south, east, north]`, east past 180 for one across
     * the antimeridian, which `LatLngBounds.from` takes as it is). The zoom is capped
     * because a single short activity — or one clipped to almost nothing by a Private location —
     * has a near-zero extent, and fitting the camera to that box lands well past the basemap's
     * z14 data, on a grey rectangle.
     */
    private fun flyTo(box: DoubleArray, maxZoom: Double = MAX_FRAME_ZOOM) {
        val instance = map ?: return
        val bounds = LatLngBounds.from(box[3], box[2], box[1], box[0])
        // Into the map left showing between the chrome row and the panel, not under either.
        val card = findViewById<View>(R.id.edit_window)
        val banner = findViewById<View>(R.id.capture_banner)
        val top = maxOf(
            FRAME_PADDING_PX,
            findViewById<View>(R.id.top_bar).bottom,
            if (banner.isVisible) topChrome.top + banner.bottom + FRAME_PADDING_PX / 2 else 0,
        )
        // Above the height the sheet rests at or is heading to, not mid-drag: a View on map
        // collapses it and flies in the same moment, and fitting to where it was pushed the
        // activity to the top of the screen.
        val root = findViewById<View>(R.id.map_root)
        val editor = listOf(card, findViewById<View>(R.id.private_editor)).firstOrNull { it.isVisible }
        val bottom = FRAME_PADDING_PX + when {
            editor != null -> root.height - editor.top
            sheet.isVisible -> root.height - panel.restingTop(root.height)
            else -> 0
        }
        // A landscape phone's top row (the rail's two buttons stacked) and sheet can leave
        // nothing between them; the fit then gets at least a strip, taken from the top first.
        val minSpan = (MIN_FRAME_SPAN_DP * resources.displayMetrics.density).toInt()
        val fitTop = top.coerceAtMost(maxOf(FRAME_PADDING_PX, root.height - bottom - minSpan))
        val fitBottom = bottom.coerceAtMost(maxOf(FRAME_PADDING_PX, root.height - fitTop - minSpan))
        val padding = intArrayOf(FRAME_PADDING_PX, fitTop, FRAME_PADDING_PX, fitBottom)
        val fitted = instance.getCameraForLatLngBounds(bounds, padding) ?: return
        val target = CameraPosition.Builder(fitted)
            .zoom(minOf(fitted.zoom, maxZoom))
            .build()
        instance.animateCamera(CameraUpdateFactory.newCameraPosition(target), FRAME_DURATION_MS)
    }

    /**
     * Flavor follows the app's night mode, the same one its own colors follow: the system's
     * day/night setting, or the You tab's Theme toggle when that overrides it (`AppTheme`). The
     * API serves five (`light`, `dark`, `white`, `black`, `grayscale`); this picks between the
     * two general-purpose ones.
     */
    private fun styleUrl(): String {
        val flavor = if (isNight()) "dark" else "light"
        return "${BuildConfig.API_BASE_URL}/v1/map/style/$flavor"
    }

    private fun isNight(): Boolean =
        resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK == Configuration.UI_MODE_NIGHT_YES

    /** The style (or its sources) couldn't be fetched: a plain sentence, with the origin and
     *  MapLibre's own error in small print for whoever has to fix it, and a way to try again. */
    private fun reportFailure(error: String) {
        Log.e(TAG, "map failed to load: $error")
        showNotice(
            Notice.MAP_FAILED,
            getString(R.string.map_load_failed),
            detail = "${BuildConfig.API_BASE_URL} — $error",
            failed = true,
            action = R.string.retry,
        ) { loadStyle() }
    }

    // MapLibre's MapView holds a native renderer and a GL surface, so every lifecycle
    // callback has to be forwarded by hand — a missed one leaks the surface or crashes on
    // rotation. This is the whole set the SDK expects.
    override fun onStart() {
        super.onStart()
        mapView.onStart()
        recorderBound = requireContext().bindService(Intent(requireContext(), RecordingService::class.java), recorderConnection, Context.BIND_AUTO_CREATE)
    }

    override fun onResume() {
        super.onResume()
        mapView.onResume()
        daysStale = true
        syncSession()
        if (overlaysAttached) {
            loadSpotCaptures()
            // Back from elsewhere (the web, Sync): new tiles if the version moved, and the notice
            // if the account's coverage is still being worked on.
            checkTileVersion()
        }
        captureMode.resume()
        // An empty map is asked again on every return — typically from Sync — so the notice
        // goes, and the camera frames the new history, as soon as something has arrived.
        if (shownNotice == Notice.EMPTY) {
            framed = false
            frameActivities()
        }
        // Back from Health Connect's settings or a recording's Edit: the Sync tab reads again.
        if (::panel.isInitialized && !isHidden) panel.syncTab.resume()
    }

    /** The You tab over the map, or the map back: the Sync tab stops reading, or reads again. */
    override fun onHiddenChanged(hidden: Boolean) {
        super.onHiddenChanged(hidden)
        if (!::panel.isInitialized) return
        if (hidden) panel.syncTab.pause() else panel.syncTab.resume()
    }

    override fun onPause() {
        captureMode.pause()
        mapView.onPause()
        super.onPause()
    }

    override fun onStop() {
        // Health Connect's routes can't be read in the background (`docs/IMPLEMENTATION.md` §4.0).
        if (::panel.isInitialized) panel.syncTab.pause()
        if (::mapView.isInitialized) mapView.removeCallbacks(pendingPoll)
        if (recorderBound) {
            recorder?.onChange = null
            requireContext().unbindService(recorderConnection)
            recorderBound = false
            recorder = null
        }
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
        if (attributionBaseCaptured) {
            outState.putInt(STATE_LOGO_MARGIN, logoBaseMarginBottom)
            outState.putInt(STATE_ATTRIBUTION_MARGIN, attributionBaseMarginBottom)
        }
    }

    override fun onLowMemory() {
        super.onLowMemory()
        if (::mapView.isInitialized) mapView.onLowMemory()
    }

    override fun onDestroyView() {
        leftoverDialog?.dismiss()
        stopDialog?.dismiss()
        layersMenu.dismiss()
        captureMode.stop()
        mapView.onDestroy()
        dateSlider.release()
        coverageWatch.stop()
        super.onDestroyView()
    }

    companion object {
        private const val TAG = "HoldMyTrack"
        private const val RECORD_BUTTON_IDLE_DP = 56f
        private const val RECORD_BUTTON_ACTIVE_DP = 60f
        private const val POP_SCALE = 1.15f
        private const val POP_HALF_MS = 100L
        private const val FRAME_PADDING_PX = 64
        private const val MAX_FRAME_ZOOM = 15.0

        /** The least height of map a fit keeps between the top row and the sheet. */
        private const val MIN_FRAME_SPAN_DP = 96

        /** A photo group whose photos lie further apart than this zooms in to split them; one
         *  closer opens its popup, since no zoom would — the web's `GROUP_SPREAD_M`. */
        private const val PHOTO_GROUP_SPREAD_M = 15.0

        /** How far a photo group's tap zooms in, where its 15 m is wider than a marker. */
        private const val PHOTO_GROUP_MAX_ZOOM = 19.0

        /** How far from a tap a track still counts as tapped — the web's `TAP_TOLERANCE_PX`. */
        private const val TAP_TOLERANCE_DP = 14

        /** How often a list with Pending rows is read again. */
        private const val PENDING_POLL_MS = 2_000L

        /** Where the camera flies to on a recording's first fix — street level, so the line
         *  visibly grows from the first few metres rather than being a dot on a city. */
        private const val RECORDING_ZOOM = 16.0

        /** Find my location's minimum zoom — neighbourhood level, so the dot lands in streets
         *  the user recognises; a closer zoom the user already chose is kept. */
        private const val LOCATE_ZOOM = 14.0
        private const val FRAME_DURATION_MS = 900

        /** How long a session check may take before the map says it's checking — a fast one
         *  shouldn't flash a notice. */
        private const val CHECKING_DELAY_MS = 600L

        /** Material's and the platform's minimum touch target. */
        private const val MIN_TOUCH_TARGET_DP = 48

        /** The default range's length in activity days (`docs/SPEC.md` FR-6.1). */
        private const val DEFAULT_RANGE_DAYS = 5
        private const val STATE_MODE = "mode"
        private const val STATE_FRAMED = "framed"
        private const val STATE_RANGE_FROM = "range_from"
        private const val STATE_RANGE_TO = "range_to"
        private const val STATE_RANGE_CHOSEN = "range_chosen"
        private const val STATE_LOGO_MARGIN = "logo_margin"
        private const val STATE_ATTRIBUTION_MARGIN = "attribution_margin"
    }
}
