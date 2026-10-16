package dev.holdmytrack.android.map

import org.junit.Assert.assertEquals
import org.junit.Test

class SatelliteRoadsTest {

    @Test
    fun roadsComeOffOnlyOverImageryInFogAndHeatmap() {
        for (mode in MapMode.values()) {
            assertEquals("satellite, $mode", mode != MapMode.NORMAL, roadsHiddenOverImagery(true, mode))
            assertEquals("no satellite, $mode", false, roadsHiddenOverImagery(false, mode))
        }
    }
}
