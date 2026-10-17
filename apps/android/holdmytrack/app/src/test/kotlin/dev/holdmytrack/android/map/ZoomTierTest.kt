package dev.holdmytrack.android.map

import dev.holdmytrack.android.R
import org.junit.Assert.assertEquals
import org.junit.Test

class ZoomTierTest {

    @Test
    fun fogNamesTheTierHeatmapHasOneMessage() {
        assertEquals(R.string.map_level_fog_country, levelNoticeText(MapMode.FOG, ZoomTier.COUNTRY))
        assertEquals(R.string.map_level_fog_city, levelNoticeText(MapMode.FOG, ZoomTier.CITY))
        for (tier in ZoomTier.values()) {
            assertEquals(R.string.map_level_heatmap, levelNoticeText(MapMode.HEATMAP, tier))
        }
    }
}
