package dev.holdmytrack.android.export

import kotlin.math.abs
import kotlin.math.ln
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

/**
 * The export viewfinder's geometry and the image's sizes (`apps/android/docs/SPEC.md` FR-2.11),
 * as plain values so they're unit-tested apart from the view that draws them
 * ([ExportViewfinderView]). The web's `map/exportViewfinder.ts`, the same rules: every rect is
 * in the map view's pixels.
 *
 * The window sits fixed in the middle of the map and the map moves under it. Dragging a corner
 * crops; on release the window settles back to the largest size the area holds and the map
 * zooms by the same factor, so the cropped area fills it again ([settle]). The window only sets
 * what's in the picture and its shape; how many pixels the picture has is chosen apart from it,
 * so a phone's screen makes as big an image as any.
 */
data class Box(val x: Float, val y: Float, val w: Float, val h: Float) {
    val centerX: Float get() = x + w / 2
    val centerY: Float get() = y + h / 2
}

enum class Corner { NW, NE, SW, SE }

/** The room the window keeps clear in a map view, in its pixels: [top] for the toolbar,
 *  [bottom] for the image's size under it, [side] at each side, and [minWindow], the smallest a
 *  corner can crop it to. */
data class ViewfinderMargins(val top: Float, val bottom: Float, val side: Float, val minWindow: Float)

object Viewfinder {

    /** The part of a [width] × [height] map the window may take. */
    fun area(width: Float, height: Float, m: ViewfinderMargins): Box = Box(
        m.side,
        m.top,
        max(m.minWindow, width - 2 * m.side),
        max(m.minWindow, height - m.top - m.bottom),
    )

    /** The largest window of [aspect] (width ÷ height) that fits [area], centred in it; with no
     *  aspect, the whole area. */
    fun fit(aspect: Float?, area: Box): Box {
        if (aspect == null) return area
        val w = if (area.w / area.h >= aspect) area.h * aspect else area.w
        val h = w / aspect
        return Box(area.x + (area.w - w) / 2, area.y + (area.h - h) / 2, w, h)
    }

    /**
     * The window while [corner] is dragged by ([dx], [dy]) from [start]: the opposite corner stays
     * put, the size stays within [area] and at least [minWindow], and with an [aspect] the shape
     * holds, scaled by whichever axis the finger has moved further.
     */
    fun resize(start: Box, corner: Corner, dx: Float, dy: Float, aspect: Float?, area: Box, minWindow: Float): Box {
        val sx = if (corner == Corner.NE || corner == Corner.SE) 1 else -1
        val sy = if (corner == Corner.SW || corner == Corner.SE) 1 else -1
        val ax = if (sx > 0) start.x else start.x + start.w
        val ay = if (sy > 0) start.y else start.y + start.h
        val maxW = if (sx > 0) area.x + area.w - ax else ax - area.x
        val maxH = if (sy > 0) area.y + area.h - ay else ay - area.y
        var w = start.w + sx * dx
        var h = start.h + sy * dy
        if (aspect == null) {
            w = w.coerceIn(minWindow, max(minWindow, maxW))
            h = h.coerceIn(minWindow, max(minWindow, maxH))
        } else {
            val kx = w / start.w
            val ky = h / start.h
            val scale = if (abs(kx - 1) >= abs(ky - 1)) kx else ky
            w = start.w * scale
            h = w / aspect
            val shrink = min(min(maxW / w, maxH / h), 1f)
            w *= shrink
            h *= shrink
            val grow = max(max(minWindow / w, minWindow / h), 1f)
            w *= grow
            h *= grow
        }
        return Box(if (sx > 0) ax else ax - w, if (sy > 0) ay else ay - h, w, h)
    }

    /** Where a cropped window goes on release: [window], the largest of the crop's shape in
     *  [area]; [zoomDelta], the zoom change that makes the crop fill it; and the crop's middle,
     *  in today's pixels, which has to end up in the window's. */
    data class Settled(val window: Box, val zoomDelta: Double, val focusX: Float, val focusY: Float)

    fun settle(crop: Box, area: Box): Settled {
        val window = fit(crop.w / crop.h, area)
        return Settled(window, ln((window.w / crop.w).toDouble()) / ln(2.0), crop.centerX, crop.centerY)
    }

    /**
     * The map point, in today's pixels of a [mapW] × [mapH] map, that has to become the map's
     * middle for [settled]'s crop middle to land in its window's: the window's offset from the
     * map's middle, scaled back by the zoom still to come. MapLibre eases to a middle, and the
     * window's isn't the map's, since room is kept above it for the toolbar.
     */
    fun settledCenter(settled: Settled, mapW: Float, mapH: Float): Pair<Float, Float> {
        val scale = Math.pow(2.0, settled.zoomDelta).toFloat()
        return (settled.focusX + (mapW / 2 - settled.window.centerX) / scale) to (settled.focusY + (mapH / 2 - settled.window.centerY) / scale)
    }

    /** Whether a settle would change anything visible: a drag that ended within a pixel of where
     *  it began leaves the map where it is. */
    fun moved(a: Box, b: Box): Boolean =
        abs(a.x - b.x) > 0.5f || abs(a.y - b.y) > 0.5f || abs(a.w - b.w) > 0.5f || abs(a.h - b.h) > 0.5f
}

/** A Custom image's sizes, by its long side; the short side follows the window's shape. */
enum class CustomSize(val label: String, val longSidePx: Int) {
    HD("HD", 1920),
    TWO_K("2K", 2560),
    FOUR_K("4K", 3840),
}

/** One platform's image size, from the same guide as the web's `exportPresets.ts`. */
data class ExportPreset(val id: String, val platform: String, val row: PresetRow, val widthPx: Int, val heightPx: Int) {
    val aspect: Float get() = widthPx.toFloat() / heightPx
}

enum class PresetRow { SQUARE, PORTRAIT, LANDSCAPE, STORY }

/** The web's `EXPORT_PRESETS`, in the same order: by platform, then shape. */
val EXPORT_PRESETS = listOf(
    ExportPreset("instagram-square", "Instagram", PresetRow.SQUARE, 1080, 1080),
    ExportPreset("instagram-portrait", "Instagram", PresetRow.PORTRAIT, 1080, 1350),
    ExportPreset("instagram-landscape", "Instagram", PresetRow.LANDSCAPE, 1080, 566),
    ExportPreset("instagram-story", "Instagram", PresetRow.STORY, 1080, 1920),
    ExportPreset("facebook-square", "Facebook", PresetRow.SQUARE, 1080, 1080),
    ExportPreset("facebook-portrait", "Facebook", PresetRow.PORTRAIT, 1080, 1359),
    ExportPreset("facebook-landscape", "Facebook", PresetRow.LANDSCAPE, 1080, 566),
    ExportPreset("facebook-story", "Facebook", PresetRow.STORY, 1080, 1920),
    ExportPreset("x-square", "X", PresetRow.SQUARE, 1080, 1080),
    ExportPreset("x-landscape", "X", PresetRow.LANDSCAPE, 1280, 720),
)

/** One entry of the viewfinder's one menu: Custom at a size (any shape, its long side fixed),
 *  or a platform preset (its own shape and size). */
sealed interface ExportChoice {
    data class Custom(val size: CustomSize) : ExportChoice
    data class Preset(val preset: ExportPreset) : ExportChoice

    /** The window's shape: free for Custom. */
    val aspect: Float? get() = (this as? Preset)?.preset?.aspect

    companion object {
        val DEFAULT: ExportChoice = Custom(CustomSize.HD)

        /** Every entry, in the menu's order: Custom at each size, then the presets. */
        val ALL: List<ExportChoice> = CustomSize.entries.map(::Custom) + EXPORT_PRESETS.map(::Preset)
    }
}

/** The image's pixel size for [choice] through a window [windowW] × [windowH]. */
fun outputSize(choice: ExportChoice, windowW: Float, windowH: Float): Pair<Int, Int> = when (choice) {
    is ExportChoice.Preset -> choice.preset.widthPx to choice.preset.heightPx
    is ExportChoice.Custom -> {
        val aspect = windowW / windowH
        val long = choice.size.longSidePx
        if (aspect >= 1) long to max(1, (long / aspect).roundToInt()) else max(1, (long * aspect).roundToInt()) to long
    }
}
