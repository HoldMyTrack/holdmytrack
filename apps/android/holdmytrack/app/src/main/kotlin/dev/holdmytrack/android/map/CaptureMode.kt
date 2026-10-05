package dev.holdmytrack.android.map

import android.annotation.SuppressLint
import android.content.Context
import android.hardware.Sensor
import android.hardware.SensorEvent
import android.hardware.SensorEventListener
import android.hardware.SensorManager
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Looper
import android.view.HapticFeedbackConstants
import android.view.Surface
import android.view.View
import android.view.WindowManager
import android.widget.ImageView
import android.widget.TextView
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import com.google.android.material.progressindicator.LinearProgressIndicator
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.ApiException
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Spot
import dev.holdmytrack.android.panel.PanelFormat
import dev.holdmytrack.android.recording.RecordingFormat
import org.maplibre.android.location.LocationComponentActivationOptions
import org.maplibre.android.location.modes.CameraMode
import org.maplibre.android.location.modes.RenderMode
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.Style
import kotlin.math.roundToInt

/**
 * Capture mode (`apps/android/docs/SPEC.md` FR-2.8, ADR-0023): started from a spot's popup, it
 * reads the phone's GPS and guides the user to that spot — an arrow turned towards it, the
 * distance, a dashed line on the map and every spot area shaded darker ([MapSpots.setCapturing])
 * — then counts 30 s inside it ([SpotCapture]) and captures it on the server.
 *
 * Location is read only while the app is in the foreground ([resume]/[pause], from
 * MapFragment's own), the same platform GPS provider the recording uses: the app asks for no
 * background location, and a hold pauses while the app is away. The screen stays on while it
 * runs. Stop, Back, a track edit, unticking the spot's category, or the capture itself ends it.
 */
class CaptureMode(
    private val activity: AppCompatActivity,
    private val banner: View,
    private val map: () -> MapLibreMap?,
    private val style: () -> Style?,
    /** Fits a `[west, south, east, north]` box into the map between the chrome and the panel. */
    private val frame: (DoubleArray) -> Unit,
    /** A capture was saved: the popup's captured line, and anything else showing it. */
    private val onCaptured: () -> Unit,
) {
    private val res = activity.resources
    private val arrow = banner.findViewById<ImageView>(R.id.capture_arrow)
    private val title = banner.findViewById<TextView>(R.id.capture_title)
    private val detail = banner.findViewById<TextView>(R.id.capture_detail)
    private val progress = banner.findViewById<LinearProgressIndicator>(R.id.capture_progress)
    private val locationManager = activity.getSystemService(Context.LOCATION_SERVICE) as LocationManager
    private val sensorManager = activity.getSystemService(Context.SENSOR_SERVICE) as SensorManager
    private val rotationSensor: Sensor? = sensorManager.getDefaultSensor(Sensor.TYPE_ROTATION_VECTOR)

    /** The spot aimed at, while capture mode is on. */
    var spot: Spot? = null
        private set
    private var capture: SpotCapture? = null
    private var resumed = false
    private var listening = false
    private var framed = false
    private var saving = false
    private var locationDotShown = false

    /** Whether Find my location had the dot on already — then it stays on after capture mode. */
    private var dotWasOn = false

    /** Which way the phone's top points, degrees from north; null without a rotation sensor
     *  (the arrow then follows the map's own bearing). */
    private var heading: Float? = null
    private var bearing: Double? = null

    private val back = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = stop()
    }

    private val locationListener = LocationListener(::onLocation)

    private val sensorListener = object : SensorEventListener {
        private val matrix = FloatArray(9)
        private val orientation = FloatArray(3)

        override fun onSensorChanged(event: SensorEvent) {
            SensorManager.getRotationMatrixFromVector(matrix, event.values)
            SensorManager.getOrientation(matrix, orientation)
            val azimuth = Math.toDegrees(orientation[0].toDouble()).toFloat() + screenRotation()
            // Smoothed a little, taking the short way round, so the arrow doesn't jitter.
            val last = heading
            heading = if (last == null) azimuth else last + 0.2f * (((azimuth - last + 540) % 360) - 180)
            renderArrow()
        }

        override fun onAccuracyChanged(sensor: Sensor?, accuracy: Int) = Unit
    }

    private val retryLoad = Runnable { spot?.let(::load) }
    private val retrySave = Runnable { save() }
    private val finish = Runnable { stop() }

    init {
        banner.findViewById<View>(R.id.capture_stop).setOnClickListener { stop() }
        activity.onBackPressedDispatcher.addCallback(activity, back)
    }

    val isOn: Boolean get() = spot != null

    /** Starts capture mode on [target], replacing any other. Location permission is the
     *  caller's to have asked for first. */
    fun start(target: Spot) {
        stop()
        spot = target
        framed = false
        back.isEnabled = true
        activity.window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        MapSpots.setCapturing(style(), target.id)
        banner.isVisible = true
        render(null)
        load(target)
    }

    // Only turns off the dot showLocationDot turned on, which needed location granted.
    @SuppressLint("MissingPermission")
    fun stop() {
        if (spot == null) return
        spot = null
        capture = null
        saving = false
        bearing = null
        banner.removeCallbacks(retryLoad)
        banner.removeCallbacks(retrySave)
        banner.removeCallbacks(finish)
        stopListening()
        back.isEnabled = false
        activity.window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        MapSpots.setCapturing(style(), null)
        banner.isVisible = false
        if (locationDotShown) {
            map()?.locationComponent?.takeIf { it.isLocationComponentActivated }?.let {
                it.renderMode = RenderMode.NORMAL
                // Capture's own dot goes with it: without its updates it would sit where the
                // last fix was, over the spot's badge.
                if (!dotWasOn) it.isLocationComponentEnabled = false
            }
            locationDotShown = false
        }
    }

    /** MapFragment's onResume: location again, if capture mode is on. */
    fun resume() {
        resumed = true
        if (capture != null) startListening()
    }

    /** MapFragment's onPause: no location while the app is away. */
    fun pause() {
        resumed = false
        stopListening()
    }

    /** A new style dropped the guide line and the darker areas: put them back. */
    fun onStyleAttached() {
        spot?.let { MapSpots.setCapturing(style(), it.id) }
    }

    private fun load(target: Spot) {
        HoldMyTrackApi.spotDetail(target.id) { result ->
            if (spot?.id != target.id) return@spotDetail
            val detail = result.getOrElse { error ->
                // A place retired since the map loaded it (ADR-0027) is a 404 to anyone who
                // hasn't captured it — the only accounts capture mode starts for.
                if (error is ApiException && error.code == 404) return@spotDetail gone()
                this.detail.setText(R.string.spots_capture_load_failed)
                banner.postDelayed(retryLoad, RETRY_MS)
                return@spotDetail
            }
            capture = SpotCapture(detail.area, GeoPoint(target.lat, target.lon))
            render(capture?.state)
            if (resumed) startListening()
        }
    }

    @SuppressLint("MissingPermission") // start() is only called with location granted.
    private fun startListening() {
        if (listening) return
        listening = true
        runCatching {
            locationManager.requestLocationUpdates(LocationManager.GPS_PROVIDER, UPDATE_MS, 0f, locationListener, Looper.getMainLooper())
        }
        rotationSensor?.let { sensorManager.registerListener(sensorListener, it, SensorManager.SENSOR_DELAY_UI) }
        showLocationDot()
    }

    private fun stopListening() {
        if (!listening) return
        listening = false
        locationManager.removeUpdates(locationListener)
        sensorManager.unregisterListener(sensorListener)
        heading = null
    }

    /** MapLibre's own position dot, with the phone's heading on it — without moving the camera,
     *  which frames the user and the spot once instead ([frame]). */
    @SuppressLint("MissingPermission")
    private fun showLocationDot() {
        val instance = map() ?: return
        val loaded = style() ?: return
        val location = instance.locationComponent
        if (!locationDotShown) dotWasOn = location.isLocationComponentActivated && location.isLocationComponentEnabled
        if (!location.isLocationComponentActivated) {
            location.activateLocationComponent(LocationComponentActivationOptions.Builder(activity, loaded).build())
            location.cameraMode = CameraMode.NONE
        }
        location.isLocationComponentEnabled = true
        location.renderMode = RenderMode.COMPASS
        locationDotShown = true
    }

    private fun onLocation(location: Location) {
        val counting = capture ?: return
        if (saving) return
        val accuracy = if (location.hasAccuracy()) location.accuracy else null
        val state = counting.onFix(GeoPoint(location.latitude, location.longitude), accuracy, location.elapsedRealtimeNanos / 1_000_000)
        when (state) {
            is SpotCapture.State.Away -> {
                MapSpots.setGuide(style(), state.from, state.to)
                frameOnce(state.from, state.to)
            }
            is SpotCapture.State.Holding -> MapSpots.setGuide(style(), null, null)
            is SpotCapture.State.Captured -> {
                MapSpots.setGuide(style(), null, null)
                saving = true
                banner.performHapticFeedback(HapticFeedbackConstants.CONFIRM)
                save()
            }
            SpotCapture.State.Locating -> Unit
        }
        render(state)
    }

    /** Once per start: the camera fits the user and the spot, so the way there is on screen. */
    private fun frameOnce(from: GeoPoint, to: GeoPoint) {
        if (framed) return
        framed = true
        val target = spot ?: return
        val lats = listOf(from.lat, to.lat, target.lat)
        val lons = listOf(from.lon, to.lon, target.lon)
        frame(doubleArrayOf(lons.min(), lats.min(), lons.max(), lats.max()))
    }

    private fun save() {
        val target = spot ?: return
        val at = (capture?.state as? SpotCapture.State.Captured)?.at ?: return
        HoldMyTrackApi.captureSpot(target.id, at.lat, at.lon) { result ->
            if (spot?.id != target.id) return@captureSpot
            result.onSuccess { saved ->
                MapSpots.setCaptured(style(), MapSpots.captured + (saved.spotId to saved.capturedAt))
                onCaptured()
                detail.text = target.name ?: ""
                detail.isVisible = target.name != null
                banner.postDelayed(finish, DONE_MS)
            }.onFailure { error ->
                if (error is ApiException && error.code == 410) {
                    gone()
                } else if (error is ApiException && error.code == 422) {
                    // The server saw the position outside: hold again.
                    saving = false
                    capture?.reset()
                    render(capture?.state)
                    detail.setText(R.string.spots_capture_outside)
                } else {
                    detail.setText(R.string.spots_capture_retrying)
                    banner.postDelayed(retrySave, RETRY_MS)
                }
            }
        }
    }

    /** The place is gone from OpenStreetMap and retired (ADR-0027): there's nothing to capture.
     *  Says so, and ends capture mode a moment later. */
    private fun gone() {
        capture = null // so resume() doesn't start listening again
        stopListening()
        MapSpots.setGuide(style(), null, null)
        arrow.isVisible = false
        progress.isVisible = false
        title.setText(R.string.spots_capture_gone)
        detail.isVisible = false
        banner.postDelayed(finish, GONE_MS)
    }

    private fun render(state: SpotCapture.State?) {
        val target = spot ?: return
        val name = target.name ?: MapSpots.Category.fromWire(target.category)?.let { res.getString(it.label) } ?: target.category
        progress.isVisible = state is SpotCapture.State.Holding || state is SpotCapture.State.Captured
        detail.isVisible = true
        arrow.isVisible = state is SpotCapture.State.Away
        when (state) {
            null, SpotCapture.State.Locating -> {
                title.text = res.getString(R.string.spots_capture_get_into, name)
                detail.setText(R.string.spots_capture_locating)
            }
            is SpotCapture.State.Away -> {
                title.text = res.getString(R.string.spots_capture_get_into, name)
                detail.text = res.getString(R.string.spots_capture_away, distance(state.distanceM))
                bearing = state.bearing
                renderArrow()
            }
            is SpotCapture.State.Holding -> {
                title.setText(R.string.spots_capture_stay)
                val left = ((SpotCapture.HOLD_MS - state.heldMs + 999) / 1000).toInt()
                detail.text = res.getString(R.string.spots_capture_seconds_left, left)
                progress.setProgressCompat((state.heldMs / 1000).toInt(), true)
            }
            is SpotCapture.State.Captured -> {
                title.setText(R.string.spots_capture_done)
                detail.setText(R.string.spots_capture_saving)
                progress.setProgressCompat(progress.max, true)
            }
        }
    }

    /** The arrow at the spot's bearing, relative to where the phone points — or, without a
     *  compass, to the map's own north. */
    private fun renderArrow() {
        val toSpot = bearing ?: return
        val facing = heading?.toDouble() ?: map()?.cameraPosition?.bearing ?: 0.0
        arrow.rotation = (toSpot - facing).toFloat()
    }

    /** Metres, or feet for an imperial account, under a kilometre (a tenth of a mile); then the
     *  panel's own km or mi to one decimal. */
    private fun distance(meters: Double): String {
        val imperial = RecordingFormat.imperial()
        return when {
            imperial && meters < FEET_LIMIT_M -> res.getString(R.string.spots_capture_unit_ft, (meters * FEET_PER_M).roundToInt())
            !imperial && meters < 1000 -> res.getString(R.string.spots_capture_unit_m, meters.roundToInt())
            else -> PanelFormat.distance(res, meters)
        }
    }

    @Suppress("DEPRECATION") // activity.display needs API 30; the window manager's works on all.
    private fun screenRotation(): Float = when (activity.windowManager.defaultDisplay.rotation) {
        Surface.ROTATION_90 -> 90f
        Surface.ROTATION_180 -> 180f
        Surface.ROTATION_270 -> 270f
        else -> 0f
    }

    private companion object {
        const val UPDATE_MS = 1000L
        const val RETRY_MS = 5000L

        /** How long "Captured!" stays before capture mode ends by itself. */
        const val DONE_MS = 2500L

        /** How long "no longer on the map" stays: long enough to read. */
        const val GONE_MS = 4000L
        const val FEET_PER_M = 3.28084
        const val FEET_LIMIT_M = 160.9344
    }
}
