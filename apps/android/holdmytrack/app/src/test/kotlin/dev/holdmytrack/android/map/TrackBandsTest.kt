package dev.holdmytrack.android.map

import dev.holdmytrack.android.net.TrackMetricPoint
import org.junit.Assert.assertEquals
import org.junit.Test

class TrackBandsTest {

    private fun points(vararg speeds: Double) = speeds.mapIndexed { i, v ->
        TrackMetricPoint(lon = i.toDouble(), lat = 0.0, speedMps = v)
    }

    @Test
    fun `the scale is the track's own lowest and highest speed`() {
        assertEquals(BandScale(1.0, 5.0), TrackBands.scale(points(3.0, 1.0, 5.0)))
    }

    @Test
    fun `values split into five bands, the top value in the last`() {
        val scale = BandScale(0.0, 10.0)
        assertEquals(0, TrackBands.bandIndex(0.0, scale))
        assertEquals(1, TrackBands.bandIndex(2.0, scale))
        assertEquals(2, TrackBands.bandIndex(5.0, scale))
        assertEquals(4, TrackBands.bandIndex(10.0, scale))
    }

    @Test
    fun `a flat track is all one band`() {
        assertEquals(0, TrackBands.bandIndex(3.0, BandScale(3.0, 3.0)))
    }

    @Test
    fun `runs share their boundary point and cover the whole track`() {
        val track = points(0.0, 0.0, 10.0, 10.0, 0.0)
        val runs = TrackBands.runs(track, TrackBands.scale(track))
        assertEquals(listOf(BandRun(0, 0, 1), BandRun(4, 1, 3), BandRun(0, 3, 4)), runs)
    }

    @Test
    fun `fewer than two points has no runs`() {
        assertEquals(emptyList<BandRun>(), TrackBands.runs(points(1.0), BandScale(0.0, 1.0)))
    }
}
