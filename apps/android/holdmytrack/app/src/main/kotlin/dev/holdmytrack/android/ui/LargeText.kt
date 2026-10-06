package dev.holdmytrack.android.ui

import android.content.res.Resources
import android.view.ViewGroup
import android.widget.LinearLayout

/**
 * The phone's font size turned up past [THRESHOLD] (Settings → Display → Font size): rows laid
 * out for the default size — a label beside a control, four numbers across — no longer fit, and
 * the screens that have them stack them instead ([stack]) rather than breaking words mid-way.
 */
object LargeText {

    /** 1.3× — where the You tab's labels first broke on a 412dp phone. */
    private const val THRESHOLD = 1.3f

    fun isOn(res: Resources): Boolean = res.configuration.fontScale >= THRESHOLD

    /**
     * [row] as a column when the text is large: each child that shared the row's width by
     * weight takes the whole width instead, and loses the start margin that spaced it from the
     * one before, which now sits above it — with a little room between them.
     */
    fun stack(row: LinearLayout) {
        if (!isOn(row.resources)) return
        row.orientation = LinearLayout.VERTICAL
        val gap = (4 * row.resources.displayMetrics.density).toInt()
        for (i in 0 until row.childCount) {
            val child = row.getChildAt(i)
            val params = child.layoutParams as LinearLayout.LayoutParams
            if (params.width == 0) {
                params.width = ViewGroup.LayoutParams.MATCH_PARENT
                params.weight = 0f
            }
            if (i > 0) {
                params.marginStart = 0
                if (row.showDividers == LinearLayout.SHOW_DIVIDER_NONE) params.topMargin = gap
            }
            child.layoutParams = params
        }
    }
}
