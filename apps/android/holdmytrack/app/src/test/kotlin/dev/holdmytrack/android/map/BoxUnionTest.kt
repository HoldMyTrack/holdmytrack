package dev.holdmytrack.android.map

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class BoxUnionTest {

    private fun union(vararg boxes: List<Double>) = BoxUnion.of(boxes.toList())

    @Test
    fun `boxes on one side of the antimeridian union as they always did`() {
        assertNull(union())
        assertArrayEquals(doubleArrayOf(-81.7, 41.4, 1.0, 51.5), union(listOf(-81.7, 41.4, -81.6, 41.5), listOf(-1.0, 51.0, 1.0, 51.5)), 1e-9)
    }

    @Test
    fun `boxes either side of it go the short way across the Pacific`() {
        // New Zealand and Hawaii.
        assertArrayEquals(doubleArrayOf(174.7, -41.3, 202.2, 21.3), union(listOf(174.7, -41.3, 174.8, -41.2), listOf(-157.9, 21.2, -157.8, 21.3)), 1e-9)
    }

    @Test
    fun `a crossing box from the server joins a neighbour on either side`() {
        assertArrayEquals(doubleArrayOf(179.9, -18.0, 183.0, -16.9), union(listOf(179.9, -17.0, 180.1, -16.9), listOf(-178.0, -18.0, -177.0, -17.5)), 1e-9)
        assertArrayEquals(doubleArrayOf(178.0, -18.0, 180.1, -16.9), union(listOf(179.9, -17.0, 180.1, -16.9), listOf(178.0, -18.0, 179.0, -17.5)), 1e-9)
    }

    @Test
    fun `boxes all round the world give the whole world`() {
        assertArrayEquals(doubleArrayOf(-180.0, 0.0, 180.0, 1.0), union(listOf(-180.0, 0.0, -60.0, 1.0), listOf(-60.0, 0.0, 60.0, 1.0), listOf(60.0, 0.0, 180.0, 1.0)), 1e-9)
    }

    @Test
    fun `a view reported the long way round is turned the short way`() {
        assertEquals(179.8 to 180.2, BoxUnion.viewEdges(-179.8, 179.8).let { it.first to Math.round(it.second * 10) / 10.0 })
        assertEquals(10.0 to 11.0, BoxUnion.viewEdges(10.0, 11.0))
        assertEquals(179.8 to 180.2, BoxUnion.viewEdges(179.8, 180.2))
    }
}
