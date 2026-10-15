package dev.holdmytrack.android.map

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HeatPointsRadiusTest {

    @Test
    fun radiusDoublesAcrossEachZoomAndDropsBackAtTheNext() {
        val stops = heatPointsRadiusStops()
        stops.zipWithNext().forEach { (a, b) -> assertTrue("zooms ascend", b.first > a.first) }
        val start = stops.first().second
        for (z in 0 until HEAT_POINTS_MAX_ZOOM) {
            assertEquals(start, stops.first { it.first == z.toFloat() }.second, 0f)
            assertEquals(2 * start, stops.first { it.first == z + 0.999f }.second, 0f)
        }
    }

    @Test
    fun pastTheLastZoomTheRadiusKeepsGrowing() {
        val (last, prev) = heatPointsRadiusStops().reversed()
        assertEquals(HEAT_POINTS_MAX_ZOOM.toFloat(), prev.first)
        assertEquals(Math.pow(2.0, (last.first - prev.first).toDouble()).toFloat(), last.second / prev.second, 1e-3f)
    }
}
