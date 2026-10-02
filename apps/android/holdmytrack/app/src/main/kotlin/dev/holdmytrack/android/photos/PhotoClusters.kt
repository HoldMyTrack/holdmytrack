package dev.holdmytrack.android.photos

/** A marker's place on screen, in pixels. */
data class ScreenPoint(val id: String, val x: Float, val y: Float)

/** One marker's photos, in the order given (route order) — the first being the one it shows,
 *  and where it sits. */
data class PhotoCluster(val ids: List<String>, val x: Float, val y: Float)

/**
 * Grouping photo markers that would overlap on screen (`docs/SPEC.md` FR-16.7), the web's
 * `apps/web/src/map/photoClusters.ts` — the photos of a whole trip seen zoomed out, or several
 * taken at one spot. Done in screen pixels after each move of the map, since what overlaps
 * depends on the zoom; tens of photos per route make that cheap.
 */
object PhotoClusters {

    /** How close two markers' centres may be, in dp, before they're one group: a little under
     *  a marker's width (36dp), so markers that would merely touch stay apart. */
    const val RADIUS_DP = 32f

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
