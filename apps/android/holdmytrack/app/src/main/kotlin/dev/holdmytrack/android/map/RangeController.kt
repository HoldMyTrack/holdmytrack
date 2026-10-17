package dev.holdmytrack.android.map

import android.os.Bundle
import android.util.Log
import android.view.View
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.panel.ActivitiesPanel
import dev.holdmytrack.android.panel.PanelState
import org.maplibre.android.maps.Style
import java.time.LocalDate

/**
 * What the list and the tracks show, bound to the views: the slider over the account's activity
 * days ([ActivityDays]), the default range offered on each reload, a pick or a Story through
 * [MapSession], and the list read for whichever is shown, its Pending rows tracked ([PendingTracker]).
 */
class RangeController(
    dateFooter: View,
    private val session: MapSession,
    private val panelState: PanelState,
    private val panel: ActivitiesPanel,
    /** The style once the user layers are on it. */
    private val style: () -> Style?,
    private val bands: PaceBands,
    private val coverage: CoverageController,
    private val editOpen: () -> Boolean,
    private val recording: () -> Boolean,
    /** The panel's selection was reset: the map, the bands and the sheet's head follow. */
    private val onSelectionReset: () -> Unit,
    /** The days or their readiness changed: the sheet's head follows. */
    private val onDaysChanged: () -> Unit,
    private val fly: (List<Activity>) -> Unit,
) {
    val days = ActivityDays(DateRangeSlider.WINDOW_DAYS, ::onActivityDaysChanged)

    private val slider = DateRangeSlider(
        dateFooter,
        onPan = days::panBy,
        onShift = days::shift,
        canShift = days::canShift,
    ) { range ->
        session.pickRange(range)
        showRange(reset = true)
    }

    /** Reprocesses running, and the list read again while any is. */
    val pending = PendingTracker(host = { dateFooter }, reload = ::reloadList)

    /** A View on map's activity, to select once its range's list has it ([viewActivity]). */
    private var pendingFocusId: String? = null

    private val unreachable = dateFooter.resources.getString(R.string.map_unreachable)

    /** The slider's window moved or reloaded; a reload re-derives the default range. */
    private fun onActivityDaysChanged(reloaded: Boolean) {
        slider.setDays(days.visibleDays, days.canPanEarlier, days.canPanLater)
        if (reloaded) offerDefault()
        onDaysChanged()
    }

    /** The five most recent activity days, through today (`docs/SPEC.md` FR-6.1), unless the user
     *  picked a range; held while the Edit window is open, and offered again when it closes. */
    fun offerDefault() {
        if (!days.ready) return
        val range = DefaultRange.of(days.visibleDays.map { it.date }, days.earliest, LocalDate.now().toString())
        if (session.offerDefault(range, editOpen = editOpen())) showRange(reset = false)
    }

    /** Draws the tracks for the session's range and lists its activities; with [reset], the
     *  panel's emptied selection reaches the map too (`MapSession.pickRange`). */
    private fun showRange(reset: Boolean) {
        val range = session.range ?: return
        slider.value = range
        if (reset) onSelectionReset()
        style()?.let { MapOverlays.setTrackRange(it, range) }
        reloadList()
    }

    /** Lists the open Story's activities, else the range's, in the panel; frames every drawn one
     *  when the session asked for a flight. */
    fun reloadList() {
        val range = session.range
        val story = session.storyId
        if (story == null && range == null) return
        panel.setLoading()
        HoldMyTrackApi.activities(range?.from.takeIf { story == null }, range?.to.takeIf { story == null }, story) { result ->
            // A later pick, or another Story, owns the list now.
            if (story != session.storyId || (story == null && range != session.range)) return@activities
            result.onSuccess { activities ->
                panel.setActivities(activities)
                trackPending(activities)
                pendingFocusId?.takeIf { id -> activities.any { it.id == id } }?.let { id ->
                    pendingFocusId = null
                    panel.focusFromMap(id)
                }
                // A recording started since owns the camera.
                if (session.consumeFly() && !recording()) fly(activities.filter { !it.pending })
            }.onFailure { failure ->
                Log.w(TAG, "could not load the activity list", failure)
                session.consumeFly()
                panel.setError(unreachable)
                // A failed read doesn't end the polling: a Pending row still has to be seen finishing.
                pending.readFailed()
            }
        }
    }

    /**
     * Pending rows ([PendingTracker]): a row starting or stopping Pending refetches the track tiles
     * and watches Fog and Heatmap until re-rendered; a finished one also reloads the days.
     */
    private fun trackPending(activities: List<Activity>) {
        val change = pending.track(activities)
        // The selected activity's reprocess landed: its metrics describe the old points.
        if (panelState.focused in change.landed || panelState.focused in change.left) bands.update(refetch = true)
        if (change.started || change.finished) {
            style()?.let { MapOverlays.refreshTracks(it, session.range) }
            coverage.watch()
        }
        if (change.finished) {
            days.reload()
            panel.storiesTab.reloadOpen()
        }
    }

    /**
     * A Sync history row's View on map: the activity selected. When its [day] (its local date where
     * it was recorded) is outside the range, the range becomes that day, as if picked, and the
     * selection waits for that list.
     */
    fun viewActivity(activityId: String, day: String) {
        val range = session.range
        if (range != null && day >= range.from && day <= range.to && panelState.activities.any { it.id == activityId }) {
            panel.focusFromMap(activityId)
            return
        }
        pendingFocusId = activityId
        session.viewDay(day)
        showRange(reset = true)
    }

    /**
     * Opens Story [id]: the panel's state resets as for a new range, the tracks and the list
     * become the Story's activities, and the camera fits them.
     */
    fun enterStory(id: String) {
        session.enterStory(id)
        onSelectionReset()
        panel.showStories()
        panel.storiesTab.setOpen(id)
        style()?.let { MapOverlays.setTrackStory(it, id, null) }
        reloadList()
    }

    /** Closes the open Story: back to the range, the panel's state reset. The camera stays. */
    fun exitStory() {
        session.exitStory()
        panel.storiesTab.setOpen(null)
        onSelectionReset()
        style()?.let { MapOverlays.setTrackStory(it, null, session.range) }
        if (session.range != null) reloadList() else panel.setActivities(emptyList())
    }

    /** A range carried through a recreation. */
    fun restore(saved: Bundle?) {
        val from = saved?.getString(STATE_RANGE_FROM) ?: return
        val to = saved.getString(STATE_RANGE_TO) ?: return
        session.restore(DateRange(from, to), saved.getBoolean(STATE_RANGE_CHOSEN))
        slider.value = session.range
    }

    /** The panel comes back on its Activities tab; an open Story never changed the range. */
    fun save(out: Bundle) {
        val range = session.range ?: return
        out.putString(STATE_RANGE_FROM, range.from)
        out.putString(STATE_RANGE_TO, range.to)
        out.putBoolean(STATE_RANGE_CHOSEN, session.userPicked)
    }

    fun release() = slider.release()

    private companion object {
        const val TAG = "HoldMyTrack"
        const val STATE_RANGE_FROM = "range_from"
        const val STATE_RANGE_TO = "range_to"
        const val STATE_RANGE_CHOSEN = "range_chosen"
    }
}
