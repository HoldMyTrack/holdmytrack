package dev.holdmytrack.android.map

import android.annotation.SuppressLint
import android.os.Handler
import android.os.Looper
import android.view.MotionEvent
import android.view.View
import android.widget.TextView
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.ActivityDay
import java.time.LocalDate

/**
 * The map's date-range control, at the top of the Activities sheet: the selected range as a
 * heading, ‹ › beside it, and under them the day scrubber ([DayScrubberView]) — the web's phone
 * slider (`apps/web/src/ui/DateRangeSlider.tsx`) redrawn as a bar per day.
 *
 * **Activity days, not calendar days.** The scrubber is a window of [WINDOW_DAYS] consecutive
 * days that have activity ([ActivityDays]); the days between take no room.
 *
 * **Handles sit on slot boundaries.** The start handle marks where the first selected day begins
 * and the end handle where the last one ends, so a one-day selection has its handles one slot
 * apart and they can never be dragged closer than that.
 *
 * **Swiping.** A swipe across the scrubber, away from the handles, scrolls the window a day per
 * slot the finger moves, the selection left as it is ([onSwipe]): handles scroll out of view and
 * back like any day, their dates staying in the heading.
 *
 * **Paging.** A handle held past the scrubber's edge moves the window [STEP_DAYS] that way,
 * repeating while it's held there, and pulls the handle along with it — how a selection grows
 * past the window. The far handle may scroll out of view; its date stays in the heading. The pull
 * lands once the moved window is in (the days before it may still be loading), so a gesture's
 * commit waits for that too.
 *
 * **Range shift.** ‹ › move the whole selection by its own length in activity days
 * ([shiftRange]), the window following. A tap commits; a hold repeats and commits on release.
 *
 * Drags and held buttons render from a local draft and commit through [onChange] only on
 * release, so the map's tracks aren't re-requested for every day passed.
 */
class DateRangeSlider(
    root: View,
    private val onPan: (delta: Int) -> Unit,
    private val onShift: (range: DateRange, dir: Int) -> DateRange?,
    private val canShift: (range: DateRange, dir: Int) -> Boolean,
    private val onChange: (DateRange) -> Unit,
) {
    private val track: DayScrubberView = root.findViewById(R.id.date_range_track)
    private val shiftEarlier: View = root.findViewById(R.id.date_range_shift_earlier)
    private val shiftLater: View = root.findViewById(R.id.date_range_shift_later)
    private val label: TextView = root.findViewById(R.id.date_range_label)

    private var days: List<ActivityDay> = emptyList()
    private var canPanEarlier = false
    private var canPanLater = false

    /** The committed selection; null until the map has one. */
    var value: DateRange? = null
        set(next) {
            field = next
            draft = null
            render()
        }

    private var draft: DateRange? = null
    private val current get() = draft ?: value

    private enum class Knob { START, END }

    /** The knob a press on the track is carrying, until release. */
    private var dragging: Knob? = null

    /** A pan asked for and not yet in view, the knobs it pulls along, and whether the gesture
     *  has ended and should commit once it lands. */
    private class Pan(var start: Boolean, var end: Boolean, var commit: Boolean)
    private var pan: Pan? = null

    private val handler = Handler(Looper.getMainLooper())
    /** The held button's step, until release. */
    private var repeatAction: (() -> Boolean)? = null
    private val repeat = object : Runnable {
        override fun run() {
            val action = repeatAction ?: return
            // Reaching the end disables the button, and a disabled button gets no ACTION_UP —
            // so the hold ends (and commits) here instead.
            if (!action()) return stopRepeat()
            handler.postDelayed(this, REPEAT_INTERVAL_MS)
        }
    }

    private val locale = root.resources.configuration.locales[0]

    /** The way a handle held past an edge is paging the window, until it's moved back in. */
    private var edgePan = 0
    private val edgeRepeat = object : Runnable {
        override fun run() {
            if (edgePan == 0 || !step(edgePan)) return
            handler.postDelayed(this, EDGE_REPEAT_MS)
        }
    }

    init {
        setUpRepeatButton(shiftEarlier) { shiftStep(-1) }
        setUpRepeatButton(shiftLater) { shiftStep(1) }
        track.onPress = ::onTrackPress
        track.onDrag = ::onTrackDrag
        track.onRelease = ::onTrackRelease
        track.onSwipe = ::onSwipe
    }

    /** The window has changed — paged, loaded further back, or reloaded. */
    fun setDays(days: List<ActivityDay>, canPanEarlier: Boolean, canPanLater: Boolean) {
        val moved = days.firstOrNull()?.date != this.days.firstOrNull()?.date ||
            days.lastOrNull()?.date != this.days.lastOrNull()?.date
        this.days = days
        this.canPanEarlier = canPanEarlier
        this.canPanLater = canPanLater
        if (moved) landPan()
        render()
    }

    // ---- Dates ↔ slot boundaries --------------------------------------------------------
    // Boundary b is where slot b begins (0..n). A date before the window is -1 and one after
    // it is n + 1 — off-screen — unless there's no history on that side, where it sits on the
    // edge: today with nothing recorded yet today still has its end knob on the right edge.

    private fun startOf(from: String): Int {
        val first = days.firstOrNull()?.date ?: return 0
        val n = days.size
        if (from < first) return if (canPanEarlier) -1 else 0
        val index = days.indexOfFirst { it.date >= from }
        return if (index == -1) (if (canPanLater) n + 1 else n) else index
    }

    private fun endOf(to: String): Int {
        val first = days.firstOrNull()?.date ?: return 0
        val last = days.last().date
        val n = days.size
        if (to > last) return if (canPanLater) n + 1 else n
        if (to < first) return if (canPanEarlier) -1 else 0
        return days.count { it.date <= to }
    }

    /** Moves one knob to boundary [b], a slot from the other (pushing it if needed). A knob
     *  that ends up where it already was keeps its own date, which may lie off-screen or in a
     *  gap between activity days — only a knob that actually moved takes a slot's date. */
    private fun moveKnob(knob: Knob, b: Int, sel: DateRange): DateRange {
        val n = days.size
        if (n == 0) return sel
        val s0 = startOf(sel.from)
        val e0 = endOf(sel.to)
        var s = s0
        var e = e0
        if (knob == Knob.START) {
            s = b.coerceIn(0, n - 1)
            e = maxOf(e0, s + 1)
        } else {
            e = b.coerceIn(1, n)
            s = minOf(s0, e - 1)
        }
        return DateRange(
            from = if (s == s0) sel.from else days[s].date,
            to = if (e == e0) sel.to else days[e - 1].date,
        )
    }

    private fun commit(next: DateRange?) {
        draft = null
        val committed = value
        if (next != null && next != committed) {
            value = next
            onChange(next)
        } else {
            render()
        }
    }

    // ---- Earlier / Later ----------------------------------------------------------------

    /** A pan has landed: pull the knobs it was carrying onto the new edges, then commit if the
     *  gesture that asked for it is already over. */
    private fun landPan() {
        val pending = pan ?: return
        val first = days.firstOrNull()?.date ?: return
        val last = days.last().date
        var next = current ?: return
        if (pending.start) next = DateRange(first, if (next.to < first) first else next.to)
        if (pending.end) next = DateRange(if (next.from > last) last else next.from, last)
        pan = null
        if (pending.commit) commit(next) else draft = next
    }

    /** Moves the window [STEP_DAYS]; a knob on the edge being moved toward is pulled along once
     *  the moved window is in. False once the window is already at that end of the history. */
    private fun step(dir: Int): Boolean {
        if (!(if (dir < 0) canPanEarlier else canPanLater)) return false
        val sel = current ?: return false
        val inFlight = pan
        pan = Pan(
            // A pan still in flight hasn't landed, so this window's edges are stale.
            start = (inFlight?.start ?: false) || (dir < 0 && startOf(sel.from) == 0),
            end = (inFlight?.end ?: false) || (dir > 0 && endOf(sel.to) == days.size),
            commit = false,
        )
        onPan(dir * STEP_DAYS)
        return true
    }

    /** Moves the selection its own length; false when it can't move that way (yet). */
    private fun shiftStep(dir: Int): Boolean {
        val sel = current ?: return false
        val next = onShift(sel, dir) ?: return false
        draft = next
        render()
        return true
    }

    /** Ends a gesture: commits now, or once a pan still in flight has landed. */
    private fun finish() {
        val pending = pan
        if (pending != null) pending.commit = true else commit(current)
    }

    private fun stopRepeat() {
        if (repeatAction == null) return
        handler.removeCallbacks(repeat)
        repeatAction = null
        finish()
    }

    // Press-and-hold is a touch gesture; a click that didn't come from one (TalkBack, a
    // keyboard) steps once.
    @SuppressLint("ClickableViewAccessibility")
    private fun setUpRepeatButton(button: View, action: () -> Boolean) {
        button.setOnTouchListener { view, event ->
            when (event.actionMasked) {
                MotionEvent.ACTION_DOWN -> {
                    view.isPressed = true
                    action()
                    repeatAction = action
                    handler.postDelayed(repeat, REPEAT_DELAY_MS)
                }
                MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                    view.isPressed = false
                    stopRepeat()
                }
            }
            true
        }
        button.setOnClickListener {
            if (action()) finish()
        }
    }

    // ---- The track ----------------------------------------------------------------------

    private fun onTrackPress(b: Int) {
        val sel = current ?: return
        // An off-screen knob counts as sitting on the edge it went past.
        val n = days.size
        val start = startOf(sel.from).coerceIn(0, n)
        val end = endOf(sel.to).coerceIn(0, n)
        val knob = if (Math.abs(b - start) <= Math.abs(b - end)) Knob.START else Knob.END
        dragging = knob
        draft = moveKnob(knob, b, sel)
        render()
    }

    private fun onTrackDrag(b: Int) {
        val knob = dragging ?: return
        val sel = draft ?: return
        // Held past the edge it's moving toward, a handle pages the window that way.
        val past = when {
            knob == Knob.START && b < 0 && canPanEarlier -> -1
            knob == Knob.END && b > days.size && canPanLater -> 1
            else -> 0
        }
        if (past != edgePan) {
            handler.removeCallbacks(edgeRepeat)
            edgePan = past
            if (past != 0) edgeRepeat.run()
        }
        if (past == 0) {
            draft = moveKnob(knob, b, sel)
            render()
        }
    }

    /** The finger moved [moved] slots toward the right: the window moves that many days into the
     *  past, the days following the finger. Clamped at both ends of the history by [onPan]. */
    private fun onSwipe(moved: Int) {
        if (moved > 0 && !canPanEarlier) return
        if (moved < 0 && !canPanLater) return
        onPan(-moved)
    }

    private fun onTrackRelease() {
        if (dragging == null) return
        dragging = null
        handler.removeCallbacks(edgeRepeat)
        edgePan = 0
        finish()
    }

    // ---- Rendering ----------------------------------------------------------------------

    private fun render() {
        val sel = current
        if (sel == null) {
            track.set(0, 0, 0, emptyList(), emptyList())
            label.text = null
            shiftEarlier.isEnabled = false
            shiftLater.isEnabled = false
            return
        }
        track.set(
            days.size,
            startOf(sel.from),
            endOf(sel.to),
            ScrubberBars.levels(days.map { it.distanceMeters }),
            ScrubberBars.labels(days.map { it.date }),
        )
        label.text = ScrubberBars.rangeLabel(LocalDate.parse(sel.from), LocalDate.parse(sel.to), locale)
        shiftEarlier.isEnabled = canShift(sel, -1)
        shiftLater.isEnabled = canShift(sel, 1)
    }

    /** Stops a held button's or a handle's repeat without committing — the screen is going away. */
    fun release() {
        handler.removeCallbacks(repeat)
        handler.removeCallbacks(edgeRepeat)
        repeatAction = null
        edgePan = 0
    }

    companion object {
        /** How many activity days the scrubber shows edge to edge — about 30dp a day on a
         *  412dp-wide phone, room for a bar and its day's number. */
        const val WINDOW_DAYS = 12

        /** How far the window moves for each step of a handle held past an edge, in activity
         *  days. */
        const val STEP_DAYS = 5

        /** How often a handle held past an edge steps the window again. */
        const val EDGE_REPEAT_MS = 500L

        const val REPEAT_DELAY_MS = 400L
        const val REPEAT_INTERVAL_MS = 180L
    }
}
