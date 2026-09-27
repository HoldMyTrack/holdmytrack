package dev.holdmytrack.android.panel

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RectF
import android.util.AttributeSet
import android.view.MotionEvent
import android.view.View
import dev.holdmytrack.android.R
import dev.holdmytrack.android.map.BandMetric
import dev.holdmytrack.android.map.TrackBands
import dev.holdmytrack.android.net.TrackMetricPoint
import kotlin.math.abs

/**
 * The selected activity's profile, the web's `apps/web/src/ui/TrackProfile.tsx`
 * (`docs/SPEC.md` FR-4.9): an 8dp strip of its pace or heart-rate bands, each run as wide as
 * the distance it covers, over a 28dp elevation curve — filled in the accent at 15%, outlined
 * in ink at 45% — which isn't drawn at all for an activity without elevation. Where the web
 * reads a point on hover, this reads it under a touch: pressing and dragging reports the
 * nearest point to [onScrub], and letting go reports null.
 */
class TrackProfileView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    var onScrub: ((TrackMetricPoint?) -> Unit)? = null

    private var points: List<TrackMetricPoint> = emptyList()
    private var metric = BandMetric.SPEED
    private var elevationAvailable = false
    private var scrubbed: Int? = null

    private val density = resources.displayMetrics.density
    private val stripHeight = 8 * density
    private val chartHeight = 28 * density
    private val chartGap = 2 * density
    private val stripRadius = 2 * density

    private val bandPaint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val fillPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.FILL
        color = withAlpha(context.getColor(R.color.hmt_accent), 0.15f)
    }
    private val linePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = density
        color = withAlpha(context.getColor(R.color.hmt_ink), 0.45f)
    }
    private val markerPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = 1.5f * density
        color = context.getColor(R.color.hmt_ink)
    }
    private val clip = Path()
    private val strip = RectF()

    fun set(points: List<TrackMetricPoint>, metric: BandMetric, elevationAvailable: Boolean) {
        val shapeChanged = this.elevationAvailable != elevationAvailable
        this.points = points
        this.metric = metric
        this.elevationAvailable = elevationAvailable
        scrubbed = null
        if (shapeChanged) requestLayout()
        invalidate()
    }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        // At least the view's minimum height, so a pace-only strip is still a touch target.
        val height = maxOf(stripHeight + if (elevationAvailable) chartGap + chartHeight else 0f, suggestedMinimumHeight.toFloat())
        setMeasuredDimension(
            getDefaultSize(suggestedMinimumWidth, widthMeasureSpec),
            resolveSize(kotlin.math.ceil(height).toInt(), heightMeasureSpec),
        )
    }

    private val total: Double
        get() = points.lastOrNull()?.distanceM ?: 0.0

    override fun onDraw(canvas: Canvas) {
        if (points.size < 2 || total <= 0) return
        val w = width.toFloat()
        val runs = TrackBands.runs(points, metric, TrackBands.scale(points, metric))

        // The strip, its ends rounded, each run as wide as its share of the distance.
        strip.set(0f, 0f, w, stripHeight)
        clip.reset()
        clip.addRoundRect(strip, stripRadius, stripRadius, Path.Direction.CW)
        canvas.save()
        canvas.clipPath(clip)
        for (run in runs) {
            val left = (points[run.startIndex].distanceM / total * w).toFloat()
            val right = (points[run.endIndex].distanceM / total * w).toFloat()
            bandPaint.color = Color.parseColor(TrackBands.COLORS[run.band])
            canvas.drawRect(left, 0f, right, stripHeight, bandPaint)
        }
        canvas.restore()

        if (elevationAvailable) drawElevation(canvas, w)

        scrubbed?.let { i ->
            val x = (points[i].distanceM / total * w).toFloat()
            canvas.drawLine(x, 0f, x, height.toFloat(), markerPaint)
        }
    }

    private fun drawElevation(canvas: Canvas, w: Float) {
        val elevations = points.mapNotNull { it.elevationM }
        if (elevations.size != points.size) return
        val min = elevations.min()
        val span = (elevations.max() - min).takeIf { it != 0.0 } ?: 1.0
        val top = stripHeight + chartGap
        // The web's y = height − (e − min) / span × (height − 4) − 2, in its 32-unit box.
        val inset = chartHeight / 16
        fun y(e: Double) = (top + chartHeight - (e - min) / span * (chartHeight - 2 * inset) - inset).toFloat()
        val line = Path()
        val area = Path().apply { moveTo(0f, top + chartHeight) }
        points.forEachIndexed { i, p ->
            val x = (p.distanceM / total * w).toFloat()
            val yy = y(p.elevationM!!)
            if (i == 0) line.moveTo(x, yy) else line.lineTo(x, yy)
            area.lineTo(x, yy)
        }
        area.lineTo(w, top + chartHeight)
        area.close()
        canvas.drawPath(area, fillPaint)
        canvas.drawPath(line, linePaint)
    }

    @SuppressLint("ClickableViewAccessibility") // Reading a point is a gesture; TalkBack gets the description.
    override fun onTouchEvent(event: MotionEvent): Boolean {
        if (points.size < 2 || total <= 0) return false
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN, MotionEvent.ACTION_MOVE -> {
                parent?.requestDisallowInterceptTouchEvent(true)
                val target = (event.x / width).coerceIn(0f, 1f) * total
                val nearest = points.indices.minBy { abs(points[it].distanceM - target) }
                if (nearest != scrubbed) {
                    scrubbed = nearest
                    invalidate()
                    onScrub?.invoke(points[nearest])
                }
                return true
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                scrubbed = null
                invalidate()
                onScrub?.invoke(null)
                return true
            }
        }
        return super.onTouchEvent(event)
    }

    private fun withAlpha(color: Int, alpha: Float) =
        Color.argb((alpha * 255).toInt(), Color.red(color), Color.green(color), Color.blue(color))
}
