package dev.holdmytrack.android.map

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SimplifyTest {

    @Test
    fun `a straight line keeps only its ends`() {
        val lats = DoubleArray(100) { 50.0 + it * 0.0001 }
        val lons = DoubleArray(100) { 10.0 }
        assertArrayEquals(intArrayOf(0, 99), Simplify.indexes(lats, lons))
    }

    @Test
    fun `a corner well past the tolerance is kept`() {
        // East 1 km, then north 1 km.
        val lats = doubleArrayOf(50.0, 50.0, 50.0, 50.0045, 50.009)
        val lons = doubleArrayOf(10.0, 10.007, 10.014, 10.014, 10.014)
        assertArrayEquals(intArrayOf(0, 2, 4), Simplify.indexes(lats, lons))
    }

    @Test
    fun `a wobble under the tolerance is dropped`() {
        // 5 m off the line, under the 15 m tolerance.
        val lats = doubleArrayOf(50.0, 50.000045, 50.0)
        val lons = doubleArrayOf(10.0, 10.007, 10.014)
        assertArrayEquals(intArrayOf(0, 2), Simplify.indexes(lats, lons))
    }

    @Test
    fun `a long winding route comes back under the cap`() {
        val n = 20_000
        val lats = DoubleArray(n) { 50.0 + it * 0.00005 }
        val lons = DoubleArray(n) { 10.0 + 0.01 * Math.sin(it / 20.0) }
        val kept = Simplify.indexes(lats, lons, maxPoints = 300)
        assertTrue(kept.size <= 300)
        assertEquals(0, kept.first())
        assertEquals(n - 1, kept.last())
    }

    @Test
    fun `one or two points come back as they are`() {
        assertArrayEquals(intArrayOf(0), Simplify.indexes(doubleArrayOf(50.0), doubleArrayOf(10.0)))
        assertArrayEquals(intArrayOf(0, 1), Simplify.indexes(doubleArrayOf(50.0, 50.0), doubleArrayOf(10.0, 10.0)))
    }
}
