package dev.holdmytrack.android.export

import android.view.View
import android.view.ViewGroup
import androidx.activity.OnBackPressedCallback
import dev.holdmytrack.android.R
import dev.holdmytrack.android.map.LayersController
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.Style

/**
 * The map image export (`apps/android/docs/SPEC.md` FR-2.11): the rail's camera opens the
 * viewfinder (`ExportViewfinder`), and while it shows the map's chrome steps aside, since it would
 * cover the picture. Back closes it, nothing captured.
 */
class ExportController(
    root: View,
    private val map: () -> MapLibreMap?,
    /** Whether the style has loaded, so there is a map to capture. */
    private val styleLoaded: () -> Boolean,
    private val style: () -> Style?,
    private val layers: LayersController,
    /** Makes the export's hidden map look like this one. */
    prepare: (Style) -> Unit,
    /** The chrome that steps aside while the viewfinder shows. */
    private val chrome: List<View>,
    /** The viewfinder opened or closed: popups close, and the mode chip follows. */
    private val onShown: (Boolean) -> Unit,
) {
    private val viewfinder = ExportViewfinder(
        host = root.findViewById<ViewGroup>(R.id.map_root),
        map = { map().takeIf { styleLoaded() } },
        styleUrl = layers::styleUrl,
        prepare = prepare,
        darkBase = { style()?.let(layers::darkBase) ?: layers.isNight() },
        satelliteOn = { layers.satelliteOn },
        onShown = ::render,
    )

    private val panel: View = root.findViewById(R.id.export_panel)

    /** Back closes the viewfinder before anything else on the map. */
    val backCallback = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = viewfinder.close()
    }

    init {
        root.findViewById<View>(R.id.export_button).setOnClickListener { viewfinder.toggle() }
    }

    val isOpen: Boolean get() = viewfinder.isOpen

    fun close() = viewfinder.close()

    /** A recording has the map to itself; the export would draw what it hides. */
    fun setAvailable(available: Boolean) {
        panel.visibility = if (available) View.VISIBLE else View.GONE
        if (!available) viewfinder.close()
    }

    private fun render(shown: Boolean) {
        backCallback.isEnabled = shown
        val visibility = if (shown) View.INVISIBLE else View.VISIBLE
        chrome.forEach { it.visibility = visibility }
        onShown(shown)
        map()?.uiSettings?.apply {
            isLogoEnabled = !shown
            isAttributionEnabled = !shown
            isCompassEnabled = !shown
        }
    }
}
