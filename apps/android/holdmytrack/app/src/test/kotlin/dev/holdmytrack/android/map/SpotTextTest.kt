package dev.holdmytrack.android.map

import dev.holdmytrack.android.net.Spot
import org.junit.Assert.assertEquals
import org.junit.Test

class SpotTextTest {

    private fun spot(address: String? = null) = Spot(
        id = 1, category = "monument", name = null, address = address, description = null,
        inscription = null, memorial = null, startDate = null, wikipedia = null,
        lon = -81.6943605, lat = 41.4993201,
    )

    @Test
    fun addressIsOsmsWhenThereIsOne() {
        assertEquals("1 Main St, Cleveland", SpotText.address(spot("1 Main St, Cleveland")))
    }

    @Test
    fun addressFallsBackToLatLonAtSixDecimals() {
        assertEquals("41.499320, -81.694361", SpotText.address(spot()))
    }

    @Test
    fun wikipediaUrlUsesTheTagsLanguageAndUnderscores() {
        assertEquals("https://en.wikipedia.org/wiki/Soldiers'_and_Sailors'_Monument", SpotText.wikipediaUrl("en:Soldiers' and Sailors' Monument"))
    }

    @Test
    fun wikipediaUrlEncodesLikeEncodeUriComponent() {
        assertEquals(
            "https://ru.wikipedia.org/wiki/%D0%9A%D1%80%D0%B5%D0%BC%D0%BB%D1%8C_(%D0%B7%D0%B4%D0%B0%D0%BD%D0%B8%D0%B5)",
            SpotText.wikipediaUrl("ru:Кремль (здание)"),
        )
        assertEquals("https://de.wikipedia.org/wiki/A%2FB%3F", SpotText.wikipediaUrl("de: A/B? "))
    }

    @Test
    fun memorialLabelIsWords() {
        assertEquals("War memorial", SpotText.memorialLabel("war_memorial"))
        assertEquals("Plaque stolperstein", SpotText.memorialLabel("plaque;stolperstein"))
    }
}
