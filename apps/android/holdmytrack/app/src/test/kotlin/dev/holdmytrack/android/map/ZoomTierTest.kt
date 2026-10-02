package dev.holdmytrack.android.map

import org.junit.Assert.assertEquals
import org.junit.Test

class ZoomTierTest {

    @Test
    fun bandsMatchTheLayersMinAndMaxZooms() {
        assertEquals(ZoomTier.COUNTRY, MapOverlays.zoomTier(0.0))
        assertEquals(ZoomTier.COUNTRY, MapOverlays.zoomTier(2.99))
        assertEquals(ZoomTier.REGION, MapOverlays.zoomTier(3.0))
        assertEquals(ZoomTier.REGION, MapOverlays.zoomTier(6.99))
        assertEquals(ZoomTier.CITY, MapOverlays.zoomTier(7.0))
        assertEquals(ZoomTier.CITY, MapOverlays.zoomTier(14.0))
    }
}
