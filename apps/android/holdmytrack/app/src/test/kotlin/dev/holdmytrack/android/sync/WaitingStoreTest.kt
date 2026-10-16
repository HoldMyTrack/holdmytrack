package dev.holdmytrack.android.sync

import dev.holdmytrack.android.net.TrackMetricPoint
import org.junit.Assert.assertEquals
import org.junit.Test

/** What the last Load found, kept on the phone (`apps/android/docs/SPEC.md` FR-3.2). */
class WaitingStoreTest {

    @Test
    fun `a line's vertices come back as they went in`() {
        val points = listOf(
            TrackMetricPoint(lon = 10.123456789, lat = 50.987654321, speedMps = 1.4, timeS = 1_790_000_000),
            TrackMetricPoint(lon = -0.5, lat = -33.25, speedMps = 0.0, timeS = 1_790_000_060),
        )
        assertEquals(points, WaitingStore.Vertices.decode(WaitingStore.Vertices.encode(points)))
    }

    @Test
    fun `no vertices is an empty line`() {
        assertEquals(emptyList<TrackMetricPoint>(), WaitingStore.Vertices.decode(WaitingStore.Vertices.encode(emptyList())))
    }

    @Test
    fun `a bbox comes back as it went in, and an empty one stays empty`() {
        val bbox = listOf(10.0, 50.0, 10.001, 50.0015)
        assertEquals(bbox, WaitingStore.Vertices.decodeBbox(WaitingStore.Vertices.encodeBbox(bbox)))
        assertEquals(emptyList<Double>(), WaitingStore.Vertices.decodeBbox(WaitingStore.Vertices.encodeBbox(emptyList())))
    }
}