package dev.holdmytrack.android.map

import android.Manifest
import android.content.pm.PackageManager
import android.view.View
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.fragment.app.Fragment
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Spot
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.Style

/**
 * Points of interest on the map (`apps/android/docs/SPEC.md` FR-2.8): the shown categories, the
 * captured badges, the popup a spot's tap opens, Show in this area, and capture mode, which needs
 * precise location (a capture counts only fixes accurate to 25 m).
 *
 * Create it as a field of [fragment], so its permission request is registered before the fragment starts.
 */
class SpotsController(
    private val fragment: Fragment,
    private val map: () -> MapLibreMap?,
    /** The style once the user layers are on it. */
    private val style: () -> Style?,
    /** Fits a `[west, south, east, north]` box into the map between the chrome and the panel. */
    private val frame: (DoubleArray) -> Unit,
) {
    private lateinit var popup: SpotPopup
    private lateinit var capture: CaptureMode

    /** Created with the map instance it reads the camera of. */
    private var showInArea: ShowInArea? = null

    /** The spot whose Capture is waiting on the permission prompt. */
    private var pendingCapture: Spot? = null

    private val permissionLauncher = fragment.registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        val spot = pendingCapture
        pendingCapture = null
        if (preciseLocation()) {
            spot?.let(capture::start)
        } else {
            Toast.makeText(fragment.requireContext(), R.string.spots_capture_needs_location, Toast.LENGTH_LONG).show()
        }
    }

    /** Binds the popup and the capture banner; [top] is how far down the map's top chrome reaches. */
    fun bind(root: View, top: () -> Int) {
        popup = SpotPopup(root.findViewById(R.id.spot_popup), top, ::onCaptureTap)
        capture = CaptureMode(
            fragment.requireActivity() as AppCompatActivity,
            root.findViewById(R.id.capture_banner),
            map,
            style,
            frame = frame,
            onCaptured = popup::renderCaptured,
        )
    }

    /** The map instance is ready: Show in this area reads its camera. */
    fun onMapReady(root: View, instance: MapLibreMap, shown: List<MapSpots.Category>) {
        showInArea = ShowInArea(root.findViewById(R.id.show_in_area), instance) { spots ->
            MapSpots.setInArea(style(), spots)
        }.apply { setCategories(shown) }
    }

    fun onCameraMove() = popup.place()

    fun onCameraIdle() {
        showInArea?.onCameraIdle()
    }

    /** The user layers were just put on [loaded]: the places last, over everything else, labels included. */
    fun attach(loaded: Style, shown: List<MapSpots.Category>) {
        MapSpots.attach(loaded, fragment.requireContext(), shown)
    }

    /** After [attach], once the style counts as attached: badges and capture mode come back. */
    fun onStyleAttached() {
        loadCaptures(onlyNew = false)
        capture.onStyleAttached()
    }

    /**
     * The account's captured spots (`docs/SPEC.md` FR-15.6), for the badges and the popup: all of
     * them when the overlays attach, then on every return only what's newer ([capturesSince]).
     * Kept as they were on a failure.
     */
    fun loadCaptures(onlyNew: Boolean) {
        val since = if (onlyNew) capturesSince(MapSpots.captured) else null
        HoldMyTrackApi.spotCaptures(since) { result ->
            val fresh = result.getOrNull()?.associate { it.spotId to it.capturedAt } ?: return@spotCaptures
            val captures = if (since != null) mergeCaptures(MapSpots.captured, fresh) else fresh
            MapSpots.setCaptured(style(), captures)
            popup.renderCaptured()
        }
    }

    /** The Layers menu's Points of interest, [shown] being what's drawn of them. Unticking the
     *  category being captured stops the capture. */
    fun setPicked(categories: List<MapSpots.Category>, shown: List<MapSpots.Category>) {
        MapSpots.set(fragment.requireContext(), categories)
        show(shown)
        capture.spot?.let { target -> if (categories.none { it.wire == target.category }) capture.stop() }
    }

    /** Draws these places, and closes the popup of one that's no longer drawn. */
    fun show(shown: List<MapSpots.Category>) {
        style()?.let { MapSpots.setCategories(it, shown) }
        popup.spot?.let { open -> if (shown.none { it.wire == open.category }) popup.close() }
        showInArea?.setCategories(shown)
    }

    /** A tap on the map: a spot's badge opens its popup (true); anywhere else closes it (false). */
    fun tap(instance: MapLibreMap, point: LatLng): Boolean {
        val spot = MapSpots.spotAt(instance, instance.projection.toScreenLocation(point), fragment.resources.displayMetrics.density)
        if (spot != null) {
            popup.show(instance, spot)
            return true
        }
        popup.close()
        return false
    }

    fun closePopup() = popup.close()

    /** The Track tab has the map: the places step aside, and a capture stops. */
    fun setEditing(editing: Boolean) {
        style()?.let { MapSpots.setEditing(it, editing) }
        if (!editing) return
        popup.close()
        capture.stop()
    }

    fun resume() = capture.resume()

    fun pause() = capture.pause()

    fun stopCapture() = capture.stop()

    /** A spot's Capture: capture mode on it, once precise location is granted. */
    private fun onCaptureTap(spot: Spot) {
        popup.close()
        if (preciseLocation()) {
            capture.start(spot)
        } else {
            pendingCapture = spot
            permissionLauncher.launch(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION))
        }
    }

    private fun preciseLocation() =
        ContextCompat.checkSelfPermission(fragment.requireContext(), Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
}
