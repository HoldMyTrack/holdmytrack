package dev.holdmytrack.android.profile

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import android.util.AttributeSet
import android.view.View
import dev.holdmytrack.android.R

/**
 * The You tab's glance at the activity graph: [ProfileStats.recentWeeks]' cells, a week to a
 * column, in the year grid's shades (`profile_level_0`–`3`) — small, and not to be read day by
 * day; the whole graph is a tap away. A blank (-1) cell isn't drawn.
 */
class RecentWeeksView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private val density = resources.displayMetrics.density
    private val cell = 6 * density
    private val gap = 2 * density
    private val radius = 1.5f * density
    private val shades = intArrayOf(
        context.getColorStateList(R.color.profile_level_0).defaultColor,
        context.getColorStateList(R.color.profile_level_1).defaultColor,
        context.getColorStateList(R.color.profile_level_2).defaultColor,
        context.getColorStateList(R.color.profile_level_3).defaultColor,
    )
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val rect = RectF()
    private var levels = IntArray(0)

    fun setLevels(levels: IntArray) {
        this.levels = levels
        requestLayout()
        invalidate()
    }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        val weeks = levels.size / 7
        val width = paddingLeft + paddingRight + (weeks * (cell + gap) - gap).coerceAtLeast(0f)
        val height = paddingTop + paddingBottom + 7 * (cell + gap) - gap
        setMeasuredDimension(resolveSize(width.toInt(), widthMeasureSpec), resolveSize(height.toInt(), heightMeasureSpec))
    }

    override fun onDraw(canvas: Canvas) {
        for (i in levels.indices) {
            val level = levels[i]
            if (level < 0) continue
            val x = paddingLeft + (i / 7) * (cell + gap)
            val y = paddingTop + (i % 7) * (cell + gap)
            rect.set(x, y, x + cell, y + cell)
            paint.color = shades[level.coerceIn(0, 3)]
            canvas.drawRoundRect(rect, radius, radius, paint)
        }
    }
}
