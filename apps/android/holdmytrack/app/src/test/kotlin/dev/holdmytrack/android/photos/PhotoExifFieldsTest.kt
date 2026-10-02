package dev.holdmytrack.android.photos

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.Instant

class PhotoExifFieldsTest {

    private fun resolve(
        original: String? = null,
        dateTime: String? = null,
        offset: String? = null,
        lat: Double? = null,
        lon: Double? = null,
        gpsDate: String? = null,
        gpsTime: String? = null,
    ) = PhotoExifFields.resolve(original, dateTime, offset, lat, lon, gpsDate, gpsTime)

    @Test
    fun aZonedCaptureTimeIsAnInstant() {
        val exif = resolve(original = "2026:09:30 13:00:20", offset = "-04:00")
        assertEquals(Instant.parse("2026-09-30T17:00:20Z"), exif.takenAt)
        assertNull(exif.takenLocal)
    }

    @Test
    fun theGpsClockGivesTheZoneAndTheCameraKeepsItsSeconds() {
        // The camera says 13:00:20; the fix, a little stale, 17:01:05 UTC — four hours behind,
        // to the nearest quarter hour, so the camera's own clock read at -04:00.
        val exif = resolve(original = "2026:09:30 13:00:20", gpsDate = "2026:09:30", gpsTime = "17:01:05")
        assertEquals(Instant.parse("2026-09-30T17:00:20Z"), exif.takenAt)
    }

    @Test
    fun theGpsClockReadsAsRationalsToo() {
        val exif = resolve(original = "2026:09:30 13:00:20", gpsDate = "2026:09:30", gpsTime = "17/1,0/1,30/1")
        assertEquals(Instant.parse("2026-09-30T17:00:20Z"), exif.takenAt)
    }

    @Test
    fun aZoneOutsideTheWorldsFallsBackToTheGpsTime() {
        val exif = resolve(original = "2026:09:30 13:00:20", gpsDate = "2026:09:28", gpsTime = "17:00:00")
        assertEquals(Instant.parse("2026-09-28T17:00:00Z"), exif.takenAt)
    }

    @Test
    fun withNoZoneTheTimeGoesAsAWallClock() {
        val exif = resolve(original = "2026:09:30 13:00:00")
        assertNull(exif.takenAt)
        assertEquals("2026-09-30T13:00:00", exif.takenLocal)
    }

    @Test
    fun theFilesDateTimeStandsInForAMissingOriginal() {
        assertEquals("2026-09-30T08:15:42", resolve(dateTime = "2026:09:30 08:15:42").takenLocal)
    }

    @Test
    fun onlyAGpsClockIsStillACaptureTime() {
        assertEquals(Instant.parse("2026-09-30T17:00:00Z"), resolve(gpsDate = "2026:09:30", gpsTime = "17:00:00").takenAt)
    }

    @Test
    fun blankValuesReadAsAbsent() {
        val exif = resolve(original = "0000:00:00 00:00:00", dateTime = "    :  :     :  :  ", lat = 0.0, lon = 0.0)
        assertNull(exif.takenAt)
        assertNull(exif.takenLocal)
        assertNull(exif.lat)
        assertNull(exif.lon)
    }

    @Test
    fun aPositionIsKeptWhenItsReal() {
        val exif = resolve(lat = 41.482, lon = -81.703)
        assertEquals(41.482, exif.lat!!, 0.0)
        assertEquals(-81.703, exif.lon!!, 0.0)
        assertNull(resolve(lat = 95.0, lon = 10.0).lat)
    }
}
