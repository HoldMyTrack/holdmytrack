package dev.holdmytrack.android.map

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import android.util.AttributeSet
import android.view.MotionEvent
import android.view.View
import androidx.core.graphics.ColorUtils
import dev.holdmytrack.android.R
import kotlin.math.roundToInt

/**
 * The date-range slider's track: a thin line, the selected stretch in the accent colour, and two
 * knobs — the look of the web's phone slider (`apps/web/src/index.css`'s `.date-range-slider`).
 *
 * It knows [slots] and two slot *boundaries*, [start] and [end] (0..slots, or -1 / slots + 1
 * for a knob off either side of the window, which isn't drawn), and nothing about dates;
 * `DateRangeSlider` turns those into a selection. The whole view is the touch target: a press
 * goes to [onPress] with the nearest boundary, a drag to [onDrag], a release to [onRelease].
 * The line is inset by half a knob, so a knob on either end stays whole.
 */
class DateRangeTrackView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    var slots = 0
        private set
    var start = 0
        private set
    var end = 0
        private set

    var onPress: ((boundary: Int) -> Unit)? = null
    var onDrag: ((boundary: Int) -> Unit)? = null
    var onRelease: (() -> Unit)? = null

    private val density = resources.displayMetrics.density
    private val knobWidth = 14 * density
    private val knobHeight = 26 * density
    private val knobStroke = 2 * density
    private val knobRadius = resources.getDimension(R.dimen.hmt_radius_sm)
    private val lineHeight = 2 * density
    private val lineRadius = resources.getDimension(R.dimen.hmt_radius_xs)

    private val accent = context.getColor(R.color.hmt_accent)
    private val linePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = ColorUtils.setAlphaComponent(context.getColor(R.color.hmt_ink), (0.12f * 255).roundToInt())
    }
    private val fillPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = accent }

    /** The web's `--fm-shadow-sm`: 0 1px 4px, ink at 25%. */
    private val knobPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = context.getColor(R.color.hmt_surface)
        setShadowLayer(
            2 * density,
            0f,
            density,
            ColorUtils.setAlphaComponent(context.getColor(R.color.hmt_ink), (0.25f * 255).roundToInt()),
        )
    }
    private val knobStrokePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = accent
        style = Paint.Style.STROKE
        strokeWidth = knobStroke
    }
    private val rect = RectF()

    fun set(slots: Int, start: Int, end: Int) {
        if (slots == this.slots && start == this.start && end == this.end) return
        this.slots = slots
        this.start = start
        this.end = end
        invalidate()
    }

    private val lineLeft get() = paddingLeft + knobWidth / 2
    private val lineWidth get() = (width - paddingLeft - paddingRight - knobWidth).coerceAtLeast(1f)

    private fun clamp(boundary: Int) = boundary.coerceIn(0, slots)

    private fun xOf(boundary: Int) =
        lineLeft + if (slots == 0) 0f else clamp(boundary).toFloat() / slots * lineWidth

    /** The slot boundary nearest [x]. */
    private fun boundaryAt(x: Float) = clamp(((x - lineLeft) / lineWidth * slots).roundToInt())

    override fun onDraw(canvas: Canvas) {
        val cy = paddingTop + (height - paddingTop - paddingBottom) / 2f
        rect.set(lineLeft, cy - lineHeight / 2, lineLeft + lineWidth, cy + lineHeight / 2)
        canvas.drawRoundRect(rect, lineRadius, lineRadius, linePaint)
        if (slots == 0) return
        if (clamp(end) > clamp(start)) {
            rect.set(xOf(start), cy - lineHeight / 2, xOf(end), cy + lineHeight / 2)
            canvas.drawRoundRect(rect, lineRadius, lineRadius, fillPaint)
        }
        for (boundary in intArrayOf(start, end)) {
            if (boundary < 0 || boundary > slots) continue
            val x = xOf(boundary)
            rect.set(x - knobWidth / 2, cy - knobHeight / 2, x + knobWidth / 2, cy + knobHeight / 2)
            canvas.drawRoundRect(rect, knobRadius, knobRadius, knobPaint)
            rect.inset(knobStroke / 2, knobStroke / 2)
            canvas.drawRoundRect(rect, knobRadius, knobRadius, knobStrokePaint)
        }
    }

    // A drag is a gesture over the whole track, not a click; the Earlier/Later buttons and each
    // knob's date in the labels under it are what TalkBack reads (Phase 5's Accessibility item).
    @SuppressLint("ClickableViewAccessibility")
    override fun onTouchEvent(event: MotionEvent): Boolean {
        if (!isEnabled || slots == 0) return false
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                parent?.requestDisallowInterceptTouchEvent(true)
                onPress?.invoke(boundaryAt(event.x))
            }
            MotionEvent.ACTION_MOVE -> onDrag?.invoke(boundaryAt(event.x))
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> onRelease?.invoke()
        }
        return true
    }
}
