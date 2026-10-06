package dev.holdmytrack.android.map

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import android.graphics.Typeface
import android.util.AttributeSet
import android.view.MotionEvent
import android.view.View
import android.view.ViewConfiguration
import dev.holdmytrack.android.R
import java.time.Month
import java.time.format.TextStyle
import kotlin.math.roundToInt

/**
 * The sheet's date scrubber: one bar per activity day in the window, as tall as the day's distance
 * ([ScrubberBars.levels]), its day of the month under it — and its month too where a new one
 * starts — with the selection a soft accent window over its days, a handle at each end.
 *
 * Like the track it replaced, it knows [slots] and two slot *boundaries*, [start] and [end]
 * (0..slots, or -1 / slots + 1 for a handle off either side of the window, which isn't drawn), and
 * nothing about dates; `DateRangeSlider` turns those into a selection. The whole view is the touch
 * target, and a touch is one of three gestures:
 *
 * - **A handle dragged.** A press within [HANDLE_GRAB_DP] of a drawn handle goes to [onPress] with
 *   its boundary, the drag to [onDrag] — unclamped, so a finger held past an edge can pull the
 *   window along — and the release to [onRelease].
 * - **A swipe.** A press anywhere else that moves sideways past the touch slop scrolls the window
 *   instead: [onSwipe] gets each whole slot the finger has moved, positive toward the past (the
 *   days follow the finger, as a list does), and the selection stays as it was.
 * - **A tap.** A press elsewhere let go without moving is [onPress] at the nearest boundary, then
 *   [onRelease] — the nearer handle moves there.
 */
class DayScrubberView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    var slots = 0
        private set
    var start = 0
        private set
    var end = 0
        private set
    private var levels: List<Float> = emptyList()
    private var labels: List<ScrubberBars.Label> = emptyList()

    var onPress: ((boundary: Int) -> Unit)? = null
    var onDrag: ((boundary: Int) -> Unit)? = null
    var onRelease: (() -> Unit)? = null
    var onSwipe: ((days: Int) -> Unit)? = null

    private enum class Gesture { HANDLE, UNDECIDED, SWIPE }
    private var gesture: Gesture? = null
    private var downX = 0f
    /** Where the swipe's last whole slot was counted from. */
    private var swipeAnchorX = 0f
    private val touchSlop = ViewConfiguration.get(context).scaledTouchSlop

    private val density = resources.displayMetrics.density
    private val barAreaHeight = 38 * density
    private val barMaxWidth = 10 * density
    private val barMinHeight = 4 * density
    private val barMaxHeight = 26 * density
    private val barBottomGap = 5 * density
    private val barRadius = 3 * density
    private val windowRadius = resources.getDimension(R.dimen.hmt_radius_lg)
    private val handleWidth = 5 * density
    private val handleHeight = 28 * density
    private val handleGrab = HANDLE_GRAB_DP * density
    private val labelSize = 10 * resources.displayMetrics.scaledDensity

    private val accent = context.getColor(R.color.hmt_accent)
    private val accentStrong = context.getColor(R.color.hmt_accent_strong)
    private val windowPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = context.getColor(R.color.hmt_accent_container) }
    private val barPaint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val handlePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = accentStrong }
    private val inkColor = context.getColor(R.color.hmt_ink)
    private val mutedColor = context.getColor(R.color.hmt_ink_muted)
    private val outsideBarColor = context.getColor(R.color.hmt_border_strong)
    private val labelPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        textSize = labelSize
        textAlign = Paint.Align.CENTER
    }
    private val monthTypeface = Typeface.create(Typeface.DEFAULT, Typeface.BOLD)
    private val dayTypeface = Typeface.DEFAULT
    private val rect = RectF()
    private val locale = resources.configuration.locales[0]

    fun set(slots: Int, start: Int, end: Int, levels: List<Float>, labels: List<ScrubberBars.Label>) {
        if (slots == this.slots && start == this.start && end == this.end && levels == this.levels && labels == this.labels) return
        this.slots = slots
        this.start = start
        this.end = end
        this.levels = levels
        this.labels = labels
        invalidate()
    }

    private val left get() = paddingLeft.toFloat()
    private val slotWidth get() = if (slots == 0) 0f else (width - paddingLeft - paddingRight).toFloat() / slots

    private fun xOf(boundary: Int) = left + boundary.coerceIn(0, slots) * slotWidth

    /** The slot boundary nearest [x], not clamped to the window. */
    private fun boundaryAt(x: Float) = if (slotWidth == 0f) 0 else ((x - left) / slotWidth).roundToInt()

    override fun onDraw(canvas: Canvas) {
        if (slots == 0) return
        val top = paddingTop.toFloat()
        val s = start.coerceIn(0, slots)
        val e = end.coerceIn(0, slots)
        if (e > s) {
            rect.set(xOf(s), top, xOf(e), top + barAreaHeight)
            canvas.drawRoundRect(rect, windowRadius, windowRadius, windowPaint)
        }
        val barWidth = minOf(barMaxWidth, slotWidth * 0.5f)
        val baseline = top + barAreaHeight - barBottomGap
        for (i in 0 until slots) {
            val inside = i >= s && i < e
            val cx = left + (i + 0.5f) * slotWidth
            val level = levels.getOrElse(i) { ScrubberBars.MIN_LEVEL }
            val height = barMinHeight + level * (barMaxHeight - barMinHeight)
            barPaint.color = if (inside) accent else outsideBarColor
            rect.set(cx - barWidth / 2, baseline - height, cx + barWidth / 2, baseline)
            canvas.drawRoundRect(rect, barRadius, barRadius, barPaint)
            val label = labels.getOrNull(i) ?: continue
            val month = label.month
            labelPaint.typeface = if (month != null) monthTypeface else dayTypeface
            labelPaint.color = if (inside) inkColor else mutedColor
            val text = if (month != null) Month.of(month).getDisplayName(TextStyle.SHORT_STANDALONE, locale) else label.day.toString()
            canvas.drawText(text, cx, top + barAreaHeight + labelSize + 2 * density, labelPaint)
        }
        val cy = top + barAreaHeight / 2
        for (boundary in intArrayOf(start, end)) {
            if (boundary < 0 || boundary > slots) continue
            val x = xOf(boundary).coerceIn(left + handleWidth / 2, width - paddingRight - handleWidth / 2)
            rect.set(x - handleWidth / 2, cy - handleHeight / 2, x + handleWidth / 2, cy + handleHeight / 2)
            canvas.drawRoundRect(rect, handleWidth / 2, handleWidth / 2, handlePaint)
        }
    }

    /** The drawn handle within reach of [x], if any — the nearer of the two. */
    private fun handleNear(x: Float): Int? =
        intArrayOf(start, end)
            .filter { it in 0..slots && Math.abs(xOf(it) - x) <= handleGrab }
            .minByOrNull { Math.abs(xOf(it) - x) }

    // A drag is a gesture over the whole scrubber, not a click; the ‹ › buttons beside the range
    // and the range written above it are what TalkBack reads.
    @SuppressLint("ClickableViewAccessibility")
    override fun onTouchEvent(event: MotionEvent): Boolean {
        if (!isEnabled || slots == 0) return false
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                // The sheet under it drags; a press here is the scrubber's.
                parent?.requestDisallowInterceptTouchEvent(true)
                downX = event.x
                val handle = handleNear(event.x)
                if (handle != null) {
                    gesture = Gesture.HANDLE
                    onPress?.invoke(handle)
                } else {
                    gesture = Gesture.UNDECIDED
                }
            }
            MotionEvent.ACTION_MOVE -> when (gesture) {
                Gesture.HANDLE -> onDrag?.invoke(boundaryAt(event.x))
                Gesture.UNDECIDED -> if (Math.abs(event.x - downX) > touchSlop) {
                    gesture = Gesture.SWIPE
                    swipeAnchorX = downX
                    swipe(event.x)
                }
                Gesture.SWIPE -> swipe(event.x)
                null -> Unit
            }
            MotionEvent.ACTION_UP -> {
                when (gesture) {
                    Gesture.HANDLE -> onRelease?.invoke()
                    Gesture.UNDECIDED -> {
                        onPress?.invoke(boundaryAt(event.x).coerceIn(0, slots))
                        onRelease?.invoke()
                    }
                    Gesture.SWIPE, null -> Unit
                }
                gesture = null
            }
            MotionEvent.ACTION_CANCEL -> {
                if (gesture == Gesture.HANDLE) onRelease?.invoke()
                gesture = null
            }
        }
        return true
    }

    /** Hands on each whole slot the finger has moved since the last one counted. */
    private fun swipe(x: Float) {
        if (slotWidth == 0f) return
        val moved = ((x - swipeAnchorX) / slotWidth).toInt()
        if (moved == 0) return
        swipeAnchorX += moved * slotWidth
        onSwipe?.invoke(moved)
    }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        val wanted = (paddingTop + barAreaHeight + labelSize + 6 * density + paddingBottom).toInt()
        setMeasuredDimension(MeasureSpec.getSize(widthMeasureSpec), resolveSize(wanted, heightMeasureSpec))
    }

    private companion object {
        /** How near a handle a press grabs it rather than starting a swipe or a tap — 24dp, half
         *  a 48dp target either side of a 5dp handle. */
        const val HANDLE_GRAB_DP = 24f
    }
}
