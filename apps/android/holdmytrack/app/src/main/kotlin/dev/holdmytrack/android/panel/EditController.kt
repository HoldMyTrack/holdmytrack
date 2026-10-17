package dev.holdmytrack.android.panel

import android.annotation.SuppressLint
import android.view.View
import androidx.activity.OnBackPressedCallback
import dev.holdmytrack.android.R
import dev.holdmytrack.android.map.EditPreview
import dev.holdmytrack.android.map.MapMode
import dev.holdmytrack.android.map.MapOverlays
import dev.holdmytrack.android.map.SpotsController
import dev.holdmytrack.android.map.TrackEditOverlay
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.TrackPoint
import dev.holdmytrack.android.photos.PhotosController
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style

/**
 * The two editors that open along the bottom of the map and hold it meanwhile: the Edit window
 * over the toolbar's target, with its Track tab's session, and the Private location editor. Back
 * closes each; a press on a Private location's handle or on a track point in Move point mode takes
 * the touch before the map pans.
 */
class EditController(
    root: View,
    private val panelState: PanelState,
    private val panel: () -> ActivitiesPanel,
    private val photos: PhotosController,
    private val spots: SpotsController,
    private val map: () -> MapLibreMap?,
    /** The style once loaded, user layers or not. */
    private val style: () -> Style?,
    /** The style once the user layers are on it. */
    private val attached: () -> Style?,
    private val mode: () -> MapMode,
    /** Frames activities between the chrome and the window. */
    private val frame: (List<Activity>) -> Unit,
    /** Where the Private location editor opens a new circle, between the top row and what covers the bottom. */
    openAreaCenterY: () -> Float,
    onPrivateLocationsChanged: () -> Unit,
    /** The Edit window opened or closed: the chrome around it follows. */
    private val onWindowChanged: (open: Boolean) -> Unit,
    /** The Private location editor opened or closed: the chrome around it follows. */
    onPrivacyChanged: (open: Boolean) -> Unit,
    /** The window closed: [saved] when something was written, [editedTrack] when a track edit was applied. */
    private val onWindowClosed: (saved: Boolean, editedTrack: String?) -> Unit,
) {
    /** The activity whose track the Track tab is editing, while it is. */
    private var editingTrackId: String? = null
    private val windowView: View = root.findViewById(R.id.edit_window)

    val window: EditActivityWindow = EditActivityWindow(
        windowView,
        root.findViewById(R.id.edit_bar),
        onStartTrack = ::startTrack,
        onDrawTrack = ::drawTrack,
        onPickPhotos = photos::pick,
        onPhotoOverlay = photos::setOverlay,
        onTabChanged = photos::render,
        onClose = ::onClosed,
    )

    lateinit var privacy: PrivateLocationEditor
        private set

    /** Back closes the Edit window before it leaves the map. */
    val closeWindowOnBack = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = window.back()
    }

    /** Back closes the Private location editor, unsaved, before it leaves the map. */
    val closePrivacyOnBack = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = privacy.stop()
    }

    init {
        privacy = PrivateLocationEditor(
            root.findViewById(R.id.private_editor),
            map = map,
            style = attached,
            onEditorOpen = {
                closePrivacyOnBack.isEnabled = true
                onPrivacyChanged(true)
            },
            // Closing the editor gives the map back.
            onEditorClose = {
                closePrivacyOnBack.isEnabled = false
                if (privacy.isShowing) privacy.stop()
                onPrivacyChanged(false)
            },
            onChanged = onPrivateLocationsChanged,
            openAreaCenterY = openAreaCenterY,
        )
    }

    /** Whether the Private location editor is open over the map. */
    val privacyEditing: Boolean get() = privacy.isEditing

    /** The toolbar's Edit: the window over its target, the panel held and inert under it. */
    fun open(group: List<Activity>) {
        panel().dismissPopups()
        panel().hold(true)
        window.open(group, ActivityFacets.typeFacets(panelState.activities, null))
        // The photos follow the window: its one activity's, which its Photos tab manages.
        photos.refresh(force = false)
        closeWindowOnBack.isEnabled = true
        onWindowChanged(true)
    }

    /** Takes the press before the map pans when it lands on a handle or a point. */
    @SuppressLint("ClickableViewAccessibility")
    fun takeHandleDrags(mapView: MapView) {
        mapView.setOnTouchListener { _, event ->
            when {
                privacy.isShowing -> privacy.onMapTouch(event)
                window.isOpen -> map()?.let { window.trackEditor.onMapTouch(event, it, mapView.resources.displayMetrics.density) } ?: false
                else -> false
            }
        }
    }

    /** The Track tab opened: the other tracks and the bands away, and the camera on this one. */
    private fun startTrack(activity: Activity) {
        editingTrackId = activity.id
        attached()?.let { MapOverlays.setEditingTrack(it, true, mode()) }
        spots.setEditing(true)
        // Once the Track tab has laid out, so the fit leaves room under the window as it now is.
        windowView.post { frame(listOf(activity)) }
    }

    /** The track editor's overlay: the points as edited, its knobs and what would go. */
    private fun drawTrack(visible: List<TrackPoint>?, lo: Int, hi: Int, preview: EditPreview, split: Int?) {
        val loaded = style() ?: return
        if (visible == null) TrackEditOverlay.clear(loaded) else TrackEditOverlay.set(loaded, visible, lo, hi, preview, split)
    }

    private fun onClosed(saved: Boolean, trackApplied: Boolean, photosSaved: Boolean) {
        photos.editClosed(photosSaved)
        val edited = editingTrackId?.also {
            style()?.let(TrackEditOverlay::clear)
            attached()?.let { loaded -> MapOverlays.setEditingTrack(loaded, false, mode()) }
            spots.setEditing(false)
        }
        editingTrackId = null
        closeWindowOnBack.isEnabled = false
        panel().hold(false)
        onWindowChanged(false)
        onWindowClosed(saved, edited?.takeIf { trackApplied })
    }
}
