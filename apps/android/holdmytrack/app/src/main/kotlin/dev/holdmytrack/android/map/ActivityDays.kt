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

    /**
     * The range-shift buttons ([shiftRange]): [range] moved its own length in activity days,
     * with the window moved just enough to show it — or null when there's nothing further that
     * way, or the days it needs are still loading (the next page is asked for; ask again).
     */
    fun shift(range: DateRange, dir: Int): DateRange? {
        val dates = days.map { it.date }
        val next = when (val result = shiftRange(dates, hasEarlier, range, dir)) {
            is RangeShift.Shifted -> result.range
            RangeShift.Load -> {
                extendEarlier()
                return null
            }
            RangeShift.None -> return null
        }
        // Move the window only as far as it takes to show the new range; one longer than the
        // window shows its leading edge, the end it moved toward.
        val first = dates.indexOf(next.from)
        val last = dates.indexOf(next.to)
        val start = start
        var left = when {
            last - first + 1 > windowDays -> if (dir < 0) first else last - windowDays + 1
            first < start -> first
            last >= start + windowDays -> last - windowDays + 1
            else -> start
        }
        left = left.coerceIn(0, maxStart)
        if (left != start) {
            anchor = if (left >= maxStart) null else dates[left]
            changed(reloaded = false)
        }
        return next
    }

    /** Whether [shift] has anywhere to go — true while the days it needs are only unloaded. */
    fun canShift(range: DateRange, dir: Int) =
        shiftRange(days.map { it.date }, hasEarlier, range, dir) != RangeShift.None

    /**
     * Re-reads from the newest end — on every return to the map, since Sync or a recording may
     * have added days. The window stays where it was: its anchor still names the same day, and
     * when that day is older than the fresh page, the page before it is fetched ([changed]) and
     * the window lands back on it. Pinned to the newest end, it stays pinned there.
     */
    fun reload() {
        val gen = ++generation
        HoldMyTrackApi.activityDayPage(PAGE_SIZE, null) { result ->
            if (gen != generation) return@activityDayPage
            result.onSuccess { page ->
                days = page.days
                earliest = page.earliest
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
