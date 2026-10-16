package dev.holdmytrack.android.photos

import android.util.Log
import android.view.View
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.fragment.app.Fragment
import dev.holdmytrack.android.R
import dev.holdmytrack.android.map.MapMode
import dev.holdmytrack.android.map.PhotoMarkerOverlay
import dev.holdmytrack.android.map.PhotoMarkers
import dev.holdmytrack.android.map.PhotoPopup
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Photo
import dev.holdmytrack.android.panel.EditActivityWindow
import dev.holdmytrack.android.panel.PanelState
import org.maplibre.android.maps.MapLibreMap

/**
 * Photos on the map (`apps/android/docs/SPEC.md` FR-2.10): the scope's photos as markers on their
 * routes, the popup a marker's tap opens, and Android's photo picker for the Edit window's Photos
 * tab, which needs no storage permission. Which photos and markers show is [PhotoMarkerScope]'s.
 *
 * Create it as a field of [fragment], so the picker is registered before the fragment starts.
 */
class PhotosController(
    private val fragment: Fragment,
    private val panelState: PanelState,
    private val map: () -> MapLibreMap?,
    private val editWindow: () -> EditActivityWindow,
    private val mode: () -> MapMode,
    private val storyId: () -> String?,
    private val recording: () -> Boolean,
    /** Flies to a group's bounds, at most to the zoom given. */
    private val frame: (DoubleArray, Double) -> Unit,
    /** A popup is opening: the spot's goes. */
    private val onPopupOpening: () -> Unit,
) {
    private lateinit var markers: PhotoMarkers
    private lateinit var popup: PhotoPopup

    /** `"activity:<id>"` or `"story:<id>"` ([PhotoMarkerScope.scope]). */
    private var scope: String? = null

    /** The scope's saved photos, in route order, and why they couldn't be read. */
    private var photos: List<Photo> = emptyList()
    private var error: String? = null
    private var loaded = false
    private var generation = 0

    /** The Photos tab's unsaved changes as they alter the markers, while it shows. */
    private var overlay: PhotoMarkerOverlay? = null

    /** What's picked goes to the Photos tab to prepare and place. */
    private val picker = fragment.registerForActivityResult(ActivityResultContracts.PickMultipleVisualMedia()) { uris ->
        if (editWindow().isOpen) editWindow().photosTab.add(uris)
    }

    /** Binds the marker layer and the popup; [top] is how far down the map's top chrome reaches. */
    fun bind(root: View, top: () -> Int) {
        markers = PhotoMarkers(root.findViewById(R.id.photo_markers), ::openMarker)
        popup = PhotoPopup(root.findViewById(R.id.photo_popup), top) { render() }
    }

    /** The Photos tab's Add photos. */
    fun pick() {
        picker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
    }

    /** The Photos tab's unsaved changes moved. */
    fun setOverlay(next: PhotoMarkerOverlay?) {
        overlay = next
        render()
    }

    /** The Edit window closed: back to the selection's or the Story's photos, read again when any was written. */
    fun editClosed(photosSaved: Boolean) {
        overlay = null
        refresh(force = photosSaved)
    }

    fun onCameraMove() {
        markers.place()
        popup.place()
    }

    /** What overlaps depends on the zoom: the groups again once the camera rests. */
    fun onCameraIdle() = markers.regroup()

    fun closePopup() = popup.close()

    /**
     * The photos for the scope now: read when the scope moves, or with [force] when they may have
     * changed (a Photos tab save, a reprocess, a Story's members). A new scope starts empty rather
     * than showing the last one's while it loads.
     */
    fun refresh(force: Boolean) {
        val edit = editWindow()
        val single = edit.single
        val next = PhotoMarkerScope.scope(mode(), edit.isOpen, single?.id, panelState.focused, storyId())
        if (next == scope && !force) {
            if (loaded && single != null) edit.setPhotos(photos, error)
            render()
            return
        }
        if (next != scope) {
            photos = emptyList()
            error = null
            loaded = false
            popup.close()
        }
        scope = next
        val gen = ++generation
        render()
        if (next == null) return
        val (kind, id) = next.split(':', limit = 2)
        HoldMyTrackApi.photos(activity = id.takeIf { kind == "activity" }, story = id.takeIf { kind == "story" }) { result ->
            if (gen != generation) return@photos
            photos = result.getOrDefault(emptyList())
            loaded = true
            error = result.exceptionOrNull()?.let { it.message?.takeIf { m -> m.isNotBlank() } ?: fragment.getString(R.string.map_unreachable) }
            if (error != null) Log.w(TAG, "could not load photos", result.exceptionOrNull())
            if (single != null && edit.single?.id == single.id) edit.setPhotos(photos, error)
            popup.retain(photos)
            render()
        }
    }

    /** The markers: the scope's photos on their tracks, with the Photos tab's unsaved changes
     *  merged in. Not while recording, while the Track tab has the map, nor for a hidden route. */
    fun render() {
        if (!::markers.isInitialized) return
        val shown = PhotoMarkerScope.shown(scope, recording(), editWindow().showingTrack, panelState.mapHidden)
        val overlay = overlay
        val items = if (shown) PhotoMarkerScope.markers(photos, panelState.mapHidden, overlay) else emptyList()
        if (!shown) popup.close()
        markers.set(map(), items, overlay?.activeId ?: popup.showingId, overlay?.activeId)
    }

    /** A marker's tap: a spread group zooms to fit ([PhotoMarkerScope.groupFit]), else the popup
     *  opens on its first photo. Only saved photos open. */
    private fun openMarker(ids: List<String>) {
        val instance = map() ?: return
        val located = ids.mapNotNull { id -> photos.firstOrNull { it.id == id }?.takeIf { it.lat != null && it.lon != null } }
        PhotoMarkerScope.groupFit(located.map { it.lat!! to it.lon!! })?.let { box ->
            frame(box, PhotoMarkerScope.GROUP_MAX_ZOOM)
            return
        }
        if (located.isEmpty()) return
        onPopupOpening()
        popup.show(instance, located)
    }

    private companion object {
        const val TAG = "HoldMyTrack"
    }
}
