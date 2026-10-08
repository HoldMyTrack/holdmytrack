package dev.holdmytrack.android.map

import android.os.Handler
import android.os.Looper
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session

/**
 * Keeps Fog and Heatmap current after a delete or a reprocess — the web's
 * `apps/web/src/map/useCoverageRefresh.ts`. Both are re-rendered server-side by a queued job,
 * and their tile URLs never change by themselves, so the map would otherwise go on showing
 * coverage from before the change.
 *
 * [watch] polls `GET /v1/coverage/status` and calls [onRefetch] once the account has no
 * coverage-changing job left — and also mid-job, whenever the status's `version` moves, since a
 * reprocess renders once with its Pending activities left out and again once they're back. Each
 * call restarts the watch rather than joining a running one: a second delete can enqueue its job
 * just after a read came back "done". Reads go every 2 seconds for about three minutes, then
 * every 10, a failed read included, until one finds nothing left: a big import can keep the
 * server's worker busy far longer than three minutes, and only that last read refetches.
 * [onRendering] gets every read's `rendering`, for the map's "still being updated" notice.
 */
class CoverageWatch(private val onRendering: (Boolean) -> Unit, private val onRefetch: () -> Unit) {

    private val handler = Handler(Looper.getMainLooper())
    private var generation = 0
    private var polls = 0

    /** The status `version` the map is showing; null until a watch has read one. */
    private var shownVersion: Long? = null

    /** Runs once the account has no coverage-changing job left — for a change that moves more
     *  than coverage (a Private location reprocesses whole activities) and has no other way
     *  of telling when the server is done. A later [watch] without one keeps it. */
    private var onDone: (() -> Unit)? = null

    fun watch(onDone: (() -> Unit)? = null) {
        if (onDone != null) this.onDone = onDone
        generation += 1
        polls = 0
        handler.removeCallbacksAndMessages(null)
        check(generation)
    }

    fun stop() {
        generation += 1
        handler.removeCallbacksAndMessages(null)
    }

    private fun check(watching: Int) {
        HoldMyTrackApi.coverageStatus { result ->
            if (watching != generation) return@coverageStatus
            polls += 1
            val status = result.getOrNull()
            if (status == null) {
                handler.postDelayed({ check(watching) }, delay())
                return@coverageStatus
            }
            onRendering(status.rendering)
            if (status.rendering) {
                val shown = shownVersion
                if (shown == null) {
                    shownVersion = status.version
                } else if (status.version != shown) {
                    shownVersion = status.version
                    Session.tileVersion = status.tileVersion
                    onRefetch()
                }
                handler.postDelayed({ check(watching) }, delay())
                return@coverageStatus
            }
            shownVersion = status.version
            Session.tileVersion = status.tileVersion
            onRefetch()
            val done = onDone
            onDone = null
            done?.invoke()
        }
    }

    /** The web's `coveragePoll.ts`: every 2 seconds for the first [FAST_POLLS] reads, then every 10. */
    private fun delay() = if (polls < FAST_POLLS) FAST_POLL_MS else SLOW_POLL_MS

    private companion object {
        const val FAST_POLL_MS = 2_000L
        const val FAST_POLLS = 90
        const val SLOW_POLL_MS = 10_000L
    }
}
