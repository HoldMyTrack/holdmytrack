package dev.holdmytrack.android.map

import android.content.Context
import android.graphics.Bitmap
import android.os.Bundle
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import androidx.appcompat.app.AppCompatDialog
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.isVisible
import androidx.core.view.updatePadding
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Photo
import dev.holdmytrack.android.panel.PanelFormat
import dev.holdmytrack.android.photos.PhotoZoom

/**
 * The photo viewer (`apps/android/docs/SPEC.md` FR-2.10), the web's `PhotoViewer.tsx`: a photo
 * over the whole screen, opened from its popup ([PhotoPopup]), to pinch, double-tap and drag
 * ([ZoomImageView]) or zoom with the bottom bar's buttons. Steps through a group as the popup
 * does, and with it: [onIndex] moves the popup too. Back or Close dismisses it, leaving the popup
 * open.
 *
 * The popup's smaller copy ([preview]) shows at once; the stored copy replaces it when it
 * arrives, at the same fit and zoom, since the view sizes by the photo's own [Photo.width] and
 * [Photo.height] rather than the bitmap's.
 */
class PhotoViewer(
    context: Context,
    private var photos: List<Photo>,
    private var index: Int,
    private val preview: (Photo) -> Bitmap?,
    private val onIndex: (Int) -> Unit,
) : AppCompatDialog(context, R.style.Theme_HoldMyTrack_PhotoViewer) {

    private lateinit var image: ZoomImageView
    private lateinit var loading: View
    private lateinit var error: View
    private lateinit var caption: TextView
    private lateinit var taken: TextView
    private lateinit var position: TextView
    private lateinit var previous: View
    private lateinit var next: View
    private lateinit var zoomOut: View
    private lateinit var zoomIn: View
    private lateinit var fit: View

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.view_photo_viewer)
        window?.let {
            it.setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT)
            WindowCompat.setDecorFitsSystemWindows(it, false)
        }
        image = findViewById(R.id.photo_viewer_image)!!
        loading = findViewById(R.id.photo_viewer_loading)!!
        error = findViewById(R.id.photo_viewer_error)!!
        caption = findViewById(R.id.photo_viewer_caption)!!
        taken = findViewById(R.id.photo_viewer_taken)!!
        position = findViewById(R.id.photo_viewer_position)!!
        previous = findViewById(R.id.photo_viewer_previous)!!
        next = findViewById(R.id.photo_viewer_next)!!
        zoomOut = findViewById(R.id.photo_viewer_zoom_out)!!
        zoomIn = findViewById(R.id.photo_viewer_zoom_in)!!
        fit = findViewById(R.id.photo_viewer_fit)!!

        // The bars clear the status bar, the cutout and the navigation bar; the picture runs
        // under all of them.
        val top = findViewById<View>(R.id.photo_viewer_top)!!
        val bottom = findViewById<View>(R.id.photo_viewer_bottom)!!
        ViewCompat.setOnApplyWindowInsetsListener(top.rootView) { _, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
            top.updatePadding(top = bars.top)
            bottom.updatePadding(bottom = bars.bottom)
            insets
        }

        findViewById<View>(R.id.photo_viewer_close)!!.setOnClickListener { dismiss() }
        previous.setOnClickListener { step(index - 1) }
        next.setOnClickListener { step(index + 1) }
        zoomOut.setOnClickListener { image.zoomOut() }
        zoomIn.setOnClickListener { image.zoomIn() }
        fit.setOnClickListener { image.fit() }
        image.onZoomChanged = ::renderZoom
        render()
    }

    /** The popup's photos changed under it (a refetch): kept open on the same photo if it's
     *  still there, closed if not. */
    fun update(next: List<Photo>, nextIndex: Int) {
        val same = next.getOrNull(nextIndex)?.id == photos.getOrNull(index)?.id
        photos = next
        index = nextIndex
        if (!same && isShowing) render()
    }

    private fun step(to: Int) {
        if (to !in photos.indices || to == index) return
        index = to
        onIndex(to)
        render()
    }

    private fun render() {
        val photo = photos[index]
        caption.text = photo.caption
        caption.isVisible = photo.caption != null
        taken.text = photo.takenAt?.let { context.getString(R.string.photos_taken, PanelFormat.startedAt(context.resources, it.toString())) }
        taken.isVisible = photo.takenAt != null
        val group = photos.size > 1
        position.text = context.getString(R.string.photos_position, index + 1, photos.size)
        for (view in listOf(previous, position, next)) view.isVisible = group
        enable(previous, index > 0)
        enable(next, index < photos.size - 1)
        error.isVisible = false
        val first = preview(photo)
        image.setImage(first, photo.width, photo.height)
        loading.isVisible = first == null
        HoldMyTrackApi.image(photo.url, maxSide = null) { result ->
            if (photos.getOrNull(index)?.id != photo.id) return@image
            loading.isVisible = false
            val full = result.getOrNull()
            if (full != null) image.bitmap = full else error.isVisible = image.bitmap == null
        }
    }

    private fun renderZoom(view: PhotoZoom.View) {
        enable(zoomOut, view.scale > 1f)
        enable(fit, view.scale > 1f)
        enable(zoomIn, view.scale < PhotoZoom.MAX_SCALE)
    }

    /** The icon buttons' tint is a plain color with no disabled state, so a disabled one is
     *  dimmed by hand, as the popup's ‹ › are. */
    private fun enable(button: View, on: Boolean) {
        button.isEnabled = on
        button.alpha = if (on) 1f else DISABLED_ALPHA
    }

    override fun onStart() {
        super.onStart()
        renderZoom(image.view)
    }

    private companion object {
        const val DISABLED_ALPHA = 0.35f
    }
}
