package dev.holdmytrack.android.recording

import android.animation.Animator
import android.animation.AnimatorListenerAdapter
import android.animation.ValueAnimator
import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import android.util.AttributeSet
import android.view.HapticFeedbackConstants
import android.view.MotionEvent
import android.view.ViewConfiguration
import android.view.animation.LinearInterpolator
import androidx.appcompat.widget.AppCompatImageButton
import androidx.core.view.ViewCompat
import androidx.core.view.accessibility.AccessibilityNodeInfoCompat.AccessibilityActionCompat
import dev.holdmytrack.android.R

/**
 * The map's record button, with a hold-to-stop gesture: while [holdEnabled], pressing and
 * holding for [HOLD_DURATION_MS] fills a ring around the button and then calls [onHoldComplete].
 * The platform's own long-press (~400 ms, no feedback until it fires) is replaced rather than
 * reused — ending a recording is the one irreversible action here, so the hold is long and
 * visibly counts down, and letting go at any point before the ring closes cancels it.
 *
 * The countdown has to survive the finger covering the button, so it's told three ways: the
 * ring is drawn [RING_GAP_DP] outside the button's own edge (its parents don't clip children,
 * `activity_main.xml`), where it shows around a fingertip; a tick is felt at each quarter; and
 * [onHoldProgress] reports it for the status pill beside the button to show as well.
 *
 * While [pulsing] — recording, not paused — a red halo keeps widening out from the button and
 * fading, so a running recording looks alive at a glance rather than only by its icon.
 *
 * A plain tap still clicks, but only when released before the system long-press timeout: a
 * hold abandoned part-way is a change of mind about stopping, not a request to pause, and a
 * long hold while idle must not fall through to a tap that starts a recording.
 *
 * TalkBack can't hold, so while [holdEnabled] the same stop is offered as the node's long-click
 * accessibility action.
 */
class RecordButton @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : AppCompatImageButton(context, attrs) {

    var onHoldComplete: (() -> Unit)? = null

    /** The hold's progress, 0 to 1, on every frame of it; 0 again when it's let go or done. */
    var onHoldProgress: ((Float) -> Unit)? = null

    var pulsing: Boolean = false
        set(value) {
            if (field == value) return
            field = value
            if (value && isAttachedToWindow) pulse.start() else pulse.cancel()
            pulsePhase = 0f
            invalidate()
        }

    var holdEnabled: Boolean = false
        set(value) {
            if (field == value) return
            field = value
            if (!value) cancelHold()
            updateAccessibilityAction()
        }

    private var progress = 0f
    private var ticksFelt = 0
    private var suppressClick = false
    private var pulsePhase = 0f

    private val density = resources.displayMetrics.density
    private val ringPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = RING_WIDTH_DP * density
        strokeCap = Paint.Cap.ROUND
        color = RING_COLOR
    }
    private val trackPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = RING_WIDTH_DP * density
        color = TRACK_COLOR
    }
    private val pulsePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = PULSE_WIDTH_DP * density
        color = PULSE_COLOR
    }
    private val ringBounds = RectF()

    private val animator = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = HOLD_DURATION_MS
        interpolator = LinearInterpolator()
        addUpdateListener {
            progress = it.animatedValue as Float
            // A tick at each quarter the ring passes, the last quarter being the stop itself.
            val quarter = (progress * HOLD_TICKS).toInt()
            if (quarter > ticksFelt && quarter < HOLD_TICKS) {
                ticksFelt = quarter
                performHapticFeedback(HapticFeedbackConstants.SEGMENT_TICK)
            }
            onHoldProgress?.invoke(progress)
            invalidate()
        }
        addListener(object : AnimatorListenerAdapter() {
            private var cancelled = false
            override fun onAnimationStart(animation: Animator) { cancelled = false }
            override fun onAnimationCancel(animation: Animator) { cancelled = true }
            override fun onAnimationEnd(animation: Animator) {
                if (cancelled) return
                suppressClick = true
                progress = 0f
                onHoldProgress?.invoke(0f)
                invalidate()
                performHapticFeedback(HapticFeedbackConstants.CONFIRM)
                onHoldComplete?.invoke()
            }
        })
    }

    private val pulse = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = PULSE_DURATION_MS
        repeatCount = ValueAnimator.INFINITE
        addUpdateListener {
            pulsePhase = it.animatedValue as Float
            invalidate()
        }
    }

    init {
        isLongClickable = false
    }

    // Lint's ClickableViewAccessibility doesn't see that every event still reaches
    // super.onTouchEvent, which performs the click; TalkBack's stop is the long-click action
    // (updateAccessibilityAction), since a screen reader can't hold.
    @SuppressLint("ClickableViewAccessibility")
    override fun onTouchEvent(event: MotionEvent): Boolean {
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                suppressClick = false
                ticksFelt = 0
                if (holdEnabled) animator.start()
            }
            MotionEvent.ACTION_UP -> {
                if (event.eventTime - event.downTime >= ViewConfiguration.getLongPressTimeout()) suppressClick = true
                cancelHold()
            }
            MotionEvent.ACTION_CANCEL -> cancelHold()
        }
        return super.onTouchEvent(event)
    }

    override fun performClick(): Boolean {
        if (suppressClick) {
            suppressClick = false
            return true
        }
        return super.performClick()
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        val cx = width / 2f
        val cy = height / 2f
        val edge = minOf(width, height) / 2f
        if (progress > 0f) {
            val radius = edge + (RING_GAP_DP + RING_WIDTH_DP / 2) * density
            ringBounds.set(cx - radius, cy - radius, cx + radius, cy + radius)
            canvas.drawOval(ringBounds, trackPaint)
            canvas.drawArc(ringBounds, -90f, 360f * progress, false, ringPaint)
        } else if (pulsing) {
            pulsePaint.alpha = ((1f - pulsePhase) * PULSE_MAX_ALPHA).toInt()
            canvas.drawCircle(cx, cy, edge + pulsePhase * PULSE_REACH_DP * density, pulsePaint)
        }
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        if (pulsing) pulse.start()
    }

    override fun onDetachedFromWindow() {
        cancelHold()
        pulse.cancel()
        super.onDetachedFromWindow()
    }

    private fun cancelHold() {
        animator.cancel()
        if (progress != 0f) {
            progress = 0f
            onHoldProgress?.invoke(0f)
            invalidate()
        }
    }

    private fun updateAccessibilityAction() {
        if (holdEnabled) {
            ViewCompat.replaceAccessibilityAction(this, AccessibilityActionCompat.ACTION_LONG_CLICK, context.getString(R.string.recording_stop)) { _, _ ->
                onHoldComplete?.invoke()
                true
            }
        } else {
            ViewCompat.removeAccessibilityAction(this, AccessibilityActionCompat.ACTION_LONG_CLICK.id)
        }
    }

    companion object {
        const val HOLD_DURATION_MS = 2_000L
        private const val HOLD_TICKS = 4
        private const val RING_WIDTH_DP = 5f

        /** How far outside the button's edge the ring starts — enough to clear a fingertip
         *  pressed on the 56dp button. Kept inside the space `activity_main.xml` leaves around
         *  it, since that's all the room it has to draw in. */
        private const val RING_GAP_DP = 5f

        /** White, not the recording red: it runs just outside the red (recording) or amber
         *  (paused) outline `bg_record_button_*.xml` gives the button, and over any map, so it
         *  has to stand out from all of them — on a dark track that reads over light maps too. */
        private const val RING_COLOR = 0xFFFFFFFF.toInt()
        private const val TRACK_COLOR = 0x80000000.toInt()

        /** The recording outline's red (`bg_record_button_recording.xml`). */
        private const val PULSE_COLOR = 0xFFE53935.toInt()
        private const val PULSE_WIDTH_DP = 3f
        private const val PULSE_REACH_DP = 8f
        private const val PULSE_MAX_ALPHA = 200
        private const val PULSE_DURATION_MS = 1_600L
    }
}
