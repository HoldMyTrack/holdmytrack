package dev.holdmytrack.android.export

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.RectF
import android.graphics.Typeface
import androidx.core.content.res.ResourcesCompat
import androidx.core.graphics.toColorInt
import dev.holdmytrack.android.R
import kotlin.math.max
import kotlin.math.min

/**
 * What every exported image carries in its bottom corners, the web export's `drawAttribution` and
 * `drawWatermark` (`apps/web/src/map/exportMap.ts`) at the same proportions: the basemap's credit
 * on the right over a dark plate — an ODbL requirement on any image the map is in, not a choice —
 * and the HoldMyTrack logo and wordmark on the left, with no plate, at partial opacity.
 */
object ImageMarks {

    /** Font size as a fraction of the image's shorter side, clamped, so a 566 px landscape and a
     *  3840 px Custom both read the line at a sensible size. */
    private const val FONT_FRACTION = 0.016f
    private const val MIN_FONT_PX = 12f
    private const val MAX_FONT_PX = 28f
    private const val MARGIN_FRACTION = 0.014f
    private const val WATERMARK_FONT_SCALE = 1.15f
    private const val WATERMARK_LOGO_SCALE = 1.7f
    private const val WATERMARK_ALPHA = 0.7f

    /** The credit line's font size and its distance from the corners, for an image [width] × [height]. */
    fun stripMetrics(width: Int, height: Int): Pair<Float, Float> {
        val short = min(width, height).toFloat()
        val font = (short * FONT_FRACTION).coerceIn(MIN_FONT_PX, MAX_FONT_PX)
        return font to max(font * 0.5f, short * MARGIN_FRACTION)
    }

    /** The credit as plain text: the basemap's, then the imagery's when the image shows it, each
     *  a source's own HTML attribution with its tags taken out and its entities read. */
    fun creditText(basemap: String?, satellite: String?): String =
        listOfNotNull(basemap, satellite)
            .map { decodeEntities(it.replace(Regex("<[^>]+>"), "")).trim() }
            .filter { it.isNotEmpty() }
            .joinToString(" · ")

    private val NAMED_ENTITIES = mapOf("copy" to "©", "amp" to "&", "lt" to "<", "gt" to ">", "quot" to "\"", "apos" to "'", "nbsp" to " ")

    private fun decodeEntities(text: String): String =
        Regex("&(#x?[0-9a-fA-F]+|[a-z]+);").replace(text) { match ->
            val name = match.groupValues[1]
            when {
                name.startsWith("#x") -> name.drop(2).toIntOrNull(16)?.let { String(Character.toChars(it)) }
                name.startsWith("#") -> name.drop(1).toIntOrNull()?.let { String(Character.toChars(it)) }
                else -> NAMED_ENTITIES[name]
            } ?: match.value
        }

    /** Draws both marks onto [bitmap] in place. [dark]: the map under them reads dark — the
     *  dark flavor or satellite imagery — so the wordmark takes its light colors. */
    fun draw(context: Context, bitmap: Bitmap, credit: String, dark: Boolean) {
        val canvas = Canvas(bitmap)
        val (fontPx, margin) = stripMetrics(bitmap.width, bitmap.height)
        val bottom = drawCredit(canvas, bitmap.width, bitmap.height, credit, fontPx, margin)
        drawWatermark(context, canvas, bottom, fontPx, margin, dark)
    }

    private fun drawCredit(canvas: Canvas, width: Int, height: Int, text: String, fontPx: Float, margin: Float): Float {
        val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            textSize = fontPx
            color = Color.WHITE
            textAlign = Paint.Align.RIGHT
        }
        val padX = fontPx * 0.5f
        val padY = fontPx * 0.3f
        val textWidth = paint.measureText(text)
        val ascent = -paint.fontMetrics.ascent * 0.8f
        val descent = paint.fontMetrics.descent * 0.8f
        val baselineX = width - margin
        val baselineY = height - margin
        val plate = Paint().apply { color = Color.argb(140, 0, 0, 0) }
        canvas.drawRect(
            RectF(baselineX - textWidth - padX * 2, baselineY - ascent - padY, baselineX, baselineY + descent + padY),
            plate,
        )
        canvas.drawText(text, baselineX - padX, baselineY, paint)
        return baselineY + descent + padY
    }

    private fun drawWatermark(context: Context, canvas: Canvas, bottomY: Float, creditFontPx: Float, margin: Float, dark: Boolean) {
        val fontPx = creditFontPx * WATERMARK_FONT_SCALE
        val logoH = fontPx * WATERMARK_LOGO_SCALE
        val gap = fontPx * 0.35f
        val serif = ResourcesCompat.getFont(context, R.font.fraunces) ?: Typeface.SERIF
        val alpha = (WATERMARK_ALPHA * 255).toInt()
        val light = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            typeface = Typeface.create(serif, 400, false)
            textSize = fontPx
            color = if (dark) "#e6ebe8".toColorInt() else "#4a5750".toColorInt()
            this.alpha = alpha
        }
        val bold = Paint(light).apply {
            typeface = Typeface.create(serif, 800, false)
            // The header's "Track" amber, lightened on a dark map so it still reads there.
            color = if (dark) "#e0a84a".toColorInt() else "#9a6b1e".toColorInt()
            this.alpha = alpha
        }
        val midY = bottomY - max(logoH, fontPx) / 2
        var x = margin
        BitmapFactory.decodeResource(context.resources, if (dark) R.drawable.export_logo_on_dark else R.drawable.export_logo_on_light)?.let { logo ->
            val logoW = logo.width.toFloat() / logo.height * logoH
            val paint = Paint(Paint.ANTI_ALIAS_FLAG or Paint.FILTER_BITMAP_FLAG).apply { this.alpha = alpha }
            canvas.drawBitmap(logo, null, RectF(x, midY - logoH / 2, x + logoW, midY + logoH / 2), paint)
            logo.recycle()
            x += logoW + gap
        }
        val baseline = midY - (light.fontMetrics.ascent + light.fontMetrics.descent) / 2
        canvas.drawText("HoldMy", x, baseline, light)
        canvas.drawText("Track", x + light.measureText("HoldMy"), baseline, bold)
    }
}
