package dev.holdmytrack.android.map

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Matrix
import android.graphics.Paint
import android.util.AttributeSet
import android.view.GestureDetector
import android.view.MotionEvent
import android.view.ScaleGestureDetector
import android.view.View
import android.view.animation.DecelerateInterpolator
import dev.holdmytrack.android.photos.PhotoZoom

/**
 * The photo viewer's picture ([PhotoViewer]): [bitmap] fitted to the view and centered, zoomed and
 * dragged with [PhotoZoom]'s math — a pinch zooms about the point between the fingers, a drag pans
 * once zoomed in, a double tap zooms to [PhotoZoom.DOUBLE_TAP_SCALE] about the tap (or back to
 * fitted). [zoomIn], [zoomOut] and [fit] are the toolbar's, eased; gestures follow the fingers.
 * [imageWidth]/[imageHeight] are the stored copy's, so swapping the popup's smaller copy for the
 * full one keeps the same fit and zoom.
 */
class ZoomImageView @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {

    var bitmap: Bitmap? = null
        set(value) {
            field = value
            invalidate()
        }

    private var imageWidth = 0f
    private var imageHeight = 0f

    /** Called whenever the zoom changes, so the toolbar can enable and disable its buttons. */
    var onZoomChanged: ((PhotoZoom.View) -> Unit)? = null

    var view: PhotoZoom.View = PhotoZoom.FIT
        private set

    private var animator: ValueAnimator? = null
    private val drawMatrix = Matrix()
    private val paint = Paint(Paint.FILTER_BITMAP_FLAG or Paint.ANTI_ALIAS_FLAG)

    private val stage get() = PhotoZoom.Size(width.toFloat(), height.toFloat())
    private val fitted get() = PhotoZoom.fitSize(PhotoZoom.Size(imageWidth, imageHeight), stage)

    private val scaleDetector = ScaleGestureDetector(context, object : ScaleGestureDetector.SimpleOnScaleGestureListener() {
        override fun onScale(detector: ScaleGestureDetector): Boolean {
            setView(PhotoZoom.zoomAt(view, detector.scaleFactor, detector.focusX - width / 2f, detector.focusY - height / 2f, fitted, stage))
            return true
        }
    })

    private val gestureDetector = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
        override fun onDown(e: MotionEvent): Boolean = true

        override fun onScroll(e1: MotionEvent?, e2: MotionEvent, distanceX: Float, distanceY: Float): Boolean {
            if (scaleDetector.isInProgress) return false
            setView(PhotoZoom.panBy(view, -distanceX, -distanceY, fitted, stage))
            return true
        }

        override fun onDoubleTap(e: MotionEvent): Boolean {
            val target = if (view.scale > 1f) PhotoZoom.FIT
            else PhotoZoom.zoomAt(view, PhotoZoom.DOUBLE_TAP_SCALE, e.x - width / 2f, e.y - height / 2f, fitted, stage)
            animateTo(target)
            return true
        }
    })

    /** A new photo: fitted again. */
    fun setImage(next: Bitmap?, width: Int, height: Int) {
        imageWidth = width.toFloat()
        imageHeight = height.toFloat()
        animator?.cancel()
        bitmap = next
        setView(PhotoZoom.FIT)
    }

    fun zoomIn() = animateTo(PhotoZoom.zoomAt(view, PhotoZoom.STEP, 0f, 0f, fitted, stage))

    fun zoomOut() = animateTo(PhotoZoom.zoomAt(view, 1 / PhotoZoom.STEP, 0f, 0f, fitted, stage))

    fun fit() = animateTo(PhotoZoom.FIT)

    override fun onTouchEvent(event: MotionEvent): Boolean {
        if (event.actionMasked == MotionEvent.ACTION_DOWN) animator?.cancel()
        // Zoomed in, the drag is the picture's, not a scrolling parent's.
        if (view.scale > 1f || event.pointerCount > 1) parent?.requestDisallowInterceptTouchEvent(true)
        scaleDetector.onTouchEvent(event)
        gestureDetector.onTouchEvent(event)
        return true
    }

    override fun onSizeChanged(w: Int, h: Int, oldw: Int, oldh: Int) {
        // A rotation can leave an edge pulled in.
        setView(PhotoZoom.clamp(view, fitted, stage))
    }

    override fun onDraw(canvas: Canvas) {
        val image = bitmap ?: return
        val fit = fitted
        if (fit.width <= 0f) return
        val scale = fit.width / image.width * view.scale
        drawMatrix.reset()
        drawMatrix.setScale(scale, scale)
        drawMatrix.postTranslate(
            width / 2f + view.x - image.width * scale / 2f,
            height / 2f + view.y - image.height * scale / 2f,
        )
        canvas.drawBitmap(image, drawMatrix, paint)
    }

    private fun setView(next: PhotoZoom.View) {
        if (next == view) return
        view = next
        invalidate()
        onZoomChanged?.invoke(next)
    }

    private fun animateTo(target: PhotoZoom.View) {
        animator?.cancel()
        val from = view
        animator = ValueAnimator.ofFloat(0f, 1f).apply {
            duration = STEP_MS
            interpolator = DecelerateInterpolator()
            addUpdateListener {
                val t = it.animatedValue as Float
                setView(
                    PhotoZoom.View(
                        from.scale + (target.scale - from.scale) * t,
                        from.x + (target.x - from.x) * t,
                        from.y + (target.y - from.y) * t,
                    ),
                )
            }
            start()
        }
    }

    private companion object {
        const val STEP_MS = 180L
    }
}
