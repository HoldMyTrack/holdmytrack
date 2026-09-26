package dev.holdmytrack.android.recording

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.util.AttributeSet
import android.view.View
import dev.holdmytrack.android.map.MapOverlays
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.max

/**
 * A recording's route drawn as a bare line, no basemap — `RecordedActivitiesActivity`'s
 * per-row preview. Enough to tell two recordings apart at a glance, which the name, type and
 * distance alone often aren't (the same walk, a week apart).
 *
 * Longitude is scaled by cos(latitude) at the track's middle, an equirectangular projection
 * local to the track: at a few kilometres across this is indistinguishable from Web Mercator,
 * and without it a route far from the equator would draw stretched east–west. The track is
 * fitted to the view keeping that aspect ratio, and centred along the shorter side.
 */
class TrackSilhouetteView(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {

    private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = 2f * resources.displayMetrics.density
        strokeJoin = Paint.Join.ROUND
        strokeCap = Paint.Cap.ROUND
        // The colour the map draws tracks in, so the preview reads as the same line.
        color = Color.parseColor(MapOverlays.TRACK_COLOR)
    }

    private val path = Path()
    private var points: List<RecordedPoint> = emptyList()

    fun setPoints(value: List<RecordedPoint>) {
        points = value
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        if (points.size < 2) return

        val minLat = points.minOf { it.lat }
        val maxLat = points.maxOf { it.lat }
        val minLon = points.minOf { it.lon }
        val maxLon = points.maxOf { it.lon }
        val xScale = cos((minLat + maxLat) / 2 * PI / 180)
        val spanX = (maxLon - minLon) * xScale
        val spanY = maxLat - minLat

        val inset = paint.strokeWidth
        val width = width - paddingLeft - paddingRight - 2 * inset
        val height = height - paddingTop - paddingBottom - 2 * inset
        // A track that never moved has zero span on both axes; any positive divisor then just
        // stacks every point in the centre, which is the honest drawing of it.
        val scale = minOf(width / max(spanX, 1e-9), height / max(spanY, 1e-9))
        val offsetX = paddingLeft + inset + (width - spanX * scale) / 2
        val offsetY = paddingTop + inset + (height - spanY * scale) / 2

        path.reset()
        points.forEachIndexed { i, point ->
            val x = (offsetX + (point.lon - minLon) * xScale * scale).toFloat()
            // Screen y grows downward, latitude upward.
            val y = (offsetY + (maxLat - point.lat) * scale).toFloat()
            if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
        }
        canvas.drawPath(path, paint)
    }
}
