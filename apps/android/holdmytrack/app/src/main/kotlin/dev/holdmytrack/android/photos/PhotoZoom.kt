package dev.holdmytrack.android.photos

import kotlin.math.max
import kotlin.math.min

/**
 * The photo viewer's zoom and pan (`map/PhotoViewer`, `apps/android/docs/SPEC.md` FR-2.10), the web's
 * `ui/photoZoom.ts`: the picture fitted to the screen and centered is scale 1 at (0, 0); [View.scale]
 * multiplies that and [View.x]/[View.y] move its center, in pixels from the screen's center. Plain
 * math, no views, so it's what the unit tests exercise.
 */
object PhotoZoom {

    data class View(val scale: Float, val x: Float, val y: Float)

    data class Size(val width: Float, val height: Float)

    val FIT = View(1f, 0f, 0f)

    /** Past this the 2048 px stored copy is only blur on a phone. */
    const val MAX_SCALE = 8f

    /** One tap of Zoom in or Zoom out. */
    const val STEP = 1.5f

    /** Where a double tap on the fitted picture zooms to. */
    const val DOUBLE_TAP_SCALE = 3f

    /** The picture's size at scale 1: as large as fits the stage, never larger than it is. */
    fun fitSize(image: Size, stage: Size): Size {
        if (image.width <= 0f || image.height <= 0f) return Size(0f, 0f)
        val ratio = min(min(stage.width / image.width, stage.height / image.height), 1f)
        return Size(image.width * ratio, image.height * ratio)
    }

    /** No smaller than fitted, no larger than [MAX_SCALE], and an edge never pulled in past the
     *  stage's edge — a side narrower than the stage stays centered. */
    fun clamp(view: View, fitted: Size, stage: Size): View {
        val scale = view.scale.coerceIn(1f, MAX_SCALE)
        val maxX = max(0f, (fitted.width * scale - stage.width) / 2f)
        val maxY = max(0f, (fitted.height * scale - stage.height) / 2f)
        // + 0f turns a -0 into 0, so a view that hasn't moved equals FIT.
        return View(scale, view.x.coerceIn(-maxX, maxX) + 0f, view.y.coerceIn(-maxY, maxY) + 0f)
    }

    /** Scales by [factor] about ([px], [py]) from the stage's center, so what's under the
     *  fingers stays there. */
    fun zoomAt(view: View, factor: Float, px: Float, py: Float, fitted: Size, stage: Size): View {
        val scale = (view.scale * factor).coerceIn(1f, MAX_SCALE)
        val k = scale / view.scale
        return clamp(View(scale, px - (px - view.x) * k, py - (py - view.y) * k), fitted, stage)
    }

    fun panBy(view: View, dx: Float, dy: Float, fitted: Size, stage: Size): View =
        clamp(view.copy(x = view.x + dx, y = view.y + dy), fitted, stage)
}
