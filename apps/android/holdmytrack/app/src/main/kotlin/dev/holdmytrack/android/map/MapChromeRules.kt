package dev.holdmytrack.android.map

import dev.holdmytrack.android.panel.PanelTab

/** The map's notices, most important first: when two apply, the earlier stays up. */
enum class MapNotice { MAP_FAILED, UNREACHABLE, CHECKING, EMPTY }

/**
 * Which of the map's controls and notices show, as plain values `MapFragment` renders.
 * `sessionReady` is the session confirmed and the mode toggle shown once (`syncSession`).
 */
object MapChromeRules {
    /** Normal mode, session ready, not recording: when the sheet and its editors have the map. */
    fun normalMode(sessionReady: Boolean, mode: MapMode, recording: Boolean): Boolean =
        sessionReady && mode == MapMode.NORMAL && !recording

    /** The mode toggle, hidden while recording, editing or checking rows. */
    fun modeBarShown(recording: Boolean, editOpen: Boolean, selectionShown: Boolean): Boolean =
        !recording && !editOpen && !selectionShown

    /** The chip naming what Fog or Heatmap shows, since neither takes the date range. */
    fun modeChipShown(mode: MapMode, sessionReady: Boolean, recording: Boolean, exportOpen: Boolean): Boolean =
        mode != MapMode.NORMAL && sessionReady && !recording && !exportOpen

    /** "Your map is still being updated…" (`docs/SPEC.md` FR-4.2), in Fog and Heatmap while
     *  coverage is rendering. */
    fun coverageNoticeShown(rendering: Boolean, mode: MapMode, sessionReady: Boolean, recording: Boolean): Boolean =
        rendering && mode != MapMode.NORMAL && sessionReady && !recording

    /** The rail (Layers, Find my location), hidden while an editor is open or rows are checked. */
    fun railShown(editing: Boolean, selectionShown: Boolean): Boolean = !editing && !selectionShown

    /** Whether rows are being checked where the selection bar can show: the sheet up on the
     *  Activities tab, no Edit window. */
    fun selectionShown(sheetVisible: Boolean, selecting: Boolean, tab: PanelTab, editOpen: Boolean): Boolean =
        sheetVisible && selecting && tab == PanelTab.ACTIVITIES && !editOpen

    /** The date range at the sheet's head: the Activities tab's, in Normal mode, unless the
     *  selected activity's card has the head, once the days are in, and only with history. */
    fun dateFooterShown(normal: Boolean, tab: PanelTab, showsCard: Boolean, daysReady: Boolean, hasHistory: Boolean): Boolean =
        normal && tab == PanelTab.ACTIVITIES && !showsCard && daysReady && hasHistory

    /** The sheet: Normal mode's, and the Sync tab's even while recording, so what's on the phone
     *  can be sent mid-walk; hidden while an editor has the screen. */
    fun sheetShown(normal: Boolean, sessionReady: Boolean, mode: MapMode, tab: PanelTab, editing: Boolean): Boolean {
        val syncing = sessionReady && mode == MapMode.NORMAL && tab == PanelTab.SYNC
        return (normal || syncing) && !editing
    }

    /** Whether [next] may replace the notice up: a less important one never replaces a more important one. */
    fun noticeReplaces(current: MapNotice?, next: MapNotice): Boolean = current == null || current.ordinal >= next.ordinal

    /** The empty notice steps aside while recording and on the Sync tab it points to; failures stay up. */
    fun noticeShown(kind: MapNotice?, recording: Boolean, onSync: Boolean): Boolean =
        kind != null && !(kind == MapNotice.EMPTY && (recording || onSync))
}
