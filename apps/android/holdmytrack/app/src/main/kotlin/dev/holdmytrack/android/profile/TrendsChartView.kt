package dev.holdmytrack.android.profile

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Path
import android.util.AttributeSet
import android.view.MotionEvent
import android.view.View
import androidx.core.graphics.ColorUtils
import dev.holdmytrack.android.R
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.roundToInt

/**
 * Trends' bars, the web's `.trends__bars`: one accent bar per period sharing the width 2dp
 * apart, 140dp tall at most, never under 2dp, top corners rounded, over a 1dp baseline at
 * ink 12%. [heights] are 0–1 (`ProfileStats.trendHeights`).
 *
 * A tap goes to [onBarTap] with the bar under it, or -1 for the bar already [selected] — the
 * web's tap fallback for its hover tooltip — and the selected bar takes the web's hover colour.
 */
class TrendsChartView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    var onBarTap: ((Int) -> Unit)? = null

    var heights: List<Double> = emptyList()
        set(value) {
            field = value
            invalidate()
        }

    var selected = -1
        set(value) {
            field = value
            invalidate()
        }

    private val density = resources.displayMetrics.density
    private val chartHeight = 140 * density
    private val gap = 2 * density
    private val minBar = 2 * density
    private val radius = resources.getDimension(R.dimen.hmt_radius_xs)
    private val baseline = 1 * density

    private val barPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = context.getColor(R.color.hmt_accent) }
    private val selectedPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = context.getColor(R.color.hmt_accent_hover) }
    private val baselinePaint = Paint().apply {
        color = ColorUtils.setAlphaComponent(context.getColor(R.color.hmt_ink), (0.12f * 255).roundToInt())
    }
    private val path = Path()
    private val corners = floatArrayOf(radius, radius, radius, radius, 0f, 0f, 0f, 0f)

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        setMeasuredDimension(getDefaultSize(suggestedMinimumWidth, widthMeasureSpec), (chartHeight + baseline).roundToInt())
    }

    private fun barWidth(): Float = if (heights.isEmpty()) 0f else (width - gap * (heights.size - 1)) / heights.size

    override fun onDraw(canvas: Canvas) {
        val bottom = chartHeight
        val w = barWidth()
        heights.forEachIndexed { i, h ->
            val left = i * (w + gap)
            val top = bottom - max(minBar, (h * chartHeight).toFloat())
            path.reset()
            path.addRoundRect(left, top, left + w, bottom, corners, Path.Direction.CW)
            canvas.drawPath(path, if (i == selected) selectedPaint else barPaint)
        }
        canvas.drawRect(0f, bottom, width.toFloat(), bottom + baseline, baselinePaint)
    }

    override fun onTouchEvent(event: MotionEvent): Boolean {
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> return heights.isNotEmpty()
            MotionEvent.ACTION_UP -> {
                // A tap in a gap counts for the bar before it, so the whole width is a target.
                val i = floor(event.x / (barWidth() + gap)).toInt().coerceIn(0, heights.size - 1)
                onBarTap?.invoke(if (i == selected) -1 else i)
                performClick()
                return true
            }
        }
        return super.onTouchEvent(event)
    }

    override fun performClick(): Boolean = super.performClick()
}