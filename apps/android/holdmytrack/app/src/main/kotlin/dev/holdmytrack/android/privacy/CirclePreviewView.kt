package dev.holdmytrack.android.privacy

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RectF
import android.util.AttributeSet
import android.view.View
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.PrivateLocation
import kotlin.math.ln
import kotlin.math.min

/**
 * A Private location as a picture rather than a map (the Privacy screen's rows): a tile with a
 * couple of streets across it and the circle in the map's purple, larger the larger its radius —
 * on a log scale from [PrivateLocation.MIN_RADIUS_M] to [PrivateLocation.MAX_RADIUS_M], so 50 m
 * and 2 km both read.
 */
class CirclePreviewView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    var radiusM: Int = PrivateLocation.MIN_RADIUS_M
        set(value) {
            field = value
            invalidate()
        }

    private val density = resources.displayMetrics.density
    private val corner = 12 * density
    private val purple = context.getColor(R.color.hmt_private)
    private val tile = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = context.getColor(R.color.hmt_surface_tint_strong) }
    private val street = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = context.getColor(R.color.hmt_surface)
        style = Paint.Style.STROKE
        strokeWidth = 3 * density
    }
    private val fill = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = purple; alpha = 56 }
    private val ring = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = purple
        style = Paint.Style.STROKE
        strokeWidth = 1.5f * density
    }
    private val dot = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = purple }
    private val rect = RectF()
    private val path = Path()

    override fun onDraw(canvas: Canvas) {
        val w = width.toFloat()
        val h = height.toFloat()
        rect.set(0f, 0f, w, h)
        canvas.save()
        path.reset()
        path.addRoundRect(rect, corner, corner, Path.Direction.CW)
        canvas.clipPath(path)
        canvas.drawRect(rect, tile)
        canvas.drawLine(0f, h * 0.6f, w, h * 0.5f, street)
        canvas.drawLine(w * 0.35f, 0f, w * 0.5f, h, street)
        val span = ln(PrivateLocation.MAX_RADIUS_M.toFloat() / PrivateLocation.MIN_RADIUS_M)
        val share = (ln(radiusM.coerceIn(PrivateLocation.MIN_RADIUS_M, PrivateLocation.MAX_RADIUS_M).toFloat() / PrivateLocation.MIN_RADIUS_M) / span)
        val r = 5 * density + share * (min(w, h) / 2 - 7 * density)
        canvas.drawCircle(w / 2, h / 2, r, fill)
        canvas.drawCircle(w / 2, h / 2, r, ring)
        canvas.drawCircle(w / 2, h / 2, 2.5f * density, dot)
        canvas.restore()
    }
}
