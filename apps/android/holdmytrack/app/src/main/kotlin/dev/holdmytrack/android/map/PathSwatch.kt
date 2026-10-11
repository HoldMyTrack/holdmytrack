package dev.holdmytrack.android.map

import android.graphics.Canvas
import android.graphics.ColorFilter
import android.graphics.DashPathEffect
import android.graphics.Paint
import android.graphics.PixelFormat
import android.graphics.drawable.Drawable
import androidx.core.graphics.toColorInt

/**
 * A short sample of a kind of path, in the color and dash the map draws it with: the Layers
 * menu's legend ([LayersMenu]), the web's `PathSwatch` in `ui/OverlaysMenu.tsx`. [dash] is in
 * line widths, as MapLibre's `line-dasharray` takes it, or null for a solid line.
 */
class PathSwatch(color: String, private val dash: FloatArray?, density: Float) : Drawable() {

    private val stroke = SWATCH_WIDTH_DP * density
    private val width = (24 * density).toInt()
    private val height = (8 * density).toInt()

    private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        this.color = color.toColorInt()
        style = Paint.Style.STROKE
        strokeWidth = stroke
        strokeCap = if (dash == null) Paint.Cap.ROUND else Paint.Cap.BUTT
        if (dash != null) pathEffect = DashPathEffect(dash.map { it * stroke }.toFloatArray(), 0f)
    }

    override fun draw(canvas: Canvas) {
        val y = bounds.exactCenterY()
        canvas.drawLine(bounds.left + stroke, y, bounds.right - stroke, y, paint)
    }

    override fun getIntrinsicWidth() = width
    override fun getIntrinsicHeight() = height
    override fun setAlpha(alpha: Int) { paint.alpha = alpha }
    override fun setColorFilter(colorFilter: ColorFilter?) { paint.colorFilter = colorFilter }
    @Deprecated("Deprecated in Java")
    override fun getOpacity() = PixelFormat.TRANSLUCENT

    private companion object {
        /** How wide the legend draws a line; the dash scales with it, as on the map. */
        const val SWATCH_WIDTH_DP = 3f
    }
}
