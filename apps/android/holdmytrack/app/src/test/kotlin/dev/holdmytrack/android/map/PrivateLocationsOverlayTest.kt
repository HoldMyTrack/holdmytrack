package dev.holdmytrack.android.map

import org.junit.Assert.assertEquals
import org.junit.Test
import kotlin.math.asin
import kotlin.math.cos
import kotlin.math.sin
import kotlin.math.sqrt

class PrivateLocationsOverlayTest {

    private fun haversine(lat1: Double, lon1: Double, lat2: Double, lon2: Double): Double {
        val r = 6371008.8
        val dLat = Math.toRadians(lat2 - lat1)
        val dLon = Math.toRadians(lon2 - lon1)
        val h = sin(dLat / 2) * sin(dLat / 2) + cos(Math.toRadians(lat1)) * cos(Math.toRadians(lat2)) * sin(dLon / 2) * sin(dLon / 2)
        return 2 * r * asin(sqrt(h))
    }

    @Test
    fun `every point of the ring is the radius away, as the server measures it`() {
        val circle = Circle(id = null, lon = -81.69, lat = 41.49, radiusM = 300)
        val ring = PrivateLocationsOverlay.polygon(circle)
        assertEquals(65, ring.size)
        for (p in ring) assertEquals(300.0, haversine(circle.lat, circle.lon, p.latitude(), p.longitude()), 0.01)
    }

    @Test
    fun `the ring closes on itself`() {
        val ring = PrivateLocationsOverlay.polygon(Circle(id = "a", lon = 10.0, lat = 60.0, radiusM = 2000))
        assertEquals(ring.first().latitude(), ring.last().latitude(), 1e-9)
        assertEquals(ring.first().longitude(), ring.last().longitude(), 1e-9)
    }
}
