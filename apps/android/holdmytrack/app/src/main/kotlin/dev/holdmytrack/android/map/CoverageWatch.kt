package dev.holdmytrack.android.map

import android.os.Handler
import android.os.Looper
import dev.holdmytrack.android.net.HoldMyTrackApi

/**
 * Keeps Fog and Heatmap current after a delete or a reprocess — the web's
 * `apps/web/src/map/useCoverageRefresh.ts`. Both are re-rendered server-side by a queued job,
 * and their tile URLs never change by themselves, so the map would otherwise go on showing
 * coverage from before the change.
 *
 * [watch] polls `GET /v1/coverage/status` every 2 seconds and calls [onRefetch] once the
 * account has no coverage-changing job left — and also mid-job, whenever the status's
 * `version` moves, since a reprocess renders once with its Pending activities left out and
 * again once they're back. Each call restarts the watch rather than joining a running one: a
 * second delete can enqueue its job just after a read came back "done". Gives up waiting
 * after about three minutes and refetches anyway — a failed render leaves no job behind, so
 * that only bounds one stuck in the queue.
 */
class CoverageWatch(private val onRefetch: () -> Unit) {

    private val handler = Handler(Looper.getMainLooper())
    private var generation = 0
    private var polls = 0

    /** The status `version` the map is showing; null until a watch has read one. */
    private var shownVersion: Long? = null

    fun watch() {
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
            // A failed read ends the watch quietly; the next change starts another.
            val status = result.getOrNull() ?: return@coverageStatus
            polls += 1
            if (status.rendering && polls < MAX_POLLS) {
                val shown = shownVersion
                if (shown == null) {
                    shownVersion = status.version
                } else if (status.version != shown) {
                    shownVersion = status.version
                    onRefetch()
                }
                handler.postDelayed({ check(watching) }, POLL_MS)
                return@coverageStatus
            }
            shownVersion = status.version
            onRefetch()
        }
    }

    private companion object {
        const val POLL_MS = 2_000L
        const val MAX_POLLS = 90
    }
}
