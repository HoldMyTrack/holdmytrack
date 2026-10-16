package dev.holdmytrack.android.map

/** Where a camera fit may put activities: the map left between the top chrome and what covers the bottom. */
object FrameRules {
    const val PADDING_PX = 64

    /**
     * The fit's padding `[left, top, right, bottom]`. [chromeBottom] is the top row's bottom,
     * [bannerBottom] a capture banner's bottom if one shows, and [coveredBelow] how much of the
     * map an editor or the resting sheet covers. In landscape the two can leave no gap, so the
     * fit keeps a strip of [minSpanPx], taken from the top first.
     */
    fun padding(rootHeight: Int, chromeBottom: Int, bannerBottom: Int?, coveredBelow: Int, minSpanPx: Int): IntArray {
        val top = maxOf(PADDING_PX, chromeBottom, bannerBottom?.let { it + PADDING_PX / 2 } ?: 0)
        val bottom = PADDING_PX + coveredBelow
        val fitTop = top.coerceAtMost(maxOf(PADDING_PX, rootHeight - bottom - minSpanPx))
        val fitBottom = bottom.coerceAtMost(maxOf(PADDING_PX, rootHeight - fitTop - minSpanPx))
        return intArrayOf(PADDING_PX, fitTop, PADDING_PX, fitBottom)
    }
}
