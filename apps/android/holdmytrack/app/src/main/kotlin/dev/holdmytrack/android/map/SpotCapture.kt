package dev.holdmytrack.android.map

/**
 * Capture mode's count (`apps/android/docs/SPEC.md` FR-2.8, ADR-0023): fed the phone's
 * location fixes, it says whether the user is still on the way to the spot, holding inside it,
 * or has held for [HOLD_MS] and captured it. Only a fix accurate to [MAX_ACCURACY_M] counts —
 * a coarse fix can land inside a spot its owner is only walking past. A counted fix outside the
 * area starts the hold again. The hold only runs on fixes: a gap longer than [MAX_GAP_MS] (the
 * app was in the background, or GPS dropped out) doesn't count towards it.
 *
 * No Android in it, so it's tested on the JVM (`SpotCaptureTest`).
 */
class SpotCapture(private val area: SpotArea, private val anchor: GeoPoint) {

    sealed interface State {
        /** No fix accurate enough yet. */
        data object Locating : State

        /** Outside the area: [distanceM] to its edge, [bearing] (degrees from north) to it. */
        data class Away(val distanceM: Double, val bearing: Double, val from: GeoPoint, val to: GeoPoint) : State

        /** Inside, held for [heldMs] of [HOLD_MS] so far, at [at] last. */
        data class Holding(val heldMs: Long, val at: GeoPoint) : State

        /** Held long enough; [at] is the last fix inside, what the server checks the capture by. */
        data class Captured(val at: GeoPoint) : State
    }

    var state: State = State.Locating
        private set

    /** When the hold's last counted fix was taken, or null while not holding. */
    private var lastInsideMs: Long? = null
    private var heldMs = 0L

    /** One location fix: [timeMs] is its own time (elapsed-realtime or wall clock, as long as
     *  it's the same clock throughout). Returns the new [state]. */
    fun onFix(point: GeoPoint, accuracyM: Float?, timeMs: Long): State {
        if (state is State.Captured) return state
        if (accuracyM == null || accuracyM > MAX_ACCURACY_M) return state
        if (SpotGeometry.contains(area, point)) {
            val last = lastInsideMs
            if (last != null && timeMs > last) heldMs += (timeMs - last).coerceAtMost(MAX_GAP_MS)
            lastInsideMs = timeMs
            state = if (heldMs >= HOLD_MS) State.Captured(point) else State.Holding(heldMs, point)
        } else {
            lastInsideMs = null
            heldMs = 0
            val to = SpotGeometry.nearestPoint(area, point) ?: anchor
            state = State.Away(SpotGeometry.distanceM(area, point), SpotGeometry.bearing(point, to), point, to)
        }
        return state
    }

    /** The capture was refused (the server saw the position outside): start over from [Locating]. */
    fun reset() {
        state = State.Locating
        lastInsideMs = null
        heldMs = 0
    }

    companion object {
        const val HOLD_MS = 30_000L

        /** A fix less accurate than this is ignored. */
        const val MAX_ACCURACY_M = 25f

        /** The most one gap between fixes inside can add to the hold. Fixes come every second;
         *  a longer gap means the app wasn't reading them. */
        const val MAX_GAP_MS = 3_000L
    }
}
