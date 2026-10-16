package dev.holdmytrack.android.export

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.util.AttributeSet
import android.view.MotionEvent
import android.view.View
import kotlin.math.hypot

/**
 * The viewfinder's drawing and touch (`apps/android/docs/IMPLEMENTATION.md` §3.5): the map
 * dimmed around [window], its border, and a crop corner at each of its corners.
 *
 * Which touches the map under it gets is decided at the first finger: on a corner, the touch
 * crops ([onCrop] while it moves, [onCropEnd] on release); inside the window, the view doesn't
 * take it, so the map does; anywhere else, on the dimmed map, the view takes it and does nothing.
 * Every later finger of the gesture goes where the first went — the parent's motion-event
 * splitting is off while the viewfinder shows — so a pinch begun in the window keeps a finger
 * that lands on the dimmed part.
 */
class ExportViewfinderView @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {

    var window: Box = Box(0f, 0f, 0f, 0f)
        set(value) {
            field = value
            invalidate()
        }

    /** The map under it reads dark — the dark flavor or satellite — so it's dimmed harder: black
     *  over an already dark map otherwise barely shows where the picture ends. */
    var darkMap = false
        set(value) {
            field = value
            shade.alpha = if (value) 200 else 148
            invalidate()
        }

    /** Corners don't respond during a capture. */
    var locked = false

    var onCrop: ((Box) -> Unit)? = null
    var onCropEnd: ((start: Box, end: Box) -> Unit)? = null

    private val density = resources.displayMetrics.density
    private val shade = Paint().apply { color = Color.argb(148, 10, 14, 12) }
    private val border = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = 2 * density
        color = Color.argb(235, 255, 255, 255)
    }
    private val cornerPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = 4 * density
        strokeCap = Paint.Cap.SQUARE
        color = Color.WHITE
        setShadowLayer(1.5f * density, 0f, 0f, Color.argb(128, 0, 0, 0))
    }
    private val cornerLength = 22 * density
    /** How near a corner a finger counts as on it: half a 48dp touch target. */
    private val cornerReach = 24 * density
    private val path = Path()

    private var dragCorner: Corner? = null
    private var dragStart = window
    private var downX = 0f
    private var downY = 0f

    override fun onDraw(canvas: Canvas) {
        val w = width.toFloat()
        val h = height.toFloat()
        val b = window
        canvas.drawRect(0f, 0f, w, b.y, shade)
        canvas.drawRect(0f, b.y + b.h, w, h, shade)
        canvas.drawRect(0f, b.y, b.x, b.y + b.h, shade)
        canvas.drawRect(b.x + b.w, b.y, w, b.y + b.h, shade)
        canvas.drawRect(b.x, b.y, b.x + b.w, b.y + b.h, border)
        path.reset()
        val l = cornerLength
        path.moveTo(b.x, b.y + l); path.lineTo(b.x, b.y); path.lineTo(b.x + l, b.y)
        path.moveTo(b.x + b.w - l, b.y); path.lineTo(b.x + b.w, b.y); path.lineTo(b.x + b.w, b.y + l)
        path.moveTo(b.x, b.y + b.h - l); path.lineTo(b.x, b.y + b.h); path.lineTo(b.x + l, b.y + b.h)
        path.moveTo(b.x + b.w - l, b.y + b.h); path.lineTo(b.x + b.w, b.y + b.h); path.lineTo(b.x + b.w, b.y + b.h - l)
        canvas.drawPath(path, cornerPaint)
    }

    @SuppressLint("ClickableViewAccessibility")
    override fun onTouchEvent(event: MotionEvent): Boolean {
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                val corner = if (locked) null else cornerAt(event.x, event.y)
                if (corner == null) return !inside(event.x, event.y)
                dragCorner = corner
                dragStart = window
                downX = event.x
                downY = event.y
                parent?.requestDisallowInterceptTouchEvent(true)
                return true
            }
            MotionEvent.ACTION_MOVE -> {
                if (dragCorner == null) return true
                onCrop?.invoke(cropAt(event))
                return true
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                if (dragCorner == null) return true
                dragCorner = null
                onCropEnd?.invoke(dragStart, window)
                return true
            }
        }
        return true
    }

    /** The window [ExportViewfinder] makes of the drag so far; it hands it back as [window]. */
    var cropOf: ((start: Box, corner: Corner, dx: Float, dy: Float) -> Box)? = null

    private fun cropAt(event: MotionEvent): Box =
        cropOf?.invoke(dragStart, dragCorner ?: Corner.SE, event.x - downX, event.y - downY) ?: window

    private fun inside(x: Float, y: Float): Boolean =
        x >= window.x && x <= window.x + window.w && y >= window.y && y <= window.y + window.h

    private fun cornerAt(x: Float, y: Float): Corner? {
        val b = window
        val corners = listOf(
            Corner.NW to (b.x to b.y),
            Corner.NE to (b.x + b.w to b.y),
            Corner.SW to (b.x to b.y + b.h),
            Corner.SE to (b.x + b.w to b.y + b.h),
        )
        return corners.minByOrNull { (_, p) -> hypot(x - p.first, y - p.second) }
            ?.takeIf { (_, p) -> hypot(x - p.first, y - p.second) <= cornerReach }
            ?.first
    }
}
