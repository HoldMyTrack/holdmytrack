package dev.holdmytrack.android.panel

import dev.holdmytrack.android.net.TrackEdit
import dev.holdmytrack.android.net.TrackPoint
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class EditTrackOpsTest {

    /** Points at t = 0, 10, 20, … along the equator, 0.001° (≈111 m) apart. */
    private val track = List(10) { TrackPoint(lon = it * 0.001, lat = 0.0, t = it * 10L) }

    private fun times(points: List<TrackPoint>) = points.map { it.t }

    @Test
    fun `chop keeps the knobs' stretch, inclusive`() {
        val op = EditTrackOps.chop(track, 2, 5)!!
        val edit = EditTrackOps.fold(null, listOf(op))
        assertEquals(listOf(20L, 30L, 40L, 50L), times(EditTrackOps.apply(track, edit)))
    }

    @Test
    fun `chop at both ends or on one point does nothing`() {
        assertNull(EditTrackOps.chop(track, 0, 9))
        assertNull(EditTrackOps.chop(track, 4, 4))
    }

    @Test
    fun `cut removes what's between the knobs and keeps the knobs`() {
        val op = EditTrackOps.cut(track, 2, 6)!!
        val edit = EditTrackOps.fold(null, listOf(op))
        assertEquals(listOf(0L, 10L, 20L, 60L, 70L, 80L, 90L), times(EditTrackOps.apply(track, edit)))
        assertNull(EditTrackOps.cut(track, 2, 3))
    }

    @Test
    fun `a second chop narrows the first`() {
        val edit = EditTrackOps.fold(null, listOf(EditOp.Chop(10L to 70L), EditOp.Chop(30L to 90L)))
        assertEquals(30L to 70L, edit.keep)
    }

    @Test
    fun `steps build on the saved edit, and reset clears both`() {
        val base = TrackEdit(drop = listOf(40L))
        val edit = EditTrackOps.fold(base, listOf(EditOp.Drop(50L)))
        assertEquals(listOf(40L, 50L), edit.drop)
        assertTrue(EditTrackOps.fold(base, listOf(EditOp.Drop(50L), EditOp.Reset)).isEmpty)
    }

    @Test
    fun `points are named by time, so a drop doesn't shift a later cut`() {
        val afterDrop = EditTrackOps.apply(track, EditTrackOps.fold(null, listOf(EditOp.Drop(10L))))
        val cut = EditTrackOps.cut(afterDrop, 0, 3)!!
        val edit = EditTrackOps.fold(null, listOf(EditOp.Drop(10L), cut))
        assertEquals(listOf(0L, 40L, 50L, 60L, 70L, 80L, 90L), times(EditTrackOps.apply(track, edit)))
    }

    @Test
    fun `distances add up along the track`() {
        val d = EditTrackOps.cumulativeDistances(track)
        assertEquals(0.0, d[0], 0.0)
        assertEquals(111.19, d[1], 0.1)
        assertEquals(9 * 111.19, d[9], 1.0)
    }
}
