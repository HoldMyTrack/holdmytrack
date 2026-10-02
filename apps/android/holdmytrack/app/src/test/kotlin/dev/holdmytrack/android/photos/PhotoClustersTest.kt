package dev.holdmytrack.android.photos

import org.junit.Assert.assertEquals
import org.junit.Test

class PhotoClustersTest {

    @Test
    fun pointsApartStayApartAndCloseOnesGroup() {
        val groups = PhotoClusters.cluster(
            listOf(ScreenPoint("a", 0f, 0f), ScreenPoint("b", 10f, 10f), ScreenPoint("c", 200f, 0f)),
            radius = 32f,
        )
        assertEquals(listOf(listOf("a", "b"), listOf("c")), groups.map { it.ids })
    }

    @Test
    fun aGroupSitsOnItsFirstPoint() {
        val group = PhotoClusters.cluster(listOf(ScreenPoint("a", 5f, 7f), ScreenPoint("b", 20f, 7f)), radius = 32f).single()
        assertEquals(5f, group.x, 0f)
        assertEquals(7f, group.y, 0f)
    }

    @Test
    fun aRunOfClosePointsSplitsAtTheRadiusRatherThanChaining() {
        // Every point 20 px from the last: each is within the radius of its neighbour, but a
        // group anchored on its first point only takes those within 32 px of that.
        val run = (0 until 5).map { ScreenPoint("p$it", it * 20f, 0f) }
        assertEquals(listOf(listOf("p0", "p1"), listOf("p2", "p3"), listOf("p4")), PhotoClusters.cluster(run, radius = 32f).map { it.ids })
    }

    @Test
    fun theEdgeOfTheRadiusCounts() {
        val groups = PhotoClusters.cluster(listOf(ScreenPoint("a", 0f, 0f), ScreenPoint("b", 32f, 0f)), radius = 32f)
        assertEquals(1, groups.size)
    }
}
