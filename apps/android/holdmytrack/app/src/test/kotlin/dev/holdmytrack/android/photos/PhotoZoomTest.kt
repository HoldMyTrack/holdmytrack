package dev.holdmytrack.android.photos

import dev.holdmytrack.android.photos.PhotoZoom.Size
import dev.holdmytrack.android.photos.PhotoZoom.View
import org.junit.Assert.assertEquals
import org.junit.Test

class PhotoZoomTest {

    private val stage = Size(1000f, 800f)

    @Test
    fun fitsOnTheTighterSideAndNeverBlowsUp() {
        assertEquals(Size(1000f, 750f), PhotoZoom.fitSize(Size(2048f, 1536f), stage))
        assertEquals(Size(600f, 800f), PhotoZoom.fitSize(Size(1536f, 2048f), stage))
        assertEquals(Size(400f, 300f), PhotoZoom.fitSize(Size(400f, 300f), stage))
    }

    @Test
    fun zoomingAboutAPointKeepsItUnderTheFingers() {
        val fitted = Size(1000f, 750f)
        val view = PhotoZoom.zoomAt(PhotoZoom.FIT, 2f, 200f, -100f, fitted, stage)
        assertEquals(2f, view.scale)
        // The picture point under (200, -100) at scale 1 is still under it.
        assertEquals(200f, (200f - view.x) / view.scale, 0.001f)
        assertEquals(-100f, (-100f - view.y) / view.scale, 0.001f)
    }

    @Test
    fun theScaleStaysBetweenFittedAndTheMaximum() {
        val fitted = Size(1000f, 750f)
        assertEquals(PhotoZoom.FIT, PhotoZoom.zoomAt(PhotoZoom.FIT, 0.5f, 0f, 0f, fitted, stage))
        assertEquals(PhotoZoom.MAX_SCALE, PhotoZoom.zoomAt(PhotoZoom.FIT, 100f, 0f, 0f, fitted, stage).scale)
    }

    @Test
    fun panningStopsAtTheEdgesAndANarrowSideStaysCentered() {
        // At 1.5x: 900 wide in a 1000-wide stage (no room to pan), 1200 tall in 800 (200 each way).
        val view = PhotoZoom.panBy(View(1.5f, 0f, 0f), 500f, -500f, Size(600f, 800f), stage)
        assertEquals(View(1.5f, 0f, -200f), view)
    }

    @Test
    fun zoomingOutPullsAPannedPictureBackIn() {
        val fitted = Size(1000f, 750f)
        val zoomed = View(4f, 1500f, 0f)
        assertEquals(zoomed, PhotoZoom.clamp(zoomed, fitted, stage))
        assertEquals(View(2f, 500f, 0f), PhotoZoom.zoomAt(zoomed, 0.5f, 0f, 0f, fitted, stage))
        assertEquals(PhotoZoom.FIT, PhotoZoom.zoomAt(zoomed, 0.25f, 0f, 0f, fitted, stage))
    }
}
