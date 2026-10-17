package dev.holdmytrack.android.map

import org.junit.Assert.assertArrayEquals
import org.junit.Test

class FrameRulesTest {

    @Test
    fun `the fit sits between the top row and the covered bottom`() {
        assertArrayEquals(intArrayOf(64, 200, 64, 364), FrameRules.padding(2000, chromeBottom = 200, bannerBottom = null, coveredBelow = 300, minSpanPx = 96))
    }

    @Test
    fun `a capture banner pushes the top down`() {
        assertArrayEquals(intArrayOf(64, 432, 64, 64), FrameRules.padding(2000, chromeBottom = 200, bannerBottom = 400, coveredBelow = 0, minSpanPx = 96))
    }

    @Test
    fun `with no room left the fit keeps a strip, taken from the top first`() {
        // 600 high, 400 covered below: the top gives way to keep 96 between them.
        assertArrayEquals(intArrayOf(64, 64, 64, 440), FrameRules.padding(600, chromeBottom = 300, bannerBottom = null, coveredBelow = 376, minSpanPx = 96))
    }
}
