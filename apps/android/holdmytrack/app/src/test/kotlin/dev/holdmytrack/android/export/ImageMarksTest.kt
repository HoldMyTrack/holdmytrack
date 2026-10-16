package dev.holdmytrack.android.export

import org.junit.Assert.assertEquals
import org.junit.Test

class ImageMarksTest {

    @Test
    fun `the credit is the basemap's, then the imagery's when shown, as plain text`() {
        val basemap = "<a href=\"https://protomaps.com\">Protomaps</a> © <a href=\"https://openstreetmap.org\">OpenStreetMap</a>"
        assertEquals("Protomaps © OpenStreetMap", ImageMarks.creditText(basemap, null))
        assertEquals("Protomaps © OpenStreetMap · © MapTiler", ImageMarks.creditText(basemap, "<a href=\"x\">© MapTiler</a>"))
        assertEquals("Protomaps © OpenStreetMap", ImageMarks.creditText(basemap, "  "))
        assertEquals("© OpenStreetMap & co", ImageMarks.creditText("&copy; OpenStreetMap &amp; co", null))
        assertEquals("© ©", ImageMarks.creditText("&#169; &#xA9;", null))
    }

    @Test
    fun `the credit's size follows the image's shorter side, within bounds`() {
        assertEquals(17.28f, ImageMarks.stripMetrics(1080, 1920).first, 1e-3f)
        assertEquals(12f, ImageMarks.stripMetrics(500, 300).first, 1e-3f)
        assertEquals(28f, ImageMarks.stripMetrics(3840, 3840).first, 1e-3f)
        assertEquals(53.76f, ImageMarks.stripMetrics(3840, 3840).second, 1e-3f)
    }
}
