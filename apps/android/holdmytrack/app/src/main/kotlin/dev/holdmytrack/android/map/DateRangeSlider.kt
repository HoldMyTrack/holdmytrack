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
import java.time.format.DateTimeFormatter

/**
 * The map's date-range control — the Android counterpart of the web's phone footer,
 * `apps/web/src/ui/DateRangeSlider.tsx`, and the same design: one row, « ‹ track › », with the
 * selected dates under it (`docs/SPEC.md` §19 item 2).
 *
 * **Activity days, not calendar days.** The track is a window of [WINDOW_DAYS] consecutive
 * days that have activity ([ActivityDays]); the days between take no room.
 *
 * **Knobs sit on slot boundaries.** The start knob marks where the first selected day begins and
 * the end knob where the last one ends, so a one-day selection has its knobs one slot apart and
 * they can never be dragged closer than that.
 *
 * **Paging.** Earlier/Later move the window [STEP_DAYS] per tap, repeating while held. A knob on
 * the edge the window moves toward is pulled along with it — how a selection grows past the
 * window. The far knob may scroll out of view; its date stays in the label. The pull lands once
 * the moved window is in (the days before it may still be loading), so a gesture's commit waits
 * for that too.
 *
 * **Range shift.** The inner pair, ‹ ›, move the whole selection by its own length in activity
 * days ([shiftRange]), the window following — the outer pair, « » (Earlier/Later), move the
 * scale. A tap commits; a hold repeats like Earlier/Later and commits on release.
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
    private val earlier: View = root.findViewById(R.id.date_range_earlier)
    private val later: View = root.findViewById(R.id.date_range_later)
    private val track: DateRangeTrackView = root.findViewById(R.id.date_range_track)
    private val shiftEarlier: View = root.findViewById(R.id.date_range_shift_earlier)
    private val shiftLater: View = root.findViewById(R.id.date_range_shift_later)
    private val fromLabel: TextView = root.findViewById(R.id.date_range_from)
    private val toLabel: TextView = root.findViewById(R.id.date_range_to)

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

    private val dayFormat: DateTimeFormatter
    private val monthFormat: DateTimeFormatter

    init {
        val locale = root.resources.configuration.locales[0]
        dayFormat = DateTimeFormatter.ofPattern("d", locale)
        // Standalone month (LLL), as the web formats the month on its own.
        monthFormat = DateTimeFormatter.ofPattern("LLL", locale)
        earlier.contentDescription = root.resources.getQuantityString(R.plurals.date_range_earlier, STEP_DAYS, STEP_DAYS)
        later.contentDescription = root.resources.getQuantityString(R.plurals.date_range_later, STEP_DAYS, STEP_DAYS)
        setUpRepeatButton(earlier) { step(-1) }
        setUpRepeatButton(later) { step(1) }
        setUpRepeatButton(shiftEarlier) { shiftStep(-1) }
        setUpRepeatButton(shiftLater) { shiftStep(1) }
        track.onPress = ::onTrackPress
        track.onDrag = ::onTrackDrag
        track.onRelease = ::onTrackRelease
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
        draft = moveKnob(knob, b, sel)
        render()
    }

    private fun onTrackRelease() {
        if (dragging == null) return
        dragging = null
        commit(draft)
    }

    // ---- Rendering ----------------------------------------------------------------------

    private fun render() {
        earlier.isEnabled = canPanEarlier
        later.isEnabled = canPanLater
        val sel = current
        if (sel == null) {
            track.set(0, 0, 0, emptyList())
            fromLabel.text = null
            toLabel.text = null
            shiftEarlier.isEnabled = false
            shiftLater.isEnabled = false
            return
        }
        track.set(days.size, startOf(sel.from), endOf(sel.to), ticksOf(days.map { it.date }))
        fromLabel.text = formatDay(sel.from)
        toLabel.text = formatDay(sel.to)
        shiftEarlier.isEnabled = canShift(sel, -1)
        shiftLater.isEnabled = canShift(sel, 1)
    }

    /** "12 MAR 2026" — the web's `formatDayLabel`, in the app's language. */
    private fun formatDay(date: String): String {
        val day = LocalDate.parse(date)
        return "${dayFormat.format(day)} ${monthFormat.format(day).uppercase()} ${day.year}"
    }

    /** Stops a held button's repeat without committing — the screen is going away. */
    fun release() {
        handler.removeCallbacks(repeat)
        repeatAction = null
    }

    companion object {
        /** How many activity days the track shows edge to edge — 3 fewer than the web's, as
         *  the « ‹ › » buttons leave a phone's track narrower: ~18dp a day on a Pixel 10a
         *  (411dp wide), so the 14dp knobs of a one-day selection still sit apart. */
        const val WINDOW_DAYS = 12

        /** How far one Earlier/Later tap moves the window, in activity days. */
        const val STEP_DAYS = 5

        const val REPEAT_DELAY_MS = 400L
        const val REPEAT_INTERVAL_MS = 180L
    }
}
