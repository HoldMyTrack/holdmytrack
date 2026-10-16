package dev.holdmytrack.android.sync

import dev.holdmytrack.android.map.SyncCandidatesOverlay
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.Style

/** What the Sync tab has the map draw (`map/SyncCandidatesOverlay`), kept to draw again after a
 *  style reload, which drops the layers. */
class SyncCandidatesController(
    /** The style once the user layers are on it. */
    private val style: () -> Style?,
) {
    private var candidates: List<Candidate> = emptyList()
    private var highlight: String? = null

    /** The Sync tab's lines over the map, or none as it leaves. */
    fun set(next: List<Candidate>, highlighted: String?) {
        candidates = next
        highlight = highlighted
        draw()
    }

    /** Draws them again, onto a new style. */
    fun draw() {
        val loaded = style() ?: return
        if (candidates.isEmpty()) SyncCandidatesOverlay.clear(loaded) else SyncCandidatesOverlay.set(loaded, candidates, highlight)
    }

    /** The candidate's key under a tap, if any. */
    fun keyAt(instance: MapLibreMap, point: LatLng, density: Float): String? =
        SyncCandidatesOverlay.keyAt(instance, instance.projection.toScreenLocation(point), density)
}
