package dev.holdmytrack.android.profile

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import android.util.AttributeSet
import android.view.MotionEvent
import android.view.View
import dev.holdmytrack.android.R
import java.time.LocalDate
import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.roundToInt

/**
 * One year's activity grid, the web's `.year-grid__body` (`services/server/internal/web/static/pages.css`)
 * drawn at its sizes — 12dp cells 4dp apart, the Mon/Wed/Fri row labels and a month label over
 * the column its first day falls in, both at 9sp and ink 45%. A year is wider than a phone, so
 * it sits in a `HorizontalScrollView`, labels included, as on the web.
 *
 * Where the web shows a cell's detail as its hover tooltip, a tap here goes to [onDayTap] with
 * that day — or null for padding, outside the cells, or the day already [selected] — and the
 * selected day is ringed in ink.
 */
class YearGridView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    var onDayTap: ((LocalDate?) -> Unit)? = null

    var layout: YearLayout? = null
        set(value) {
            field = value
            requestLayout()
            invalidate()
        }

    var selected: LocalDate? = null
        set(value) {
            field = value
            invalidate()
        }

    private val density = resources.displayMetrics.density
    private val cell = 12 * density
    private val gap = 4 * density
    private val pitch = cell + gap
    private val monthRow = 14 * density + 2 * density
    private val labelGap = 6 * density
    private val radius = resources.getDimension(R.dimen.hmt_radius_xs)
    private val ring = 1.5f * density

    // The legend's squares take the same four colours, as backgroundTints.
    private val levelPaints = listOf(R.color.profile_level_0, R.color.profile_level_1, R.color.profile_level_2, R.color.profile_level_3)
        .map { id -> Paint(Paint.ANTI_ALIAS_FLAG).apply { color = context.getColor(id) } }
    private val ringPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = context.getColor(R.color.hmt_ink)
        style = Paint.Style.STROKE
        strokeWidth = ring
    }
    private val labelPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = context.getColor(R.color.profile_ink_45)
        textSize = resources.getDimension(R.dimen.hmt_text_3xs)
    }

    private val locale = resources.configuration.locales[0]
    private val months = resources.getStringArray(R.array.profile_months).map { it.uppercase(locale) }
    private val weekdays = resources.getStringArray(R.array.profile_weekdays)
    private val labelWidth = ceil(weekdays.maxOf { labelPaint.measureText(it) })
    private val cellsLeft = labelWidth + labelGap

    private val rect = RectF()

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        val weeks = layout?.weeks ?: 0
        val width = cellsLeft + weeks * pitch - gap
        val height = monthRow + 7 * pitch - gap
        setMeasuredDimension(ceil(width).toInt(), ceil(height).toInt())
    }

    override fun onDraw(canvas: Canvas) {
        val l = layout ?: return
        val baseline = -labelPaint.ascent()
        // The web's months row is 14px tall with its label centred in a 14px line.
        val monthBaseline = (14 * density - (labelPaint.descent() - labelPaint.ascent())) / 2 + baseline
        l.monthColumns.forEachIndexed { m, col ->
            canvas.drawText(months[m], cellsLeft + col * pitch, monthBaseline, labelPaint)
        }
        // Rows 1, 3 and 5 are Monday, Wednesday and Friday.
        weekdays.forEachIndexed { i, label ->
            val row = 1 + 2 * i
            val top = monthRow + row * pitch
            canvas.drawText(label, 0f, top + (cell - (labelPaint.descent() - labelPaint.ascent())) / 2 + baseline, labelPaint)
        }
        for (col in 0 until l.weeks) {
            for (row in 0..6) {
                val level = l.levels[col * 7 + row]
                if (level < 0) continue
                cellRect(col, row)
                canvas.drawRoundRect(rect, radius, radius, levelPaints[level])
            }
        }
        val sel = selected ?: return
        if (sel.year != l.year) return
        cellRect(l.columnOf(sel), sel.dayOfWeek.value % 7)
        rect.inset(-ring / 2, -ring / 2)
        canvas.drawRoundRect(rect, radius + ring / 2, radius + ring / 2, ringPaint)
    }

    private fun cellRect(col: Int, row: Int) {
        val left = cellsLeft + col * pitch
        val top = monthRow + row * pitch
        rect.set(left, top, left + cell, top + cell)
    }

    /** The column [day] sits in, in px from this view's left, for scrolling it into sight. */
    fun xOf(day: LocalDate): Int {
        val l = layout ?: return 0
        return (cellsLeft + l.columnOf(day) * pitch).roundToInt()
    }

    override fun onTouchEvent(event: MotionEvent): Boolean {
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> return true
            MotionEvent.ACTION_UP -> {
                val l = layout ?: return true
                // A tap in a gap counts for the cell it follows, so the whole grid is a target.
                val col = floor((event.x - cellsLeft) / pitch).toInt()
                val row = floor((event.y - monthRow) / pitch).toInt()
                val day = if (event.x >= cellsLeft && event.y >= monthRow) l.dayAt(col, row) else null
                onDayTap?.invoke(if (day == selected) null else day)
                performClick()
                return true
            }
        }
        return super.onTouchEvent(event)
    }

    override fun performClick(): Boolean = super.performClick()
}