package dev.holdmytrack.android.map

import dev.holdmytrack.android.panel.PanelState

/**
 * Which activities the map's list and tracks show, a date range or a Story, and the rules between
 * that scope, the panel's selection ([panel]) and the camera: a picked range or a Story resets the
 * selection, the default never replaces a picked range, and only a picked range or an opened Story
 * moves the camera onto the next list ([consumeFly]). The web's `session.ts`; both run
 * `testdata/map-session`.
 */
class MapSession(private val panel: PanelState) {

    /** The date range; null until its default is known. */
    var range: DateRange? = null
        private set

    /** Whether the user picked [range]; until then the default may replace it. */
    var userPicked = false
        private set

    /** The open Story (`docs/SPEC.md` FR-14.6): the list and tracks are its activities, [range]
     *  kept for when it closes. */
    var storyId: String? = null
        private set

    /** The next list loaded moves the camera onto what it draws. */
    var flyRequest = false
        private set

    /** The slider picked [next]: a new set of rows, so the selection resets (`docs/SPEC.md` FR-6.6). */
    fun pickRange(next: DateRange) {
        range = next
        userPicked = true
        flyRequest = true
        panel.resetForNewRange()
    }

    /** The default range re-derived: taken, and true, unless the user picked one, it's the same,
     *  or the Edit window is open, since sliding past the edited activity would empty its row. */
    fun offerDefault(next: DateRange, editOpen: Boolean = false): Boolean {
        if (userPicked || editOpen || next == range) return false
        range = next
        return true
    }

    /** View on map for an activity outside the range: its own day, picked, without the range's
     *  flight, since the selection flies to the activity. */
    fun viewDay(day: String) {
        pickRange(DateRange(day, day))
        flyRequest = false
    }

    fun enterStory(id: String) {
        storyId = id
        flyRequest = true
        panel.resetForNewRange()
    }

    /** Back to the range; the camera stays. */
    fun exitStory() {
        storyId = null
        flyRequest = false
        panel.resetForNewRange()
    }

    /** Whether a flight was requested, which this answer uses up. */
    fun consumeFly(): Boolean = flyRequest.also { flyRequest = false }

    /** A range carried through a recreation. */
    fun restore(saved: DateRange, picked: Boolean) {
        range = saved
        userPicked = picked
    }
}
