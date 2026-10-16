package dev.holdmytrack.android.export

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The export viewfinder and its sizes (`apps/android/docs/SPEC.md` FR-2.11). */
class ViewfinderTest {

    private val margins = ViewfinderMargins(top = 64f, bottom = 44f, side = 16f, minWindow = 64f)
    private val area = Viewfinder.area(1032f, 708f, margins)

    @Test
    fun `the area keeps room for the toolbar above and the size below`() {
        assertEquals(Box(16f, 64f, 1000f, 600f), area)
    }

    @Test
    fun `a window fits the area in its shape, centred`() {
        assertEquals(Box(216f, 64f, 600f, 600f), Viewfinder.fit(1f, area))
        assertEquals(Box(16f, 239f, 1000f, 250f), Viewfinder.fit(4f, area))
        assertEquals(area, Viewfinder.fit(null, area))
    }

    @Test
    fun `dragging a corner keeps the opposite one where it was`() {
        assertEquals(Box(16f, 64f, 800f, 500f), Viewfinder.resize(area, Corner.SE, -200f, -100f, null, area, 64f))
        assertEquals(Box(316f, 164f, 700f, 500f), Viewfinder.resize(area, Corner.NW, 300f, 100f, null, area, 64f))
    }

    @Test
    fun `a corner never crops past the area or below the smallest window`() {
        assertEquals(area, Viewfinder.resize(area, Corner.SE, 500f, 500f, null, area, 64f))
        val tiny = Viewfinder.resize(area, Corner.SE, -5000f, -5000f, null, area, 64f)
        assertEquals(64f, tiny.w)
        assertEquals(64f, tiny.h)
    }

    @Test
    fun `a preset's shape holds while its corner is dragged`() {
        val start = Viewfinder.fit(1f, area)
        val r = Viewfinder.resize(start, Corner.NE, -300f, 0f, 1f, area, 64f)
        assertEquals(r.w, r.h)
        assertEquals(300f, r.w)
        assertEquals(start.x, r.x)
        assertEquals(start.y + start.h, r.y + r.h)
    }

    @Test
    fun `a crop settles back to the full window and zooms in so the crop fills it`() {
        val settled = Viewfinder.settle(Box(16f, 64f, 500f, 300f), area)
        assertEquals(area, settled.window)
        assertEquals(1.0, settled.zoomDelta, 1e-9)
        assertEquals(266f, settled.focusX)
        assertEquals(214f, settled.focusY)
    }

    @Test
    fun `a crop of a new shape settles to the largest window of that shape`() {
        val settled = Viewfinder.settle(Box(100f, 100f, 300f, 300f), area)
        assertEquals(Box(216f, 64f, 600f, 600f), settled.window)
        assertEquals(1.0, settled.zoomDelta, 1e-9)
    }

    @Test
    fun `a drag that changed nothing leaves the map alone`() {
        assertFalse(Viewfinder.moved(area, area.copy(w = area.w + 0.2f)))
        assertTrue(Viewfinder.moved(area, area.copy(w = area.w - 10f)))
    }

    @Test
    fun `a Custom image's long side is the size picked, its short side the window's shape`() {
        assertEquals(1920 to 1080, outputSize(ExportChoice.Custom(CustomSize.HD), 1600f, 900f))
        assertEquals(2160 to 3840, outputSize(ExportChoice.Custom(CustomSize.FOUR_K), 900f, 1600f))
        assertEquals(2560 to 2560, outputSize(ExportChoice.Custom(CustomSize.TWO_K), 500f, 500f))
    }

    @Test
    fun `a preset makes its own size whatever the window's shape`() {
        val story = EXPORT_PRESETS.first { it.id == "instagram-story" }
        assertEquals(1080 to 1920, outputSize(ExportChoice.Preset(story), 300f, 300f))
        assertEquals(1080f / 1920f, ExportChoice.Preset(story).aspect!!, 1e-6f)
        assertNull(ExportChoice.Custom(CustomSize.HD).aspect)
    }

    @Test
    fun `the menu lists Custom at each size, then every preset, Custom HD first`() {
        assertEquals(ExportChoice.DEFAULT, ExportChoice.ALL.first())
        assertEquals(CustomSize.entries.size + EXPORT_PRESETS.size, ExportChoice.ALL.size)
    }

    @Test
    fun `the map eases to the point that puts the crop's middle in the window's`() {
        // Window centred 20 px above the map's middle; a crop that doubles to fill it.
        val settled = Viewfinder.Settled(Box(0f, 0f, 400f, 360f), 1.0, 100f, 100f)
        val (cx, cy) = Viewfinder.settledCenter(settled, 400f, 400f)
        assertEquals(100f, cx, 1e-4f)
        assertEquals(110f, cy, 1e-4f)
    }
}
