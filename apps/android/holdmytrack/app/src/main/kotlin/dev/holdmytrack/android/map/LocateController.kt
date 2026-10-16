package dev.holdmytrack.android.map

import android.Manifest
import android.annotation.SuppressLint
import android.content.pm.PackageManager
import android.view.View
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import androidx.fragment.app.Fragment
import dev.holdmytrack.android.R
import org.maplibre.android.location.LocationComponentActivationOptions
import org.maplibre.android.location.modes.CameraMode
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.Style

/**
 * Find my location: MapLibre's position dot and tracking camera, which flies to the first fix and
 * follows until the user pans, like the web's geolocate control. Activated on the first tap, so a
 * user who never asks is never located. Approximate location is enough, so either grant counts.
 *
 * Create it as a field of [fragment], so its permission request is registered before the fragment starts.
 */
class LocateController(
    private val fragment: Fragment,
    private val map: () -> MapLibreMap?,
    private val style: () -> Style?,
) {
    /** The button's panel, hidden whole while recording so no empty panel is left. */
    private lateinit var panel: View

    private val permissionLauncher = fragment.registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        if (granted()) {
            show()
        } else {
            Toast.makeText(fragment.requireContext(), R.string.locate_needs_location, Toast.LENGTH_LONG).show()
        }
    }

    fun bind(root: View) {
        panel = root.findViewById(R.id.locate_panel)
        root.findViewById<View>(R.id.locate_button).setOnClickListener { onTap() }
    }

    fun setShown(shown: Boolean) {
        panel.visibility = if (shown) View.VISIBLE else View.GONE
    }

    private fun onTap() {
        if (granted()) {
            show()
        } else {
            permissionLauncher.launch(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION))
        }
    }

    private fun granted(): Boolean {
        val context = fragment.requireContext()
        return listOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION)
            .any { ContextCompat.checkSelfPermission(context, it) == PackageManager.PERMISSION_GRANTED }
    }

    @SuppressLint("MissingPermission") // Only reached once granted() holds.
    private fun show() {
        val instance = map() ?: return
        val loaded = style() ?: return
        val location = instance.locationComponent
        if (!location.isLocationComponentActivated) {
            location.activateLocationComponent(LocationComponentActivationOptions.Builder(fragment.requireContext(), loaded).build())
        }
        location.isLocationComponentEnabled = true
        location.setCameraMode(CameraMode.TRACKING, FLY_DURATION_MS, maxOf(instance.cameraPosition.zoom, LOCATE_ZOOM), null, null, null)
    }

    private companion object {
        /** The minimum zoom: neighbourhood level. A closer zoom is kept. */
        const val LOCATE_ZOOM = 14.0
        const val FLY_DURATION_MS = 900L
    }
}
