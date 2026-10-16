package dev.holdmytrack.android.photos

import dev.holdmytrack.android.map.MapOverlays

/** How photo markers are drawn at a zoom ([PhotoClusters.markerSize]). */
enum class MarkerSize { HIDDEN, SMALL, FULL }

/** A marker's place on screen, in pixels. */
data class ScreenPoint(val id: String, val x: Float, val y: Float)

/** One marker's photos, in the order given (route order) — the first being the one it shows,
 *  and where it sits. */
data class PhotoCluster(val ids: List<String>, val x: Float, val y: Float)

/**
 * Grouping photo markers that would overlap on screen (`apps/android/docs/SPEC.md` FR-2.10), the web's
 * `apps/web/src/map/photoClusters.ts` — the photos of a whole trip seen zoomed out, or several
 * taken at one spot. Done in screen pixels after each move of the map, since what overlaps
 * depends on the zoom; tens of photos per route make that cheap.
 */
object PhotoClusters {

    /** How close two markers' centres may be, in dp, before they're one group: a little under
     *  a marker's width (36dp), so markers that would merely touch stay apart. */
    const val RADIUS_DP = 32f

    /** The same for a small marker (22dp). */
    const val SMALL_RADIUS_DP = 18f

    /** From this zoom up a marker is drawn full size; below it, small, so the tracks — thin
     *  lines across a whole region at that scale — still show under the photos. */
    const val FULL_ZOOM = 10.0

    /** How photo markers are drawn at [zoom], as the web's `photoMarkerSize`: not at all where
     *  tracks aren't either (below [MapOverlays.TRACKS_MIN_ZOOM]) — a photo would sit over a
     *  continent with no route under it — small below [FULL_ZOOM], full size from it. */
    fun markerSize(zoom: Double): MarkerSize = when {
        zoom < MapOverlays.TRACKS_MIN_ZOOM -> MarkerSize.HIDDEN
        zoom < FULL_ZOOM -> MarkerSize.SMALL
        else -> MarkerSize.FULL
    }

    /**
     * Greedy grouping in the given order: each point joins the first group whose anchor — its
     * first point, where the group's marker sits — is within [radius], or starts a group of its
     * own. Anchoring on a real point rather than a moving centroid keeps a group's marker on the
     * route and doesn't chain a long run of close photos into one group.
     */
    fun cluster(points: List<ScreenPoint>, radius: Float): List<PhotoCluster> {
        val anchors = ArrayList<ScreenPoint>()
        val members = ArrayList<MutableList<String>>()
        for (p in points) {
            val near = anchors.indexOfFirst { (it.x - p.x) * (it.x - p.x) + (it.y - p.y) * (it.y - p.y) <= radius * radius }
            if (near >= 0) {
                members[near] += p.id
            } else {
                anchors += p
                members += mutableListOf(p.id)
            }
        }
        return anchors.mapIndexed { i, a -> PhotoCluster(members[i], a.x, a.y) }
    }
}
