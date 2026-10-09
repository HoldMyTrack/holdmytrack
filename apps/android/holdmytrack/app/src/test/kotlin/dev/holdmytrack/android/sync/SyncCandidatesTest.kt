package dev.holdmytrack.android.sync

import dev.holdmytrack.android.map.Geo
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Test

class SyncCandidatesTest {

    private fun candidate(source: String, id: String, startedAt: String) = SyncCandidates.build(
        source = source,
        externalId = id,
        startedAt = Instant.parse(startedAt),
        endedAt = Instant.parse(startedAt).plusSeconds(3600),
        activityType = "walking",
        name = "",
        lats = doubleArrayOf(50.0, 50.001),
        lons = doubleArrayOf(10.0, 10.001),
        times = longArrayOf(0L, 60_000L),
        distanceM = 130.0,
        origin = null,
    )

    private val older = candidate("healthconnect", "hc-old", "2026-09-01T08:00:00Z")
    private val newer = candidate("healthconnect", "hc-new", "2026-09-20T08:00:00Z")
    private val recorded = candidate("recorded", "rec-1", "2026-09-10T08:00:00Z")

    @Test
    fun `everything waiting is listed newest first`() {
        val listed = SyncCandidates.listed(listOf(older, newer), emptySet(), listOf(recorded), emptySet(), showHidden = false)
        assertEquals(listOf("healthconnect:hc-new", "recorded:rec-1", "healthconnect:hc-old"), listed.map { it.key })
    }

    @Test
    fun `a session the account already has is left out`() {
        val listed = SyncCandidates.listed(listOf(older, newer), setOf("hc-new"), listOf(recorded), emptySet(), showHidden = false)
        assertEquals(listOf("recorded:rec-1", "healthconnect:hc-old"), listed.map { it.key })
    }

    @Test
    fun `hidden rows come back only with Show hidden`() {
        val hidden = setOf("healthconnect:hc-old", "recorded:rec-1")
        val shown = SyncCandidates.listed(listOf(older, newer), emptySet(), listOf(recorded), hidden, showHidden = false)
        assertEquals(listOf("healthconnect:hc-new"), shown.map { it.key })
        val all = SyncCandidates.listed(listOf(older, newer), emptySet(), listOf(recorded), hidden, showHidden = true)
        assertEquals(3, all.size)
    }

    @Test
    fun `the line drawn is the route simplified, with its box`() {
        assertEquals(2, newer.line.size)
        assertEquals(listOf(10.0, 50.0, 10.001, 50.001), newer.bbox)
    }

    @Test
    fun `a vertex's speed is the stretch into it over the whole seconds it took`() {
        val lats = doubleArrayOf(50.0, 50.0005, 50.001, 50.002)
        val lons = doubleArrayOf(10.0, 10.0, 10.0, 10.0)
        // 0 s, 10.9 s (read as 10), 20.5 s (read as 20), 20.9 s (the same second).
        val times = longArrayOf(1_000_000L, 1_010_900L, 1_020_500L, 1_020_900L)
        val metrics = SyncCandidates.metrics(lats, lons, times, intArrayOf(0, 1, 2, 3))
        val leg = Geo.haversineM(50.0, 10.0, 50.0005, 10.0)
        assertEquals(leg / 10, metrics[1].speedMps, 1e-9)
        assertEquals(Geo.haversineM(50.0005, 10.0, 50.001, 10.0) / 10, metrics[2].speedMps, 1e-9)
        assertEquals(0.0, metrics[3].speedMps, 0.0)
        assertEquals(metrics[1].speedMps, metrics[0].speedMps, 0.0)
        assertEquals(1_010L, metrics[1].timeS)
    }

    @Test
    fun `speeds are over the kept vertices only, one per line vertex`() {
        val lats = doubleArrayOf(50.0, 50.0005, 50.001)
        val lons = doubleArrayOf(10.0, 10.0, 10.0)
        val metrics = SyncCandidates.metrics(lats, lons, longArrayOf(0L, 5_000L, 20_000L), intArrayOf(0, 2))
        assertEquals(2, metrics.size)
        assertEquals(Geo.haversineM(50.0, 10.0, 50.001, 10.0) / 20, metrics[1].speedMps, 1e-9)
        assertEquals(older.line.size, older.metrics.size)
    }
}
