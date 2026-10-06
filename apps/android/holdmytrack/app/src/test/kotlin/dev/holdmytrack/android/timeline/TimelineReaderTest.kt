package dev.holdmytrack.android.timeline

import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The web's `tests/timelineReader.test.mjs`, case for case: the same export, the same answers. */
class TimelineReaderTest {

    // The shape of a real Android export, with made-up places: a drive and a walk, and the
    // timelinePath points they draw their routes from (which don't line up with them). The
    // visit is what TimelineFile reads past, so it isn't here.
    private val drive = RawActivity(
        startTime = "2026-07-07T09:00:00.000-04:00",
        endTime = "2026-07-07T09:20:00.000-04:00",
        from = "10.0000000°, 20.0000000°",
        to = "10.1000000°, 20.1000000°",
        mode = "IN_PASSENGER_VEHICLE",
    )
    private val walk = RawActivity(
        startTime = "2026-07-08T18:00:00.000-04:00",
        endTime = "2026-07-08T18:30:00.000-04:00",
        from = "10.2000000°, 20.2000000°",
        to = "10.2010000°, 20.2010000°",
        mode = "WALKING",
    )
    private val path = listOf(
        RawPathPoint("10.0000000°, 20.0000000°", "2026-07-07T08:59:00.000-04:00"),
        RawPathPoint("10.0500000°, 20.0400000°", "2026-07-07T09:10:00.000-04:00"),
        RawPathPoint("10.0800000°, 20.0900000°", "2026-07-07T09:15:00.000-04:00"),
        RawPathPoint("10.1000000°, 20.1000000°", "2026-07-07T09:20:00.000-04:00"),
    )

    @Test
    fun eachActivityBecomesASegmentRoutedThroughThePathPointsInsideIt() {
        val read = TimelineReader.read(path, listOf(drive, walk))
        assertEquals(0, read.skipped)
        assertEquals(
            listOf(
                Triple("2026-07-07", "IN_PASSENGER_VEHICLE", "driving"),
                Triple("2026-07-08", "WALKING", "walking"),
            ),
            read.segments.map { Triple(it.day, it.mode, it.type) },
        )
        val segment = read.segments[0]
        // Its own start, the two path points strictly inside, its own end; the 08:59 point and
        // the one at exactly 09:20 are outside or duplicate the end.
        assertEquals(
            listOf(
                "2026-07-07T09:00:00.000-04:00",
                "2026-07-07T09:10:00.000-04:00",
                "2026-07-07T09:15:00.000-04:00",
                "2026-07-07T09:20:00.000-04:00",
            ),
            segment.points.map { it.time },
        )
        assertEquals(TimelinePoint(10.05, 20.04, "2026-07-07T09:10:00.000-04:00"), segment.points[1])
        val start = Instant.parse("2026-07-07T13:00:00Z").epochSecond
        val end = Instant.parse("2026-07-07T13:20:00Z").epochSecond
        assertEquals("seg-$start-$end", segment.id)
        assertTrue("distance ${segment.distanceM}", segment.distanceM > 15000 && segment.distanceM < 17000)
    }

    @Test
    fun anActivityThatNeverLeavesOnePlaceIsSkipped() {
        val read = TimelineReader.read(path, listOf(drive, walk.copy(to = walk.from)))
        assertEquals(1, read.segments.size)
        assertEquals(1, read.skipped)
    }

    @Test
    fun anActivityMissingItsTimesOrPlacesIsSkipped() {
        val read = TimelineReader.read(path, listOf(drive.copy(endTime = null), drive.copy(from = "nowhere"), drive.copy(endTime = drive.startTime)))
        assertEquals(0, read.segments.size)
        assertEquals(3, read.skipped)
    }

    @Test
    fun aStartOrEndThePathBesideItCouldNotHaveBeenReachedFromIsLeftOut() {
        // 150 km in the 10 minutes before the first path point, 44 km in the 5 after the last: 900 and 530 km/h.
        val far = drive.copy(from = "11.0000000°, 21.0000000°", to = "9.8000000°, 19.8000000°")
        val segment = TimelineReader.read(path, listOf(far)).segments[0]
        assertEquals(
            listOf("2026-07-07T09:10:00.000-04:00", "2026-07-07T09:15:00.000-04:00"),
            segment.points.map { it.time },
        )
        assertTrue("distance ${segment.distanceM}", segment.distanceM < 7000)
        // With no path inside, there's nothing to measure them against.
        assertEquals(2, TimelineReader.read(path, listOf(walk)).segments[0].points.size)
        // 5 s before the first path point: 550 m away is kept, 2.2 km away isn't.
        val fast = drive.copy(startTime = "2026-07-07T09:09:55.000-04:00", from = "10.0550000°, 20.0400000°")
        assertEquals("2026-07-07T09:09:55.000-04:00", TimelineReader.read(path, listOf(fast)).segments[0].points[0].time)
        val farFast = fast.copy(from = "10.0700000°, 20.0400000°")
        assertEquals("2026-07-07T09:10:00.000-04:00", TimelineReader.read(path, listOf(farFast)).segments[0].points[0].time)
    }

    @Test
    fun selectionIsByLocalStartDayAndMode() {
        val segments = TimelineReader.read(path, listOf(drive, walk)).segments
        val all = setOf("IN_PASSENGER_VEHICLE", "WALKING")
        assertEquals(2, TimelineReader.select(segments, "2026-07-07", "2026-07-08", all).size)
        assertEquals(1, TimelineReader.select(segments, "2026-07-08", "2026-07-08", all).size)
        assertEquals(1, TimelineReader.select(segments, "2026-07-01", "2026-07-31", setOf("WALKING")).size)
        assertTrue("FLYING" in TimelineReader.MODES_OFF_BY_DEFAULT)
    }

    @Test
    fun summaryAndBatches() {
        val segments = TimelineReader.read(path, listOf(drive, walk)).segments
        assertEquals(
            listOf("IN_PASSENGER_VEHICLE" to 1, "WALKING" to 1),
            TimelineReader.summarize(segments).map { it.mode to it.count },
        )
        assertEquals(2, TimelineReader.batches(segments, 1).size)
    }

    @Test
    fun modesAndCoordinates() {
        assertEquals("bus", TimelineReader.activityType("IN_BUS"))
        assertEquals("flying", TimelineReader.activityType("FLYING"))
        assertEquals("cycling", TimelineReader.activityType("CYCLING"))
        assertEquals(1.5 to -2.25, TimelineReader.parseLatLng("geo:1.5,-2.25"))
        assertNull(TimelineReader.parseLatLng("91°, 0°"))
        assertNull(TimelineReader.parseLatLng(null))
    }
}
