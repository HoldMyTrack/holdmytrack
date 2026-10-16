package dev.holdmytrack.android.map

import dev.holdmytrack.android.panel.PanelTab
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MapChromeRulesTest {

    @Test
    fun `Normal mode needs the session and no recording`() {
        assertTrue(MapChromeRules.normalMode(sessionReady = true, MapMode.NORMAL, recording = false))
        assertFalse(MapChromeRules.normalMode(sessionReady = false, MapMode.NORMAL, recording = false))
        assertFalse(MapChromeRules.normalMode(sessionReady = true, MapMode.FOG, recording = false))
        assertFalse(MapChromeRules.normalMode(sessionReady = true, MapMode.NORMAL, recording = true))
    }

    @Test
    fun `the mode chip names Fog and Heatmap only, and steps aside for recording and export`() {
        assertFalse(MapChromeRules.modeChipShown(MapMode.NORMAL, sessionReady = true, recording = false, exportOpen = false))
        assertTrue(MapChromeRules.modeChipShown(MapMode.HEATMAP, sessionReady = true, recording = false, exportOpen = false))
        assertFalse(MapChromeRules.modeChipShown(MapMode.FOG, sessionReady = true, recording = true, exportOpen = false))
        assertFalse(MapChromeRules.modeChipShown(MapMode.FOG, sessionReady = true, recording = false, exportOpen = true))
    }

    @Test
    fun `the coverage notice shows in Fog and Heatmap while rendering`() {
        assertTrue(MapChromeRules.coverageNoticeShown(rendering = true, MapMode.FOG, sessionReady = true, recording = false))
        assertFalse(MapChromeRules.coverageNoticeShown(rendering = true, MapMode.NORMAL, sessionReady = true, recording = false))
        assertFalse(MapChromeRules.coverageNoticeShown(rendering = false, MapMode.FOG, sessionReady = true, recording = false))
    }

    @Test
    fun `checking rows hides the mode toggle and the rail`() {
        assertTrue(MapChromeRules.selectionShown(sheetVisible = true, selecting = true, PanelTab.ACTIVITIES, editOpen = false))
        assertFalse(MapChromeRules.selectionShown(sheetVisible = true, selecting = true, PanelTab.STORIES, editOpen = false))
        assertFalse(MapChromeRules.modeBarShown(recording = false, editOpen = false, selectionShown = true))
        assertFalse(MapChromeRules.railShown(editing = false, selectionShown = true))
        assertTrue(MapChromeRules.railShown(editing = false, selectionShown = false))
    }

    @Test
    fun `the Sync tab keeps the sheet while recording, an editor hides it`() {
        assertTrue(MapChromeRules.sheetShown(normal = false, sessionReady = true, MapMode.NORMAL, PanelTab.SYNC, editing = false))
        assertFalse(MapChromeRules.sheetShown(normal = false, sessionReady = true, MapMode.NORMAL, PanelTab.ACTIVITIES, editing = false))
        assertFalse(MapChromeRules.sheetShown(normal = true, sessionReady = true, MapMode.NORMAL, PanelTab.ACTIVITIES, editing = true))
    }

    @Test
    fun `the date range needs the Activities tab, the days and some history`() {
        assertTrue(MapChromeRules.dateFooterShown(normal = true, PanelTab.ACTIVITIES, showsCard = false, daysReady = true, hasHistory = true))
        assertFalse(MapChromeRules.dateFooterShown(normal = true, PanelTab.ACTIVITIES, showsCard = true, daysReady = true, hasHistory = true))
        assertFalse(MapChromeRules.dateFooterShown(normal = true, PanelTab.ACTIVITIES, showsCard = false, daysReady = true, hasHistory = false))
    }

    @Test
    fun `a less important notice never replaces a more important one`() {
        assertTrue(MapChromeRules.noticeReplaces(null, MapNotice.EMPTY))
        assertTrue(MapChromeRules.noticeReplaces(MapNotice.EMPTY, MapNotice.MAP_FAILED))
        assertTrue(MapChromeRules.noticeReplaces(MapNotice.CHECKING, MapNotice.CHECKING))
        assertFalse(MapChromeRules.noticeReplaces(MapNotice.UNREACHABLE, MapNotice.EMPTY))
    }

    @Test
    fun `the empty notice steps aside while recording and on Sync, failures stay`() {
        assertFalse(MapChromeRules.noticeShown(MapNotice.EMPTY, recording = true, onSync = false))
        assertFalse(MapChromeRules.noticeShown(MapNotice.EMPTY, recording = false, onSync = true))
        assertTrue(MapChromeRules.noticeShown(MapNotice.MAP_FAILED, recording = true, onSync = true))
        assertFalse(MapChromeRules.noticeShown(null, recording = false, onSync = false))
    }
}
