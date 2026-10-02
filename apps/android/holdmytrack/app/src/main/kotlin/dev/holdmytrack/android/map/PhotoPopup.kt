package dev.holdmytrack.android.map

import android.graphics.drawable.BitmapDrawable
import android.view.View
import android.widget.ImageView
import android.widget.TextView
import androidx.core.view.isVisible
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Photo
import dev.holdmytrack.android.panel.PanelFormat
import dev.holdmytrack.android.photos.PhotoImages
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.maps.MapLibreMap

/**
 * A photo's popup (`docs/SPEC.md` FR-16.7), the web's `PhotoPopup.tsx`: [view]
 * (`view_photo_popup`) over the map at the photo's marker — above it, or under it when there's
 * no room above, below [top] — following it as the camera moves ([place]). The picture, its
 * caption, when it was taken, and Full size, which opens the stored copy over the whole screen
 * to zoom into ([PhotoViewer]) — in the app, since a browser tab would have no session to fetch
 * it with. A tap on the picture opens it too. Opened on a group
 * (photos taken at one spot, which no zoom separates), it steps through it with ‹ › and
 * "2 of 5", staying anchored at the group's first photo so it doesn't jump between them.
 * Changing a photo is the Edit window's Photos tab's job, not this.
 *
 * [onChange] says which photo is showing — its marker is drawn larger meanwhile — or null once
 * the popup closes.
 */
class PhotoPopup(private val view: View, private val top: () -> Int, private val onChange: (String?) -> Unit) {

    private val context = view.context
    private val res = view.resources
    private val image = view.findViewById<ImageView>(R.id.photo_popup_image)
    private val nav = view.findViewById<View>(R.id.photo_popup_nav)
    private val previous = view.findViewById<View>(R.id.photo_popup_previous)
    private val next = view.findViewById<View>(R.id.photo_popup_next)
    private val position = view.findViewById<TextView>(R.id.photo_popup_position)
    private val caption = view.findViewById<TextView>(R.id.photo_popup_caption)
    private val taken = view.findViewById<TextView>(R.id.photo_popup_taken)

    private var map: MapLibreMap? = null
    private var photos: List<Photo> = emptyList()
    private var index = 0
    private var generation = 0
    private var viewer: PhotoViewer? = null

    /** The photos open, in route order; empty when closed. */
    val openIds: List<String>
        get() = photos.map { it.id }

    /** The one showing, or null. */
    val showingId: String?
        get() = photos.getOrNull(index)?.id

    init {
        view.findViewById<View>(R.id.photo_popup_close).setOnClickListener { close() }
        previous.setOnClickListener { step(-1) }
        next.setOnClickListener { step(1) }
        view.findViewById<View>(R.id.photo_popup_full).setOnClickListener { openViewer() }
        image.setOnClickListener { openViewer() }
        // Placed again once it has its size — the picture arrives after the first placing.
        view.addOnLayoutChangeListener { _, _, t, _, b, _, oldT, _, oldB ->
            if (b - t != oldB - oldT) place()
        }
    }

    /** Opens on [group] (one photo, or several at one spot, in route order) at its first. */
    fun show(instance: MapLibreMap, group: List<Photo>) {
        if (group.isEmpty() || group.first().lat == null) return
        map = instance
        photos = group
        index = 0
        render()
        view.isVisible = true
        place()
    }

    /** The photos in view changed: the popup keeps showing those still there, and closes when
     *  none is. */
    fun retain(available: List<Photo>) {
        if (photos.isEmpty()) return
        val byId = available.associateBy { it.id }
        val kept = photos.mapNotNull { byId[it.id] }
        if (kept.isEmpty() || kept.first().id != photos.first().id) {
            close()
            return
        }
        val showing = showingId
        photos = kept
        index = kept.indexOfFirst { it.id == showing }.coerceAtLeast(0)
        viewer?.update(photos, index)
        render()
    }

    fun close() {
        if (photos.isEmpty()) return
        generation += 1
        viewer?.dismiss()
        photos = emptyList()
        image.setImageDrawable(null)
        view.isVisible = false
        onChange(null)
    }

    /** Over the group's first photo again — on every camera move while it's open. */
    fun place() {
        val anchor = photos.firstOrNull() ?: return
        val instance = map ?: return
        val lat = anchor.lat ?: return
        val lon = anchor.lon ?: return
        val point = instance.projection.toScreenLocation(LatLng(lat, lon))
        val density = res.displayMetrics.density
        val parent = view.parent as? View ?: return
        val gutter = GUTTER_DP * density
        val offset = OFFSET_DP * density
        val width = view.width.takeIf { it > 0 } ?: (WIDTH_DP * density).toInt()
        val height = view.height
        view.translationX = (point.x - width / 2f).coerceIn(gutter, (parent.width - width - gutter).coerceAtLeast(gutter))
        val above = point.y - offset - height
        view.translationY = if (above >= top()) above else point.y + offset
    }

    private fun step(by: Int) = showAt(index + by)

    /** The viewer stepped: the popup follows. */
    private fun showAt(at: Int) {
        val to = at.coerceIn(0, photos.size - 1)
        if (to == index) return
        index = to
        render()
    }

    private fun render() {
        val photo = photos.getOrNull(index) ?: return
        nav.isVisible = photos.size > 1
        position.text = res.getString(R.string.photos_position, index + 1, photos.size)
        previous.isEnabled = index > 0
        previous.alpha = if (index > 0) 1f else DISABLED_ALPHA
        next.isEnabled = index < photos.size - 1
        next.alpha = if (index < photos.size - 1) 1f else DISABLED_ALPHA
        caption.text = photo.caption
        caption.isVisible = photo.caption != null
        taken.text = photo.takenAt?.let { res.getString(R.string.photos_taken, PanelFormat.startedAt(res, it.toString())) }
        taken.isVisible = photo.takenAt != null
        image.contentDescription = photo.caption ?: res.getString(R.string.photos_marker)
        // The stored copy at the popup's width: the thumbnail stands in until it's here.
        PhotoImages.cached(photo.thumbUrl)?.let(image::setImageBitmap) ?: image.setImageDrawable(null)
        val gen = ++generation
        HoldMyTrackApi.image(photo.url, maxSide = (WIDTH_DP * res.displayMetrics.density).toInt()) { result ->
            if (gen != generation) return@image
            result.getOrNull()?.let(image::setImageBitmap)
        }
        onChange(photo.id)
    }

    /** The viewer on the photo showing, starting from the copy the popup already holds (or its
     *  thumbnail) while the stored copy loads. */
    private fun openViewer() {
        if (photos.isEmpty()) return
        viewer?.dismiss()
        viewer = PhotoViewer(
            context,
            photos,
            index,
            preview = { photo ->
                (image.drawable as? BitmapDrawable)?.bitmap?.takeIf { photo.id == showingId } ?: PhotoImages.cached(photo.thumbUrl)
            },
            onIndex = ::showAt,
        ).apply {
            setOnDismissListener { if (viewer === this) viewer = null }
            show()
        }
    }

    private companion object {
        const val WIDTH_DP = 276
        const val GUTTER_DP = 12
        const val OFFSET_DP = 30
        const val DISABLED_ALPHA = 0.35f
    }
}
