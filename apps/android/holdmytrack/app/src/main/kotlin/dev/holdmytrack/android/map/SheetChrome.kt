package dev.holdmytrack.android.map

import android.view.View
import androidx.activity.OnBackPressedCallback
import androidx.core.view.isVisible
import dev.holdmytrack.android.R
import dev.holdmytrack.android.panel.ActivitiesPanel

/**
 * The Activities sheet and the chrome that follows it ([MapChromeRules]): the sheet itself, the
 * date range at its head, the selection bar in the top row's place while rows are checked, and the
 * rail. Back ends selecting, then collapses an open sheet.
 */
class SheetChrome(
    view: View,
    private val panel: ActivitiesPanel,
    private val days: ActivityDays,
    /** Normal mode, session ready, not recording ([MapChromeRules.normalMode]). */
    private val normalMode: () -> Boolean,
    private val sessionReady: () -> Boolean,
    private val mode: () -> MapMode,
    private val editOpen: () -> Boolean,
    private val privacyEditing: () -> Boolean,
    /** The sheet came to rest or its head changed height. */
    private val onSheetMoved: () -> Unit,
    /** The selection bar came or went. */
    private val onSelectionBarChanged: () -> Unit,
    /** After every [render], for what else follows the sheet. */
    private val onRendered: () -> Unit,
    private val dismissLayers: () -> Unit,
) {
    private val sheet: View = view.findViewById(R.id.activities_sheet)
    private val dateFooter: View = view.findViewById(R.id.date_footer)
    private val selectionBar: View = view.findViewById(R.id.selection_bar)
    private val rail: View = view.findViewById(R.id.map_rail)

    /** Back closes an open sheet before it leaves the map. */
    val collapseOnBack = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = panel.setExpanded(false)
    }

    /** Back unchecks every row, ending selecting, before it does anything else on the map. */
    val endSelectingOnBack = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = panel.endSelecting()
    }

    /** The sheet is Normal mode's only (`docs/SPEC.md` FR-4.2), hidden while recording, and waits
     *  for the session. The range at its head is the Activities tab's, waits for the first page of
     *  days, and is absent for an account with no activity, where the empty notice speaks. */
    fun render() {
        val normal = normalMode()
        // The selected activity's card takes the range's place at the head.
        dateFooter.isVisible = MapChromeRules.dateFooterShown(normal, panel.tab, panel.showsCard, days.ready, days.earliest != null)
        // An editor over the map hides the sheet. The Sync tab stays while recording: what's on
        // the phone can be sent mid-walk.
        sheet.isVisible = MapChromeRules.sheetShown(normal, sessionReady(), mode(), panel.tab, editOpen() || privacyEditing())
        if (!normal) panel.dismissPopups()
        onSheetChanged()
        renderSelectionBar()
        onRendered()
    }

    /** Whether rows are being checked where the selection bar can show: Normal mode, the
     *  Activities tab, no Edit window. */
    fun selectionShown(): Boolean = MapChromeRules.selectionShown(sheet.isVisible, panel.selecting, panel.tab, editOpen())

    /** The selection bar replaces the top row while [selectionShown]. */
    fun renderSelectionBar() {
        val shown = selectionShown()
        endSelectingOnBack.isEnabled = shown
        if (selectionBar.isVisible == shown) return
        selectionBar.visibility = if (shown) View.VISIBLE else View.GONE
        if (!shown) panel.dismissPopups()
        onSelectionBarChanged()
        renderRail()
    }

    /** The rail (Layers, Find my location), hidden while an editor is open over the map. */
    fun renderRail() {
        val editing = editOpen() || privacyEditing()
        rail.isVisible = MapChromeRules.railShown(editing, selectionShown())
        if (editing) dismissLayers()
    }

    /** The sheet came to rest or its head changed height: what follows its head moves, and Back
     *  closes it while it's open. */
    fun onSheetChanged() {
        onSheetMoved()
        collapseOnBack.isEnabled = sheet.isVisible && panel.expanded && !editOpen()
    }
}
