package dev.holdmytrack.android.panel

import dev.holdmytrack.android.net.Activity

/**
 * What the Activities panel and the map agree on: the date range's activities, the two
 * filters, the checked group, the one selected (focused) activity, and the tracks hidden for
 * the session — the state the web keeps in `apps/web/src/map/MapView.tsx`, with the same rules.
 * Plain data and rules, no views, so it's what the unit tests exercise.
 *
 * Selection and the checked group are independent (`docs/SPEC.md` FR-5.5, FR-5.6): tapping a
 * row or a track *selects* it — at most one, bold on the map, flown to — and never touches a
 * checkbox; a checkbox only adds or removes its row from the group the toolbar acts on.
 */
class PanelState {

    var activities: List<Activity> = emptyList()
        private set

    var checked: Set<String> = emptySet()
        private set

    /** The selected activity, if any. */
    var focused: String? = null
        private set

    /** Tracks the user hid from the map with the toolbar's Show/Hide — this session only, as
     *  on the web: never sent to the server, and cleared by a new date range. */
    var hidden: Set<String> = emptySet()
        private set

    var excludedTypes: Set<String> = emptySet()
        private set

    var distanceFilter: DistanceRange? = null
        private set

    /** A fresh list for the same range — after a sync or an edit. Selection, the group and the
     *  hidden set keep whatever of theirs still exists. */
    fun setActivities(next: List<Activity>) {
        activities = next
        val ids = next.mapTo(HashSet()) { it.id }
        checked = checked intersect ids
        hidden = hidden intersect ids
        if (focused !in ids) focused = null
    }

    /** A new date range changes which rows exist, so everything built against the old one goes
     *  (`docs/SPEC.md` FR-6.6) — a stale DISTANCE band in particular could exclude everything. */
    fun resetForNewRange() {
        checked = emptySet()
        focused = null
        hidden = emptySet()
        excludedTypes = emptySet()
        distanceFilter = null
    }

    /** The rows the panel lists: the range's activities narrowed by TYPE and DISTANCE. */
    val listed: List<Activity>
        get() = activities.filter { ActivityFacets.passesFilters(it, excludedTypes, distanceFilter) }

    /** Every track the map shouldn't paint: filtered out, hidden, or Pending — all the same
     *  answer for the map (don't draw it, don't fly to it). Pending isn't folded into [hidden],
     *  so the user's own Show/Hide survives the reprocess. */
    val mapHidden: Set<String>
        get() = activities.filter {
            it.pending || it.id in hidden || !ActivityFacets.passesFilters(it, excludedTypes, distanceFilter)
        }.mapTo(HashSet()) { it.id }

    /** What the toolbar acts on: the checked group whenever anything is checked, else the
     *  selected row alone, else nothing. Checked wins rather than the two being joined, so a
     *  toolbar action never reaches further than what was ticked. */
    val targetIds: Set<String>
        get() = when {
            checked.isNotEmpty() -> checked
            focused != null -> setOf(focused!!)
            else -> emptySet()
        }

    /** [targetIds]' rows among the ones listed — none when every one is filtered out, which
     *  disables the toolbar the same as having no target at all. */
    val targets: List<Activity>
        get() {
            val ids = targetIds
            return listed.filter { it.id in ids }
        }

    fun focus(id: String) {
        focused = id
    }

    fun clearFocus() {
        focused = null
    }

    fun toggleChecked(id: String) {
        checked = if (id in checked) checked - id else checked + id
    }

    /** The master checkbox from unchecked or partial: every row listed — not the ones
     *  filtered out of sight. */
    fun checkAll() {
        checked = listed.mapTo(HashSet()) { it.id }
    }

    fun clearChecked() {
        checked = emptySet()
    }

    /** Checks every listed row that isn't and unchecks every one that is; a checked row
     *  filtered out of sight is dropped rather than kept checked where it can't be seen. */
    fun invertChecked() {
        checked = listed.filter { it.id !in checked }.mapTo(HashSet()) { it.id }
    }

    /** The toolbar's Show/Hide over its target: any of it hidden shows all of it, otherwise
     *  hides all of it — one tap always leaves the group in one state. */
    fun toggleTargetVisibility() {
        val target = targetIds
        hidden = if (target.any { it in hidden }) hidden - target else hidden + target
    }

    /** A toggle, so a type is always one tap from coming back without "Reset filters". */
    fun toggleType(type: String) {
        excludedTypes = if (type in excludedTypes) excludedTypes - type else excludedTypes + type
    }

    /** The TYPE dropdown's "All types": only ever re-includes everything. */
    fun includeAllTypes() {
        excludedTypes = emptySet()
    }

    /** Null is "any distance" — the band at the list's own ends is stored as null, so it
     *  doesn't count as an active filter. */
    fun setDistanceFilter(next: DistanceRange?) {
        distanceFilter = next
    }

    fun resetFilters() {
        excludedTypes = emptySet()
        distanceFilter = null
    }

    val hasActiveFilters: Boolean
        get() = excludedTypes.isNotEmpty() || distanceFilter != null
}
