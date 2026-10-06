package dev.holdmytrack.android.panel

import android.annotation.SuppressLint
import android.graphics.PointF
import android.view.MotionEvent
import android.view.View
import android.widget.TextView
import androidx.appcompat.widget.TooltipCompat
import com.google.android.material.button.MaterialButton
import com.google.android.material.slider.RangeSlider
import dev.holdmytrack.android.R
import dev.holdmytrack.android.map.EditPreview
import dev.holdmytrack.android.map.TrackEditOverlay
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.TrackEdit
import dev.holdmytrack.android.net.TrackPoint
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlin.math.roundToInt
import org.maplibre.android.maps.MapLibreMap

/**
 * The Edit window's Track tab — the web's `apps/web/src/ui/TrackEditor.tsx` (`docs/SPEC.md`
 * FR-5.14): one activity's recorded points and the edit already saved on them, a two-knob range
 * over the points as the edit leaves them, and **Chop** (keep the range), **Cut** (take out
 * what's between the knobs and join them), **Delete point** (a toggle: a tap on the map takes
 * out the point under it), **Move point** (a toggle: a point pressed on the map follows the
 * finger and stays where it's let go), **Undo** and **Reset**. Nothing is sent from here: [pending] is the
 * whole edit for the window's Save to send, and every change is drawn over the map through
 * [onDraw] — the points as they'd end up, the knobs, and the preview of what goes.
 *
 * The knobs are held as the times of the points they sit on, not as indices, so a deleted point
 * elsewhere never moves them; null is "at that end".
 */
class TrackEditor(
    private val root: View,
    /** Redraws the map overlay: the points as edited, the knobs' indices, what to preview.
     *  Null clears it. */
    private val onDraw: (visible: List<TrackPoint>?, lo: Int, hi: Int, preview: EditPreview) -> Unit,
    /** The session changed, or finished loading — the window's tab dot and Save follow it. */
    private val onChange: () -> Unit,
) {
    private val context = root.context
    private val res = context.resources

    private val note: TextView = root.findViewById(R.id.edit_track_note)
    private val body: View = root.findViewById(R.id.edit_track_body)
    private val readout: TextView = root.findViewById(R.id.edit_track_readout)
    private val slider: RangeSlider = root.findViewById(R.id.edit_track_slider)
    private val start: TextView = root.findViewById(R.id.edit_track_start)
    private val count: TextView = root.findViewById(R.id.edit_track_count)
    private val end: TextView = root.findViewById(R.id.edit_track_end)
    private val chop: MaterialButton = root.findViewById(R.id.edit_track_chop)
    private val cut: MaterialButton = root.findViewById(R.id.edit_track_cut)
    private val deletePoint: MaterialButton = root.findViewById(R.id.edit_track_delete_point)
    private val movePoint: MaterialButton = root.findViewById(R.id.edit_track_move_point)
    private val undo: MaterialButton = root.findViewById(R.id.edit_track_undo)
    private val reset: MaterialButton = root.findViewById(R.id.edit_track_reset)
    private val modeNote: TextView = root.findViewById(R.id.edit_track_mode_note)

    private var activityId: String? = null
    private var points: List<TrackPoint>? = null
    private var base: TrackEdit? = null
    private var ops: List<EditOp> = emptyList()
    private var knobLo: Long? = null
    private var knobHi: Long? = null
    private var preview = EditPreview.CHOP
    private var generation = 0

    /** Delete point is on: a map tap on a point goes to [dropPoint]. */
    var deleteMode = false
        private set

    /** Move point is on: a press on a point drags it ([onMapTouch]). Never on with [deleteMode]. */
    private var moveMode = false

    /** The point a Move point drag holds, drawn at the finger — nothing committed until let go. */
    private var dragging: EditOp.Move? = null

    /** The window is saving: nothing here changes meanwhile. */
    var busy = false
        set(value) {
            field = value
            render()
        }

    private var visible: List<TrackPoint> = emptyList()
    private var distances = DoubleArray(0)

    init {
        slider.stepSize = 1f
        slider.addOnChangeListener { s, _, fromUser ->
            if (!fromUser || visible.isEmpty()) return@addOnChangeListener
            val lo = s.values[0].roundToInt().coerceIn(0, visible.size - 1)
            val hi = s.values[1].roundToInt().coerceIn(lo, visible.size - 1)
            knobLo = if (lo == 0) null else visible[lo].t
            knobHi = if (hi == visible.size - 1) null else visible[hi].t
            render()
        }
        chop.setOnClickListener {
            val (lo, hi) = knobs()
            push(EditTrackOps.chop(visible, lo, hi))
        }
        cut.setOnClickListener {
            val (lo, hi) = knobs()
            preview = EditPreview.CHOP
            push(EditTrackOps.cut(visible, lo, hi))
        }
        holdToPreviewCut()
        deletePoint.addOnCheckedChangeListener { _, checked ->
            deleteMode = checked
            if (checked) movePoint.isChecked = false
            render()
        }
        movePoint.addOnCheckedChangeListener { _, checked ->
            moveMode = checked
            if (checked) deletePoint.isChecked = false else dragging = null
            render()
        }
        undo.setOnClickListener {
            if (ops.isEmpty()) return@setOnClickListener
            ops = ops.dropLast(1)
            toEnds()
        }
        reset.setOnClickListener { push(EditOp.Reset) }
        TooltipCompat.setTooltipText(chop, res.getString(R.string.edit_track_chop_title))
        TooltipCompat.setTooltipText(cut, res.getString(R.string.edit_track_cut_title))
        TooltipCompat.setTooltipText(deletePoint, res.getString(R.string.edit_track_delete_point_title))
        TooltipCompat.setTooltipText(movePoint, res.getString(R.string.edit_track_move_point_title))
        TooltipCompat.setTooltipText(undo, res.getString(R.string.edit_track_undo_title))
        TooltipCompat.setTooltipText(reset, res.getString(R.string.edit_track_reset_title))
    }

    /** Loads [activity]'s points the first time the tab opens; the session then lasts until
     *  [close], so switching back to the Activity tab loses nothing. */
    fun open(activity: Activity) {
        if (activityId == activity.id) {
            render()
            return
        }
        activityId = activity.id
        points = null
        base = null
        ops = emptyList()
        knobLo = null
        knobHi = null
        pointModesOff()
        val gen = ++generation
        note.setText(R.string.edit_track_loading)
        note.setTextColor(context.getColor(R.color.hmt_ink_meta))
        note.visibility = View.VISIBLE
        body.visibility = View.GONE
        HoldMyTrackApi.trackPoints(activity.id) { result ->
            if (gen != generation) return@trackPoints
            result.onSuccess {
                points = it.points
                base = it.edit
                note.visibility = View.GONE
                render()
                onChange()
            }.onFailure {
                note.text = it.message?.takeIf { m -> m.isNotBlank() } ?: res.getString(R.string.edit_save_failed)
                note.setTextColor(context.getColor(R.color.hmt_danger))
            }
        }
    }

    /** The tab is out of sight: Delete point and Move point go off, as the web's do when its
     *  tab does. */
    fun hide() {
        pointModesOff()
    }

    private fun pointModesOff() {
        deletePoint.isChecked = false
        movePoint.isChecked = false
    }

    /** The window closed: the session and the overlay go. */
    fun close() {
        generation += 1
        activityId = null
        points = null
        ops = emptyList()
        pointModesOff()
        onDraw(null, 0, 0, EditPreview.CHOP)
    }

    /** Whether the session changed anything — what Save sends, and the tab's dot. */
    val changed: Boolean
        get() = ops.isNotEmpty()

    /** The whole edit to send — null to go back to the track as recorded. Only meaningful
     *  when [changed]. */
    val pending: TrackEdit?
        get() = EditTrackOps.fold(base, ops).takeUnless { it.isEmpty }

    /** A map tap in Delete point mode, on the point at [t]; a track keeps at least two. */
    fun dropPoint(t: Long) {
        if (!deleteMode || busy || visible.size <= 2) return
        push(EditOp.Drop(t))
    }

    /**
     * The map's own touches, before it pans, in Move point mode: a press on a point takes the
     * gesture — the map stays put, as a Private location's handle drag does — each move draws the
     * point under the finger, and letting go is one step; let go unmoved, or a second finger
     * down, and nothing changes. Returns whether the touch was taken.
     */
    fun onMapTouch(event: MotionEvent, map: MapLibreMap, density: Float): Boolean {
        if (!moveMode || busy || points == null) return false
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                val t = TrackEditOverlay.pointAt(map, PointF(event.x, event.y), density) ?: return false
                val p = visible.firstOrNull { it.t == t } ?: return false
                dragging = EditOp.Move(t, p.lon, p.lat)
                pressedUnmoved = true
                return true
            }
            MotionEvent.ACTION_POINTER_DOWN -> {
                val was = dragging != null
                dragging = null
                if (was) draw()
                return false
            }
            MotionEvent.ACTION_MOVE -> {
                val held = dragging ?: return false
                val at = map.projection.fromScreenLocation(PointF(event.x, event.y))
                dragging = held.copy(lon = at.longitude, lat = at.latitude)
                pressedUnmoved = false
                draw()
                return true
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                val held = dragging ?: return false
                dragging = null
                if (event.actionMasked == MotionEvent.ACTION_UP && !pressedUnmoved) {
                    push(EditTrackOps.move(held.t, held.lon, held.lat))
                } else {
                    draw()
                }
                return true
            }
        }
        return dragging != null
    }

    /** No ACTION_MOVE since the press: a tap on a point, which moves nothing. */
    private var pressedUnmoved = false

    private fun push(op: EditOp?) {
        if (op == null) return
        ops = ops + op
        toEnds()
    }

    /** Chop, Cut, Reset and Undo put the knobs back at the ends, as the web's do. */
    private fun toEnds() {
        knobLo = null
        knobHi = null
        render()
        onChange()
    }

    /** The knobs' indices in [visible]: the first point at or after the low knob's time, the
     *  last at or before the high one's. */
    private fun knobs(): Pair<Int, Int> {
        val last = visible.size - 1
        val lo = knobLo?.let { t -> visible.indexOfFirst { it.t >= t }.coerceAtLeast(0) } ?: 0
        val found = knobHi?.let { t -> visible.indexOfLast { it.t <= t } } ?: last
        val hi = maxOf(lo, if (found < 0) last else found)
        return lo to hi
    }

    private fun render() {
        val recorded = points ?: return
        visible = EditTrackOps.apply(recorded, EditTrackOps.fold(base, ops))
        if (visible.size < 2) {
            body.visibility = View.GONE
            onDraw(null, 0, 0, preview)
            return
        }
        body.visibility = View.VISIBLE
        distances = EditTrackOps.cumulativeDistances(visible)
        val last = visible.size - 1
        val (lo, hi) = knobs()

        slider.valueFrom = 0f
        slider.valueTo = last.toFloat()
        slider.setValues(lo.toFloat(), hi.toFloat())
        slider.isEnabled = !busy
        readout.text = res.getString(
            R.string.edit_track_range_readout,
            PanelFormat.distanceValue(res, distances[lo]),
            PanelFormat.distanceValue(res, distances[hi]),
            PanelFormat.unit(res),
            clock(visible[lo].t),
            clock(visible[hi].t),
        )
        start.text = PanelFormat.distance(res, 0.0)
        count.text = res.getQuantityString(R.plurals.edit_track_points, visible.size, visible.size)
        end.text = PanelFormat.distance(res, distances[last])

        chop.isEnabled = !busy && EditTrackOps.chop(visible, lo, hi) != null
        cut.isEnabled = !busy && EditTrackOps.cut(visible, lo, hi) != null
        deletePoint.isEnabled = !busy
        movePoint.isEnabled = !busy
        undo.isEnabled = !busy && ops.isNotEmpty()
        reset.visibility = if (!EditTrackOps.fold(base, ops).isEmpty) View.VISIBLE else View.GONE
        reset.isEnabled = !busy
        when {
            deleteMode -> modeNote.setText(R.string.edit_track_delete_point_note)
            moveMode -> modeNote.setText(R.string.edit_track_move_point_note)
        }
        modeNote.visibility = if (deleteMode || moveMode) View.VISIBLE else View.GONE

        draw()
    }

    /** The overlay alone, for each step of a drag: the held point at the finger, so the line
     *  through it follows, over the points as the edit leaves them. */
    private fun draw() {
        if (visible.size < 2) return
        val (lo, hi) = knobs()
        val held = dragging
        val shown = if (held == null) visible else visible.map { if (it.t == held.t) it.copy(lon = held.lon, lat = held.lat) else it }
        onDraw(shown, lo, hi, if (preview == EditPreview.CUT && cut.isEnabled) EditPreview.CUT else EditPreview.CHOP)
    }

    /** The web previews Cut while its button is hovered or focused; on a phone, while it's
     *  held — the tap that follows is the Cut itself. */
    @SuppressLint("ClickableViewAccessibility") // Only a preview; the click still performs Cut.
    private fun holdToPreviewCut() {
        cut.setOnTouchListener { _, event ->
            when (event.actionMasked) {
                MotionEvent.ACTION_DOWN -> {
                    preview = EditPreview.CUT
                    render()
                }
                MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                    preview = EditPreview.CHOP
                    render()
                }
            }
            false
        }
    }

    private fun clock(ms: Long): String =
        DateTimeFormatter.ofLocalizedTime(FormatStyle.SHORT)
            .withLocale(res.configuration.locales[0])
            .withZone(ZoneId.systemDefault())
            .format(Instant.ofEpochMilli(ms))
}
