package dev.holdmytrack.android.map

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SpotCaptureTest {

    private val area: SpotArea = listOf(listOf(listOf(GeoPoint(0.0, 0.0), GeoPoint(0.0, 0.001), GeoPoint(0.001, 0.001), GeoPoint(0.001, 0.0), GeoPoint(0.0, 0.0))))
    private val inside = GeoPoint(0.0005, 0.0005)
    private val outside = GeoPoint(0.002, 0.0005)

    private fun capture() = SpotCapture(area, inside)

    /** A fix a second from [fromS] to [toS] seconds, at [at]. */
    private fun SpotCapture.fixes(fromS: Int, toS: Int, at: GeoPoint, accuracyM: Float = 5f): SpotCapture.State {
        var state = this.state
        for (s in fromS..toS) state = onFix(at, accuracyM, s * 1000L)
        return state
    }

    @Test
    fun thirtySecondsInsideCaptures() {
        val c = capture()
        assertTrue(c.fixes(0, 29, inside) is SpotCapture.State.Holding)
        assertEquals(SpotCapture.State.Captured(inside), c.fixes(30, 30, inside))
    }

    @Test
    fun outsideIsAwayWithDistanceAndBearing() {
        val state = capture().fixes(0, 0, outside) as SpotCapture.State.Away
        assertEquals(111.2, state.distanceM, 0.5)
        assertEquals(180.0, state.bearing, 1e-6)
    }

    @Test
    fun steppingOutsideStartsTheHoldAgain() {
        val c = capture()
        c.fixes(0, 20, inside)
        c.fixes(21, 21, outside)
        assertEquals(SpotCapture.State.Holding(20_000, inside), c.fixes(22, 42, inside))
        assertTrue(c.fixes(43, 52, inside) is SpotCapture.State.Captured)
    }

    @Test
    fun inaccurateFixesAreIgnored() {
        val c = capture()
        assertEquals(SpotCapture.State.Locating, c.fixes(0, 40, inside, accuracyM = 40f))
        assertEquals(SpotCapture.State.Locating, c.onFix(inside, null, 41_000))
        c.fixes(50, 60, inside)
        // A coarse fix outside doesn't break the hold either.
        c.onFix(outside, 60f, 61_000)
        assertEquals(SpotCapture.State.Holding(11_000, inside), c.fixes(61, 61, inside))
    }

    @Test
    fun aGapInFixesCountsOnlyAFewSeconds() {
        val c = capture()
        c.fixes(0, 10, inside)
        // Five minutes in the background: only MAX_GAP_MS of it counts.
        assertEquals(SpotCapture.State.Holding(13_000, inside), c.onFix(inside, 5f, 310_000))
    }

    @Test
    fun resetStartsOver() {
        val c = capture()
        c.fixes(0, 30, inside)
        c.reset()
        assertEquals(SpotCapture.State.Locating, c.state)
        assertEquals(SpotCapture.State.Holding(0, inside), c.fixes(40, 40, inside))
    }
}
