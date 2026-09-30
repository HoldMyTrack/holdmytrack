package dev.holdmytrack.android.map

import android.util.Log
import dev.holdmytrack.android.net.ActivityDay
import dev.holdmytrack.android.net.HoldMyTrackApi

/** A selected date range, both ends `YYYY-MM-DD` and inclusive — compared as strings, which
 *  that format orders correctly. */
data class DateRange(val from: String, val to: String)

/**
 * The date-range slider's window over the account's history, paged by *days that have
 * activity* — the Android counterpart of the web's `apps/web/src/ui/useActivityDays.ts`, sized
 * to the phone slider's window as the web's is on a phone.
 *
 * One slot per day the user recorded something, none for the days between, so a quiet month
 * costs no room. Pages come from `GET /v1/activities/histogram?days=&before=`, newest first, so
 * what's loaded is one contiguous run of activity days ending at the most recent one, grown by
 * prepending. The window's position is an anchor date (its leftmost day), not an index, since
 * prepending a page shifts every index; null means pinned to the newest end.
 *
 * Nothing here touches the selection: panning changes which days are in view and that is all.
 */
class ActivityDays(private val windowDays: Int, private val onChange: (reloaded: Boolean) -> Unit) {

    private var days: List<ActivityDay> = emptyList()

    /** The account's first activity day, or null — until the first page lands, and for good
     *  for an account with no activity. */
    var earliest: String? = null
        private set

    /** True once a first page has come back, however empty. */
    var ready = false
        private set

    private var anchor: String? = null

    /** One extend request at a time: a second would be anchored at the same day and prepend
     *  the same page twice. */
    private var extending = false

    /** Days a pan asked for that weren't loaded yet, carried until they are. */
    private var carry = 0

    /** Bumped by [reload], so an answer to an older request is dropped. */
    private var generation = 0

    private val maxStart get() = maxOf(0, days.size - windowDays)

    private val start: Int
        get() {
            val at = anchor ?: return maxStart
            val index = days.indexOfFirst { it.date >= at }
            return minOf(if (index == -1) maxStart else index, maxStart)
        }

    /** Whether there is history behind what's loaded — a short page is what running out
     *  looks like, and then the first loaded day *is* [earliest]. */
    private val hasEarlier: Boolean
        get() {
            val first = days.firstOrNull()?.date ?: return false
            val earliest = earliest ?: return false
            return first > earliest
        }

    /** The days in the window: consecutive activity days, ascending. */
    val visibleDays: List<ActivityDay> get() = days.subList(start, minOf(days.size, start + windowDays)).toList()

    val canPanEarlier get() = start > 0 || hasEarlier
    val canPanLater get() = start < maxStart

    /** Re-reads from the newest end, back to the window's opening position. */
    fun reload() {
        val gen = ++generation
        HoldMyTrackApi.activityDayPage(PAGE_SIZE, null) { result ->
            if (gen != generation) return@activityDayPage
            result.onSuccess { page ->
                days = page.days
                earliest = page.earliest
                anchor = null
                carry = 0
                extending = false
                ready = true
                changed(reloaded = true)
            }.onFailure { Log.w(TAG, "could not load activity days", it) }
        }
    }

    /** Moves the window by whole activity days — negative is toward the past. */
    fun panBy(delta: Int) {
        val target = start + delta
        val next = target.coerceIn(0, maxStart)
        carry = if (target < 0 && hasEarlier) target else 0
        anchor = if (next >= maxStart) null else days.getOrNull(next)?.date
        changed(reloaded = false)
    }

    private fun changed(reloaded: Boolean) {
        // Loads more history once the window is within one window's width of the loaded edge,
        // so Earlier normally lands on days that are already here.
        if (hasEarlier && start <= windowDays) extendEarlier()
        onChange(reloaded)
    }

    private fun extendEarlier() {
        val oldest = days.firstOrNull()?.date ?: return
        if (extending) return
        extending = true
        val gen = generation
        HoldMyTrackApi.activityDayPage(PAGE_SIZE, oldest) { result ->
            if (gen != generation) return@activityDayPage
            extending = false
            result.onSuccess { page ->
                if (days.firstOrNull()?.date == oldest) days = page.days + days
                earliest = page.earliest
                // A pan that was waiting on this page settles now; the anchor still names the
                // same day, so the window doesn't jump.
                val owed = carry
                if (owed < 0) {
                    carry = 0
                    panBy(owed)
                } else {
                    changed(reloaded = false)
                }
            }.onFailure { Log.w(TAG, "could not load earlier activity days", it) }
        }
    }

    private companion object {
        const val TAG = "HoldMyTrack"

        /** Activity days per request — the web's floor, far more than a window, so paging back
         *  rarely waits on the network. */
        const val PAGE_SIZE = 240
    }
}
