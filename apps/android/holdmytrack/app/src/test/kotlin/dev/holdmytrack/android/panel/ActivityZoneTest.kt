package dev.holdmytrack.android.panel

import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.ZoneId

class ActivityZoneTest {

    private val phone = ZoneId.of("Europe/Berlin")

    @Test
    fun `an activity's own zone wins`() {
        assertEquals(ZoneId.of("Asia/Tokyo"), ActivityZone.of("Asia/Tokyo", "America/New_York", phone))
    }

    @Test
    fun `without one, or one the phone doesn't know, the account's`() {
        assertEquals(ZoneId.of("America/New_York"), ActivityZone.of("", "America/New_York", phone))
        assertEquals(ZoneId.of("America/New_York"), ActivityZone.of(null, "America/New_York", phone))
        assertEquals(ZoneId.of("America/New_York"), ActivityZone.of("Mars/Olympus_Mons", "America/New_York", phone))
    }

    @Test
    fun `without either, the phone's`() {
        assertEquals(phone, ActivityZone.of("", "", phone))
        assertEquals(phone, ActivityZone.of("Mars/Olympus_Mons", "Moon/Tranquility", phone))
    }
}
