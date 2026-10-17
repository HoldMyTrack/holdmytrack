package dev.holdmytrack.android.map

import dev.holdmytrack.android.panel.ActivitiesPanel
import dev.holdmytrack.android.panel.PanelState
import dev.holdmytrack.android.photos.PhotosController
import org.maplibre.android.maps.Style

/**
 * What the map reads again when activities change under it, as the web's `useDataRefresh.ts`: the
 * track tiles, the list and its days ([RangeController]), the open Story, the bands, the photos,
 * and Fog and Heatmap once re-rendered ([CoverageController]).
 */
class DataRefresh(
    private val range: RangeController,
    private val session: MapSession,
    private val panel: ActivitiesPanel,
    private val panelState: PanelState,
    /** The style once the user layers are on it. */
    private val style: () -> Style?,
    private val coverage: CoverageController,
    private val bands: PaceBands,
    private val photos: PhotosController,
) {
    private fun refreshTracks() {
        style()?.let { MapOverlays.refreshTracks(it, session.range) }
    }

    /** An activity taken out of the open Story: its track and row go, and its selection. The
     *  tile version moved, and the tiles follow. */
    fun afterRemovedFromStory(activityId: String) {
        if (panelState.focused == activityId) panel.clearFocus()
        photos.refresh(force = true)
        coverage.checkTileVersion()
        refreshTracks()
        range.reloadList()
    }

    /**
     * A Private location saved or deleted: the activities it touches are reprocessed. The list is
     * read at once, and everything again once the server drains, since a batch that finishes
     * before the first read never shows a row Pending.
     */
    fun afterPrivateLocations() {
        range.reloadList()
        coverage.watch {
            refreshTracks()
            range.reloadList()
            range.days.reload()
            bands.update(refetch = true)
            photos.refresh(force = true)
            panel.storiesTab.reloadOpen()
        }
    }

    /** A Story copy arrived (`docs/SPEC.md` FR-14.8): new activities, refreshed as after an upload. */
    fun afterStoryCopy() {
        refreshTracks()
        range.reloadList()
        range.days.reload()
        coverage.watch()
    }

    /** The Sync tab sent activities: the list, its days and the tiles catch up. */
    fun afterSync() {
        range.days.reload()
        range.reloadList()
        // Tracks refetch at once; the tile version moves only after the coverage render.
        refreshTracks()
        coverage.checkTileVersion()
    }

    /** The toolbar's Delete finished: tracks, list and days read again, and Fog and Heatmap
     *  watched until re-rendered. Deleted ids leave the selection with the reload. */
    fun afterDelete(ids: List<String>) {
        if (ids.isEmpty()) return
        refreshTracks()
        range.reloadList()
        range.days.reload()
        coverage.watch()
        panel.storiesTab.reloadOpen()
        photos.refresh(force = true)
    }
}
