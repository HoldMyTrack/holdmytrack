package dev.holdmytrack.android.recording

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RecordTapRulesTest {

    @Test
    fun `a tap pauses, resumes or starts, and never stops`() {
        assertEquals(RecordTap.Pause, RecordTapRules.tap(RecordingState.RECORDING, emptyList()))
        assertEquals(RecordTap.Resume, RecordTapRules.tap(RecordingState.PAUSED, listOf("location")))
        assertEquals(RecordTap.Start, RecordTapRules.tap(RecordingState.IDLE, emptyList()))
        assertEquals(RecordTap.AskPermissions(listOf("location")), RecordTapRules.tap(RecordingState.IDLE, listOf("location")))
    }

    @Test
    fun `a Stop replayed from recents is ignored`() {
        assertTrue(RecordTapRules.stopIntentApplies(isStopAction = true, launchedFromHistory = false))
        assertFalse(RecordTapRules.stopIntentApplies(isStopAction = true, launchedFromHistory = true))
        assertFalse(RecordTapRules.stopIntentApplies(isStopAction = false, launchedFromHistory = false))
    }
}
