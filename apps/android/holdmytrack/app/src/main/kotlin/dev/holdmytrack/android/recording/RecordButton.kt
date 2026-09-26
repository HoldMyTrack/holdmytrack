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

    var holdEnabled: Boolean = false
        set(value) {
            if (field == value) return
            field = value
            if (!value) cancelHold()
            updateAccessibilityAction()
        }

    private var progress = 0f
    private var suppressClick = false

    private val density = resources.displayMetrics.density
    private val ringPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = RING_WIDTH_DP * density
        strokeCap = Paint.Cap.ROUND
        color = RING_COLOR
    }
    private val ringBounds = RectF()

    private val animator = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = HOLD_DURATION_MS
        interpolator = LinearInterpolator()
        addUpdateListener {
            progress = it.animatedValue as Float
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
                invalidate()
                performHapticFeedback(HapticFeedbackConstants.LONG_PRESS)
                onHoldComplete?.invoke()
            }
        })
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
        if (progress <= 0f) return
        val inset = ringPaint.strokeWidth / 2
        ringBounds.set(inset, inset, width - inset, height - inset)
        canvas.drawArc(ringBounds, -90f, 360f * progress, false, ringPaint)
    }

    override fun onDetachedFromWindow() {
        cancelHold()
        super.onDetachedFromWindow()
    }

    private fun cancelHold() {
        animator.cancel()
        if (progress != 0f) {
            progress = 0f
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
        private const val RING_WIDTH_DP = 4f

        /** White, not the recording red: it's drawn over the red (recording) or amber (paused)
         *  outline `bg_record_button_*.xml` already gives the button, and has to stand out from
         *  both. */
        private const val RING_COLOR = 0xFFFFFFFF.toInt()
    }
}
