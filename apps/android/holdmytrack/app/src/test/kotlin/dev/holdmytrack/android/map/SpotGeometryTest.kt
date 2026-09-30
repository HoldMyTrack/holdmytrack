package dev.holdmytrack.android.map

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SpotGeometryTest {

    // About 111 m on a side at the equator, with a 22 m hole in the middle.
    private val outer = listOf(GeoPoint(0.0, 0.0), GeoPoint(0.0, 0.001), GeoPoint(0.001, 0.001), GeoPoint(0.001, 0.0), GeoPoint(0.0, 0.0))
    private val hole = listOf(GeoPoint(0.0004, 0.0004), GeoPoint(0.0004, 0.0006), GeoPoint(0.0006, 0.0006), GeoPoint(0.0006, 0.0004), GeoPoint(0.0004, 0.0004))
    private val square: SpotArea = listOf(listOf(outer))
    private val withHole: SpotArea = listOf(listOf(outer, hole))

    @Test
    fun insideAndOutside() {
        assertTrue(SpotGeometry.contains(square, GeoPoint(0.0005, 0.0005)))
        assertFalse(SpotGeometry.contains(square, GeoPoint(0.002, 0.0005)))
    }

    @Test
    fun aHoleIsOutside() {
        assertFalse(SpotGeometry.contains(withHole, GeoPoint(0.0005, 0.0005)))
        assertTrue(SpotGeometry.contains(withHole, GeoPoint(0.0002, 0.0002)))
    }

    @Test
    fun anyPolygonOfAMultiPolygonCounts() {
        val far = listOf(listOf(GeoPoint(1.0, 1.0), GeoPoint(1.0, 1.001), GeoPoint(1.001, 1.001), GeoPoint(1.001, 1.0), GeoPoint(1.0, 1.0)))
        assertTrue(SpotGeometry.contains(listOf(listOf(outer), far), GeoPoint(1.0005, 1.0005)))
    }

    @Test
    fun distanceIsToTheNearestEdgeAndZeroInside() {
        assertEquals(0.0, SpotGeometry.distanceM(square, GeoPoint(0.0005, 0.0005)), 0.0)
        // 0.001° of latitude north of the top edge: about 111 m.
        assertEquals(111.2, SpotGeometry.distanceM(square, GeoPoint(0.002, 0.0005)), 0.5)
        val nearest = SpotGeometry.nearestPoint(square, GeoPoint(0.002, 0.0005))!!
        assertEquals(0.001, nearest.lat, 1e-9)
        assertEquals(0.0005, nearest.lon, 1e-9)
    }

    @Test
    fun bearingIsClockwiseFromNorth() {
        val from = GeoPoint(0.0, 0.0)
        assertEquals(0.0, SpotGeometry.bearing(from, GeoPoint(0.001, 0.0)), 1e-6)
        assertEquals(90.0, SpotGeometry.bearing(from, GeoPoint(0.0, 0.001)), 1e-6)
        assertEquals(180.0, SpotGeometry.bearing(from, GeoPoint(-0.001, 0.0)), 1e-6)
        assertEquals(270.0, SpotGeometry.bearing(from, GeoPoint(0.0, -0.001)), 1e-6)
    }
}
