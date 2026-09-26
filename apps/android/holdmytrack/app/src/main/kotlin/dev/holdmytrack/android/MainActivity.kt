package dev.holdmytrack.android

import android.Manifest
import android.annotation.SuppressLint
import android.content.ComponentName
import android.content.Intent
import android.content.ServiceConnection
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.os.Bundle
import android.os.IBinder
import android.util.Log
import android.view.View
import android.view.ViewGroup.MarginLayoutParams
import android.view.WindowInsets
import android.widget.Button
import android.widget.PopupMenu
import android.widget.TextView
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.core.view.isVisible
import com.google.android.material.button.MaterialButton
import dev.holdmytrack.android.map.ActivityDays
import dev.holdmytrack.android.map.DateRange
import dev.holdmytrack.android.map.DateRangeSlider
import dev.holdmytrack.android.map.MapMode
import dev.holdmytrack.android.map.MapOverlays
import dev.holdmytrack.android.net.ApiException
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.RecordButton
import dev.holdmytrack.android.recording.RecordedActivitiesActivity
import dev.holdmytrack.android.recording.RecordingService
import dev.holdmytrack.android.recording.RecordingState
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
 * need, and the Normal / Fog of War / Heatmap toggle between them. The toggle and the burger
 * menu (Profile, Sync) both float over the map top-start, rather than living in a bar of their
 * own, mirroring the web client's own on-map mode control (`apps/web/src/map/MapView.tsx`).
 *
 * Along the bottom in Normal mode, the date range the tracks are drawn for — the web's phone
 * footer (`map/DateRangeSlider`), defaulting to the five most recent activity days as the web
 * does (`docs/SPEC.md` FR-6.1).
 *
 * Also the one place GPS recording is controlled from in the app: a record button floats
 * bottom-centre (tap to start, tap to pause/resume, hold for two seconds to stop —
 * `RecordingService` does the rest, and its notification offers the same controls). While a recording is in
 * progress the map shows only that recording's live track: the mode toggle and every history
 * layer are hidden (`MapOverlays.setRecording`), and the camera follows the latest fix.
 *
 * Never mounted without a session: a signed-out visitor is handed straight to
 * `SignInActivity`, mirroring web's `AuthGate` (`docs/IMPLEMENTATION.md` §4.13). A bare basemap
 * with none of the three user layers — all behind `requireAuth` server-side — would be a weak
 * first impression next to the Demo account that screen offers one tap away.
 */
class MainActivity : AppCompatActivity() {

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
    private lateinit var menuButton: Button
    private lateinit var modeBar: View
    private lateinit var modeButtons: Map<MapMode, MaterialButton>
    private lateinit var recordButton: RecordButton
    private lateinit var locateButton: MaterialButton

    /** Find my location's panel — what hides while recording, so no empty panel is left. */
    private lateinit var locatePanel: View

    /** The record button and, under it, the date-range footer. */
    private lateinit var bottomChrome: View
    private lateinit var dateFooter: View
    private lateinit var dateSlider: DateRangeSlider
    private lateinit var activityDays: ActivityDays

    /** The date range the tracks are drawn for; null until the first page of activity days
     *  has set the default, when the tracks are the whole history. */
    private var selectedRange: DateRange? = null

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

    private val recorderConnection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName, binder: IBinder) {
            val service = (binder as RecordingService.LocalBinder).service
            recorder = service
            service.onChange = ::renderRecording
            renderRecording()
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
            Toast.makeText(this, R.string.recording_needs_location, Toast.LENGTH_LONG).show()
        }
    }

    /** Asked for on the first tap of Find my location. Approximate is enough to show the
     *  user roughly where they are, so either grant counts. */
    private val locatePermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions(),
    ) {
        if (hasLocationPermission()) {
            showMyLocation()
        } else {
            Toast.makeText(this, R.string.locate_needs_location, Toast.LENGTH_LONG).show()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Before any layout or MapView exists, so a signed-out launch never draws a map at all.
        if (!Session.isSignedIn) {
            openSignIn()
            return
        }
        // Likewise an account whose email isn't confirmed: the server refuses it every tile.
        if (!Session.emailVerified) {
            openVerifyEmail()
            return
        }
        setContentView(R.layout.activity_main)

        topChrome = findViewById(R.id.top_chrome)
        notice = findViewById(R.id.map_notice)
        noticeText = findViewById(R.id.map_notice_text)
        noticeDetail = findViewById(R.id.map_notice_detail)
        noticeAction = findViewById(R.id.map_notice_action)
        menuButton = findViewById(R.id.menu_button)
        modeBar = findViewById(R.id.mode_bar)
        modeButtons = mapOf(
            MapMode.NORMAL to findViewById(R.id.mode_normal),
            MapMode.FOG to findViewById(R.id.mode_fog),
            MapMode.HEATMAP to findViewById(R.id.mode_heatmap),
        )
        modeButtons.forEach { (value, button) -> button.setOnClickListener { setMode(value) } }
        menuButton.setOnClickListener { showMenu(it) }

        recordButton = findViewById(R.id.record_button)
        recordButton.setOnClickListener { onRecordTap() }
        recordButton.onHoldComplete = ::stopRecording

        locateButton = findViewById(R.id.locate_button)
        locatePanel = findViewById(R.id.locate_panel)
        locateButton.setOnClickListener { onLocateTap() }

        bottomChrome = findViewById(R.id.bottom_chrome)
        dateFooter = findViewById(R.id.date_footer)
        activityDays = ActivityDays(DateRangeSlider.WINDOW_DAYS, ::onActivityDaysChanged)
        dateSlider = DateRangeSlider(dateFooter, onPan = activityDays::panBy) { range ->
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
        setMode(mode)

        insetSystemBars()
        if (savedInstanceState == null) handleStopIntent(intent)

        mapView = findViewById(R.id.map_view)
        mapView.onCreate(savedInstanceState)

        // Failures are reported here rather than only in logcat: a style or tile fetch that
        // 404s leaves a plausible-looking blank map behind, with nothing on screen to say so.
        mapView.addOnDidFailLoadingMapListener { error -> reportFailure(error) }

        mapView.getMapAsync { instance ->
            map = instance
            // A whole-world view is the honest starting camera until the session is verified
            // and the activity extent is known; `frameActivities` replaces it with the user's
            // own, and leaves it for an account with no geometry yet.
            instance.cameraPosition = CameraPosition.Builder()
                .target(LatLng(20.0, 0.0))
                .zoom(1.0)
                .build()
            applyCompassMargin()
            applyAttributionMargin()
            loadStyle()
        }
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
            MapOverlays.attachLiveTrack(loaded)
            syncSession()
            renderRecording()
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleStopIntent(intent)
    }

    /** The notification's Stop comes through here rather than straight to `RecordingService`
     *  (`RecordingService.buildNotification`): the Save screen that follows a stop needs an app
     *  window in the foreground to open over. Ignored when relaunched from recents, which
     *  replays the last intent — that would stop a recording started since. */
    private fun handleStopIntent(intent: Intent) {
        if (intent.action != RecordingService.ACTION_STOP) return
        if (intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0) return
        stopRecording()
    }

    /**
     * Keeps the floating chrome clear of the status bar and the gesture navigation pill.
     *
     * Not optional at this target SDK: from API 35 the system draws every app edge to edge and
     * ignores the old opt-out. The top row (burger, modes, Find my location) sits in a `layout_margin`ed
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
        val bottomPadding = bottomChrome.paddingBottom
        findViewById<View>(R.id.map_root).setOnApplyWindowInsetsListener { _, insets ->
            val bars = insets.getInsets(WindowInsets.Type.systemBars())
            (topBar.layoutParams as MarginLayoutParams).topMargin = barTopMargin + bars.top
            topBar.requestLayout()
            bottomChrome.setPadding(
                bottomChrome.paddingLeft,
                bottomChrome.paddingTop,
                bottomChrome.paddingRight,
                bottomPadding + bars.bottom,
            )
            systemBarInsetTop = bars.top
            applyCompassMargin()
            insets
        }
        // The compass sits below the top row and any notice under it, whose heights are only
        // known once they're laid out; the attribution above the date-range footer likewise.
        topChrome.addOnLayoutChangeListener { _, _, _, _, _, _, _, _, _ -> applyCompassMargin() }
        bottomChrome.addOnLayoutChangeListener { _, _, _, _, _, _, _, _, _ -> applyAttributionMargin() }
    }

    /**
     * MapLibre's own compass control defaults to top-end with a small fixed margin, unaware of
     * the status bar — found sitting directly behind the clock/battery indicator on a real
     * device — and of the top row, whose Find my location button shares that corner. Called from both `insetSystemBars` and `getMapAsync` because whichever of the
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
        // Below the top row (burger, modes, Find my location), which already sits below the
        // status bar, and below the notice while one is up; before that row is laid out, below
        // the status bar at least.
        val belowTopBar = if (notice.isVisible) notice.bottom else findViewById<View>(R.id.top_bar).bottom
        settings.setCompassMargins(
            settings.compassMarginLeft,
            compassBaseMarginTop + maxOf(systemBarInsetTop, belowTopBar),
            settings.compassMarginRight,
            settings.compassMarginBottom,
        )
    }

    /**
     * MapLibre's logo and attribution sit bottom-start, where the date-range footer is — and the
     * attribution is the basemap's ODbL credit, which must stay visible (`FR-2.4`). So while
     * the footer is up both move above it; the rest of the time they keep their own margins,
     * which already clear the gesture bar.
     */
    private fun applyAttributionMargin() {
        val instance = map ?: return
        val settings = instance.uiSettings
        if (!attributionBaseCaptured) {
            logoBaseMarginBottom = settings.logoMarginBottom
            attributionBaseMarginBottom = settings.attributionMarginBottom
            attributionBaseCaptured = true
        }
        val above = if (dateFooter.isVisible) bottomChrome.height - dateFooter.top else 0
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

        modeBarReady = true
        modeBar.visibility = if (isRecording()) View.GONE else View.VISIBLE
        if (daysStale) {
            daysStale = false
            activityDays.reload()
        }
        renderDateFooter()

        val loaded = style ?: return
        if (!overlaysAttached) {
            MapOverlays.attach(loaded, mode, selectedRange)
            overlaysAttached = true
            if (isRecording()) MapOverlays.setRecording(loaded, true, mode)
            frameActivities()
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
     * Draws the tracks for [range]. Only a range the user picked moves the camera — onto that
     * range's activities, as the web does (`docs/SPEC.md` FR-6.6) — never the default
     * re-deriving itself after a sync.
     */
    private fun applyRange(range: DateRange, fly: Boolean) {
        selectedRange = range
        dateSlider.value = range
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setTrackRange(it, range) }
        if (!fly) return
        HoldMyTrackApi.activityBounds(range.from, range.to) { result ->
            val box = result.getOrNull() ?: return@activityBounds
            // A later pick, or a recording started since, owns the camera now.
            if (range != selectedRange || isRecording()) return@activityBounds
            flyTo(box)
        }
    }

    /** The footer is Normal mode's alone, as on the web (Fog and Heatmap ignore the range —
     *  `docs/SPEC.md` FR-4.2), and steps aside while recording. It waits for the session and
     *  the first page of days, and an account with no activity at all has nothing to pick
     *  from — the empty notice speaks for it instead. */
    private fun renderDateFooter() {
        val visible = modeBarReady && mode == MapMode.NORMAL && !isRecording() &&
            activityDays.ready && activityDays.earliest != null
        dateFooter.visibility = if (visible) View.VISIBLE else View.GONE
        (recordButton.layoutParams as MarginLayoutParams).bottomMargin =
            resources.getDimensionPixelSize(if (visible) R.dimen.hmt_space_16 else R.dimen.hmt_space_32)
        recordButton.requestLayout()
    }

    private fun isRecording() = (recorder?.state ?: RecordingState.IDLE) != RecordingState.IDLE

    private fun hasPermission(permission: String) =
        ContextCompat.checkSelfPermission(this, permission) == PackageManager.PERMISSION_GRANTED

    /** Idle: start (asking for permissions first if they're missing). Recording or paused:
     *  toggle between the two. Nothing here ever asks for a name or a type — see
     *  `RecordingService`'s class doc for where those come from. */
    private fun onRecordTap() {
        if (isRecording()) {
            startService(RecordingService.intent(this, RecordingService.ACTION_TOGGLE))
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
        startService(RecordingService.intent(this, RecordingService.ACTION_STOP))
    }

    private fun startRecording() {
        ContextCompat.startForegroundService(this, RecordingService.intent(this, RecordingService.ACTION_START))
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
            location.activateLocationComponent(LocationComponentActivationOptions.Builder(this, loaded).build())
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
        recordButton.setBackgroundResource(background)
        recordButton.contentDescription = getString(description)
        recordButton.holdEnabled = active
        if (modeBarReady) modeBar.visibility = if (active) View.GONE else View.VISIBLE
        locatePanel.visibility = if (active) View.GONE else View.VISIBLE
        renderDateFooter()
        updateNoticeVisibility()

        val loaded = style ?: return
        if (overlaysAttached) MapOverlays.setRecording(loaded, active, mode)
        val points = if (active) recorder?.points().orEmpty() else emptyList()
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

    private fun openSignIn() {
        SignInActivity.open(this)
        finish()
    }

    private fun openVerifyEmail() {
        VerifyEmailActivity.open(this)
        finish()
    }

    private fun verifyStoredSession() {
        if (verifying) return
        verifying = true
        notice.postDelayed(showChecking, CHECKING_DELAY_MS)
        HoldMyTrackApi.verifySession { result ->
            verifying = false
            notice.removeCallbacks(showChecking)
            hideNotice(Notice.CHECKING)
            result.onSuccess { emailVerified ->
                Session.markVerified(emailVerified)
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
        noticeText.setTextColor(getColor(if (failed) R.color.hmt_danger else R.color.hmt_ink))
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

    /** "Nothing on your map yet" has nothing to say while a recording has the map, so it
     *  steps aside then and comes back after; the failures stay up regardless. */
    private fun updateNoticeVisibility() {
        val kind = shownNotice
        notice.visibility = when {
            kind == null -> View.GONE
            kind == Notice.EMPTY && isRecording() -> View.GONE
            else -> View.VISIBLE
        }
    }

    /** The burger menu: the destinations that don't fit on the map itself. */
    private fun showMenu(anchor: View) {
        val menu = PopupMenu(this, anchor)
        menu.menu.add(0, MENU_PROFILE, 0, R.string.menu_profile)
        menu.menu.add(0, MENU_SYNC, 1, R.string.menu_sync)
        menu.menu.add(0, MENU_RECORDED_ACTIVITIES, 2, R.string.menu_recorded_activities)
        menu.setOnMenuItemClickListener { item ->
            when (item.itemId) {
                MENU_PROFILE -> startActivity(Intent(this, ProfileActivity::class.java))
                MENU_SYNC -> startActivity(Intent(this, SyncActivity::class.java))
                MENU_RECORDED_ACTIVITIES -> startActivity(Intent(this, RecordedActivitiesActivity::class.java))
            }
            true
        }
        menu.show()
    }

    private fun setMode(next: MapMode) {
        mode = next
        modeButtons.forEach { (value, button) ->
            val active = value == next
            button.isChecked = active
        }
        style?.takeIf { overlaysAttached }?.let { MapOverlays.setMode(it, next) }
        renderDateFooter()
    }

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
                    showNotice(Notice.EMPTY, getString(R.string.map_empty), action = R.string.menu_sync) {
                        startActivity(Intent(this, SyncActivity::class.java))
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
     * Flies the camera to fit [box] (`[minLon, minLat, maxLon, maxLat]`). The zoom is capped
     * because a single short activity — or one clipped to almost nothing by a Private location —
     * has a near-zero extent, and fitting the camera to that box lands well past the basemap's
     * z14 data, on a grey rectangle.
     */
    private fun flyTo(box: DoubleArray) {
        val instance = map ?: return
        val bounds = LatLngBounds.from(box[3], box[2], box[1], box[0])
        val fitted = instance.getCameraForLatLngBounds(bounds, IntArray(4) { FRAME_PADDING_PX }) ?: return
        val target = CameraPosition.Builder(fitted)
            .zoom(minOf(fitted.zoom, MAX_FRAME_ZOOM))
            .build()
        instance.animateCamera(CameraUpdateFactory.newCameraPosition(target), FRAME_DURATION_MS)
    }

    /**
     * Flavor follows the system's day/night setting. The API serves five (`light`, `dark`,
     * `white`, `black`, `grayscale`); this picks between the two general-purpose ones, and
     * there is no in-app preference — decided in Phase 5 (`apps/android/docs/ROADMAP.md`):
     * the system setting is the only input, and the app's own chrome stays light.
     */
    private fun styleUrl(): String {
        val night = resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK ==
            Configuration.UI_MODE_NIGHT_YES
        val flavor = if (night) "dark" else "light"
        return "${BuildConfig.API_BASE_URL}/v1/map/style/$flavor"
    }

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
        recorderBound = bindService(Intent(this, RecordingService::class.java), recorderConnection, BIND_AUTO_CREATE)
    }

    override fun onResume() {
        super.onResume()
        mapView.onResume()
        daysStale = true
        syncSession()
        // An empty map is asked again on every return — typically from Sync — so the notice
        // goes, and the camera frames the new history, as soon as something has arrived.
        if (shownNotice == Notice.EMPTY) {
            framed = false
            frameActivities()
        }
    }

    override fun onPause() {
        mapView.onPause()
        super.onPause()
    }

    override fun onStop() {
        if (recorderBound) {
            recorder?.onChange = null
            unbindService(recorderConnection)
            recorderBound = false
            recorder = null
        }
        mapView.onStop()
        super.onStop()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        mapView.onSaveInstanceState(outState)
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

    // A signed-out launch finishes from onCreate before the MapView exists, which skips every
    // callback above but still reaches this one.
    override fun onDestroy() {
        if (::mapView.isInitialized) mapView.onDestroy()
        if (::dateSlider.isInitialized) dateSlider.release()
        super.onDestroy()
    }

    private companion object {
        const val TAG = "HoldMyTrack"
        const val FRAME_PADDING_PX = 64
        const val MAX_FRAME_ZOOM = 15.0

        /** Where the camera flies to on a recording's first fix — street level, so the line
         *  visibly grows from the first few metres rather than being a dot on a city. */
        const val RECORDING_ZOOM = 16.0

        /** Find my location's minimum zoom — neighbourhood level, so the dot lands in streets
         *  the user recognises; a closer zoom the user already chose is kept. */
        const val LOCATE_ZOOM = 14.0
        const val FRAME_DURATION_MS = 900

        /** How long a session check may take before the map says it's checking — a fast one
         *  shouldn't flash a notice. */
        const val CHECKING_DELAY_MS = 600L
        const val MENU_PROFILE = 1
        const val MENU_SYNC = 2
        const val MENU_RECORDED_ACTIVITIES = 3

        /** The default range's length in activity days (`docs/SPEC.md` FR-6.1). */
        const val DEFAULT_RANGE_DAYS = 5
        const val STATE_RANGE_FROM = "range_from"
        const val STATE_RANGE_TO = "range_to"
        const val STATE_RANGE_CHOSEN = "range_chosen"
        const val STATE_LOGO_MARGIN = "logo_margin"
        const val STATE_ATTRIBUTION_MARGIN = "attribution_margin"
    }
}
