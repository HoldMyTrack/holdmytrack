package dev.holdmytrack.android.map

import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class CapturesSinceTest {

    private val nine = Instant.parse("2026-10-14T09:00:00.123456Z")
    private val ten = Instant.parse("2026-10-14T10:00:00Z")

    @Test
    fun sinceIsTheNewestCaptureOrNothing() {
        assertNull(capturesSince(emptyMap()))
        assertEquals(ten, capturesSince(mapOf(1L to nine, 2L to ten)))
    }

    @Test
    fun aMergeKeepsOnePerPlace() {
        val merged = mergeCaptures(mapOf(1L to nine, 2L to ten), mapOf(2L to ten, 3L to ten))
        assertEquals(mapOf(1L to nine, 2L to ten, 3L to ten), merged)
    }

    @Test
    fun sinceGoesBackAsTheServerWroteIt() {
        assertEquals("2026-10-14T09:00:00.123456Z", nine.toString())
    }
}
