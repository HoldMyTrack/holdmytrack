package dev.holdmytrack.android.panel

/** The toolbar's rules [PanelState] applies, as the web's `selection.ts`; both run `testdata/map-session`. */
object PanelRules {
    /** What the toolbar acts on: the checked group whenever anything is checked, else the
     *  selected row alone, else nothing. Checked wins so an action never reaches past what was ticked. */
    fun targetIds(checked: Set<String>, focused: String?): Set<String> = when {
        checked.isNotEmpty() -> checked
        focused != null -> setOf(focused)
        else -> emptySet()
    }

    /** Show/Hide over [target]: any of it hidden shows all of it, otherwise hides all of it, so
     *  one tap always leaves the group in one state. */
    fun toggledVisibility(hidden: Set<String>, target: Set<String>): Set<String> =
        if (target.any { it in hidden }) hidden - target else hidden + target
}
