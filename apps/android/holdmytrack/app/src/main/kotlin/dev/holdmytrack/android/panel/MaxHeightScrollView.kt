package dev.holdmytrack.android.panel

import android.content.Context
import android.util.AttributeSet
import android.view.View
import androidx.core.widget.NestedScrollView

/**
 * A scroll view as tall as its content up to [maxHeight], then scrolling — the web's
 * `max-height` with `overflow-y: auto`. The Photos tab's list sits in it, inside the Edit
 * window's own scroll view, so a long list never pushes the map out of sight. Null is no cap.
 */
class MaxHeightScrollView @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) :
    NestedScrollView(context, attrs) {

    var maxHeight: Int? = null
        set(value) {
            if (field == value) return
            field = value
            requestLayout()
        }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        val cap = maxHeight
        if (cap == null) {
            super.onMeasure(widthMeasureSpec, heightMeasureSpec)
            return
        }
        super.onMeasure(widthMeasureSpec, View.MeasureSpec.makeMeasureSpec(cap, View.MeasureSpec.AT_MOST))
    }
}
