package dev.holdmytrack.android.sync

import java.time.Duration
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Test

class SimilarTest {

    private val t0 = Instant.parse("2026-09-01T08:00:00Z")
    private val s = Duration.ofSeconds(1)
    private val m = Duration.ofMinutes(1)
    private val h = Duration.ofHours(1)

    private fun matches(aStart: Instant, aDur: Duration, bStart: Instant, bDur: Duration) =
        Similar.overlaps(aStart, aStart + aDur, bStart, bStart + bDur)

    // The same cases as the server's TestOverlapMatches, so the two rules can't drift apart.
    @Test
    fun `the server's cases, both ways round`() {
        data class Case(val name: String, val aStart: Instant, val aDur: Duration, val bStart: Instant, val bDur: Duration, val want: Boolean)
        val cases = listOf(
            Case("identical ranges", t0, h, t0, h, true),
            Case("one copy started late and stopped early", t0, h, t0 + s.multipliedBy(20), h - s.multipliedBy(30), true),
            Case("back-to-back with a few seconds of clock skew", t0, h, t0 + h - s.multipliedBy(5), m.multipliedBy(40), false),
            Case("auto-detected fragment inside a long hike", t0, h.multipliedBy(2), t0 + m.multipliedBy(30), m.multipliedBy(10), false),
            Case("day hike inside a multi-day log", t0, h.multipliedBy(72), t0 + h.multipliedBy(26), h, false),
            Case("exactly the threshold", t0, s.multipliedBy(100), t0 + s.multipliedBy(20), s.multipliedBy(100), true),
            Case("just under the threshold", t0, s.multipliedBy(100), t0 + s.multipliedBy(21), s.multipliedBy(100), false),
            Case("no duration", t0, Duration.ZERO, t0, h, false),
        )
        for (c in cases) {
            assertEquals(c.name, c.want, matches(c.aStart, c.aDur, c.bStart, c.bDur))
            assertEquals("${c.name} (reversed)", c.want, matches(c.bStart, c.bDur, c.aStart, c.aDur))
        }
    }

    private fun candidate(source: String, id: String, start: Instant, minutes: Long) = SyncCandidates.build(
        source = source,
        externalId = id,
        startedAt = start,
        endedAt = start + Duration.ofMinutes(minutes),
        activityType = "walking",
        name = "",
        lats = doubleArrayOf(50.0, 50.001),
        lons = doubleArrayOf(10.0, 10.001),
        times = longArrayOf(0L, 60_000L),
        distanceM = 130.0,
        origin = null,
    )

    @Test
    fun `a recording and the watch's copy of the same walk mark each other`() {
        val watch = candidate("healthconnect", "hc-1", t0 + m.multipliedBy(2), 58)
        val phone = candidate("recorded", "rec-1", t0, 58)
        val other = candidate("healthconnect", "hc-2", t0 + h.multipliedBy(5), 30)
        val similar = Similar.waiting(listOf(watch, phone, other))
        assertEquals(listOf("recorded:rec-1"), similar["healthconnect:hc-1"]?.map { it.key })
        assertEquals(listOf("healthconnect:hc-1"), similar["recorded:rec-1"]?.map { it.key })
        assertEquals(null, similar["healthconnect:hc-2"])
    }

    @Test
    fun `three copies each list the other two, newest first`() {
        val a = candidate("healthconnect", "a", t0, 60)
        val b = candidate("healthconnect", "b", t0 + m, 60)
        val c = candidate("recorded", "c", t0 + m.multipliedBy(2), 60)
        val similar = Similar.waiting(listOf(a, b, c))
        assertEquals(listOf("recorded:c", "healthconnect:b"), similar["healthconnect:a"]?.map { it.key })
        assertEquals(listOf("healthconnect:b", "healthconnect:a"), similar["recorded:c"]?.map { it.key })
    }
}