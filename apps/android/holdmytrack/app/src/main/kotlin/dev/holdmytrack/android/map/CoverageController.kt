package dev.holdmytrack.android.map

import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import org.maplibre.android.maps.Style

/**
 * Keeps the tiles current with the account: Fog and Heatmap fetched again once the server has
 * re-rendered them after a delete or a reprocess ([CoverageWatch]), and every tile again when the
 * account's tile version moved while the app was away.
 */
class CoverageController(
    /** The style once the user layers are on it. */
    private val style: () -> Style?,
    private val range: () -> DateRange?,
    /** [rendering] changed: the notice that Fog and Heatmap are still being updated follows. */
    private val onRenderingChanged: () -> Unit,
) {
    /** What the watch last read: the account's Fog and Heatmap are still being worked on. */
    var rendering = false
        private set

    private val coverage = CoverageWatch(
        onRendering = {
            rendering = it
            onRenderingChanged()
        },
        onRefetch = { style()?.let(CoverageLayers::refresh) },
    )

    /** Watches the account's re-render until it's done, then refetches Fog and Heatmap and runs [onDone]. */
    fun watch(onDone: (() -> Unit)? = null) = coverage.watch(onDone)

    fun stop() = coverage.stop()

    /**
     * The overlays attach at the stored tile version ([Session.tileVersion]), so cached tiles draw
     * at once. This reads the current version and refetches every tile only if it moved while the
     * app was away.
     */
    fun checkTileVersion() {
        HoldMyTrackApi.coverageStatus { result ->
            val status = result.getOrNull() ?: return@coverageStatus
            // Still rendering: watched until done, with the notice meanwhile and a refetch at the end.
            if (status.rendering) coverage.watch()
            if (status.tileVersion.isEmpty() || status.tileVersion == Session.tileVersion) return@coverageStatus
            Session.tileVersion = status.tileVersion
            style()?.let {
                CoverageLayers.refresh(it)
                MapOverlays.refreshTracks(it, range())
                // Loading places or bike paths bumps the version too.
                MapSpots.refresh(it)
                MapBikePaths.refresh(it)
            }
        }
    }
}
