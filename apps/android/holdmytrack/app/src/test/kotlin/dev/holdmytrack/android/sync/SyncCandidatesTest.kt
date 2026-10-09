package dev.holdmytrack.android.sync

import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Test

class SyncCandidatesTest {

    private fun candidate(source: String, id: String, startedAt: String) = SyncCandidates.build(
        source = source,
        externalId = id,
        startedAt = Instant.parse(startedAt),
        activityType = "walking",
        name = "",
        lats = doubleArrayOf(50.0, 50.001),
        lons = doubleArrayOf(10.0, 10.001),
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
}
