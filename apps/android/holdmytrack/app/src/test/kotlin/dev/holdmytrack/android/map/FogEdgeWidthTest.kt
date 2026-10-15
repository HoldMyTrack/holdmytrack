package dev.holdmytrack.android.map

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class FogEdgeWidthTest {
    @Test
    fun `the edge widens with every zoom, from a pixel at z10`() {
        val stops = FOG_EDGE_WIDTH_STOPS
        assertEquals(10f to 1f, stops.first())
        stops.zipWithNext().forEach { (a, b) ->
            assertTrue("$a then $b", b.first > a.first && b.second > a.second)
        }
    }

}
