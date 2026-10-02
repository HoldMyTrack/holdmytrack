package dev.holdmytrack.android.map

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapShader
import android.graphics.Canvas
import android.graphics.Matrix
import android.graphics.Outline
import android.graphics.Paint
import android.graphics.RectF
import android.graphics.Shader
import android.graphics.Typeface
import android.view.View
import android.view.ViewGroup
import android.view.ViewOutlineProvider
import android.widget.FrameLayout
import dev.holdmytrack.android.R
import dev.holdmytrack.android.photos.PhotoClusters
import dev.holdmytrack.android.photos.PhotoImages
import dev.holdmytrack.android.photos.ScreenPoint
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.maps.MapLibreMap

/** One marker: a saved photo — [thumbPath], its thumbnail's API path — or one the Photos tab
 *  is placing before it's uploaded, with its local [thumb]. */
data class PhotoMarkerItem(
    val id: String,
    val lon: Double,
    val lat: Double,
    val caption: String?,
    val thumbPath: String? = null,
    val thumb: Bitmap? = null,
)

/** How the Photos tab's unsaved changes alter the markers: photos moved or added ([upserts],
 *  replacing a saved one with the same id), photos to be deleted ([hidden]), and the one in
 *  hand ([activeId], drawn larger and never grouped). */
data class PhotoMarkerOverlay(
    val upserts: List<PhotoMarkerItem>,
    val hidden: Set<String>,
    val activeId: String?,
)

/**
 * Photo markers (`docs/SPEC.md` FR-16.7), the web's `apps/web/src/map/photos.ts`: each photo of
 * the selected activity or the open Story as its own thumbnail in a round frame, at its point on
 * the track — or, where several would overlap on screen, one marker for the group: its first
 * photo, stacked, with a count ([PhotoClusters]). The groups are worked out again whenever the
 * camera comes to rest ([regroup]), so zooming in splits them; meanwhile [place] keeps every
 * marker on its point as the camera moves.
 *
 * Views in [layer] — a pass-through layer over the map, under its chrome — rather than a symbol
 * layer, as the web's are HTML markers: there are tens of them, not thousands, and each shows
 * its own image, which a symbol layer would need added to the style as an icon, and again after
 * every style load. A marker's tap is the view's, so it never reaches the map's own tap handling
 * and leaves the selection as it was.
 */
class PhotoMarkers(private val layer: FrameLayout, private val onOpen: (List<String>) -> Unit) {

    private val context = layer.context
    private val density = layer.resources.displayMetrics.density
    private var map: MapLibreMap? = null
    private var items: List<PhotoMarkerItem> = emptyList()
    private var activeId: String? = null
    private var loneId: String? = null

    /** One view per group, keyed by its photos' ids. */
    private val markers = LinkedHashMap<String, PhotoMarkerView>()

    /** What to show: [items] in route order, the one open or in hand ([activeId]) drawn larger,
     *  and [loneId] — the photo the Photos tab is moving — kept out of every group. */
    fun set(instance: MapLibreMap?, items: List<PhotoMarkerItem>, activeId: String?, loneId: String?) {
        map = instance
        this.items = items
        this.activeId = activeId
        this.loneId = loneId
        regroup()
    }

    /** The groups again, from where the photos are on screen now. */
    fun regroup() {
        val instance = map
        if (instance == null || items.isEmpty()) {
            clear()
            return
        }
        val projection = instance.projection
        val byId = items.associateBy { it.id }
        val groups = PhotoClusters.cluster(
            items.filter { it.id != loneId }.map {
                val p = projection.toScreenLocation(LatLng(it.lat, it.lon))
                ScreenPoint(it.id, p.x, p.y)
            },
            PhotoClusters.RADIUS_DP * density,
        ).map { it.ids }.toMutableList()
        loneId?.takeIf { it in byId }?.let { groups += listOf(it) }

        val wanted = groups.associateBy { it.joinToString("|") }
        markers.keys.filter { it !in wanted }.forEach { key -> markers.remove(key)?.let(layer::removeView) }
        for ((key, ids) in wanted) {
            val item = byId.getValue(ids.first())
            val view = markers.getOrPut(key) {
                PhotoMarkerView(context).also { view ->
                    view.setOnClickListener { onOpen(view.ids) }
                    layer.addView(view, FrameLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT))
                }
            }
            view.bind(item, ids, activeId != null && activeId in ids)
        }
        // The one in hand or open over the rest.
        markers.values.filter { it.active }.forEach { it.bringToFront() }
        place()
    }

    /** Every marker on its point again — on each camera move. */
    fun place() {
        val instance = map ?: return
        val projection = instance.projection
        for (view in markers.values) {
            val item = view.item ?: continue
            val p = projection.toScreenLocation(LatLng(item.lat, item.lon))
            view.translationX = p.x - view.side / 2f
            view.translationY = p.y - view.side / 2f
        }
    }

    fun clear() {
        markers.values.forEach(layer::removeView)
        markers.clear()
    }
}

/**
 * One marker, drawn: the photo's thumbnail cropped into a 36dp circle (52dp while open or in
 * hand) in a 2dp ring — the surface's colour, the accent's when active — and, for a group, two
 * cards' edges showing behind it and the count on its corner (the web's `.photo-marker--group`
 * and `.photo-marker__count`). The view is a square [side] wide with room around the circle for
 * those, so its touch target is never under 48dp.
 */
@SuppressLint("ViewConstructor")
private class PhotoMarkerView(context: Context) : View(context) {

    private val density = resources.displayMetrics.density
    var item: PhotoMarkerItem? = null
        private set
    var ids: List<String> = emptyList()
        private set
    var active = false
        private set
    private var bitmap: Bitmap? = null

    private val ring = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.STROKE }
    private val fill = Paint(Paint.ANTI_ALIAS_FLAG)
    private val edge = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.STROKE; strokeWidth = density }
    private val image = Paint(Paint.ANTI_ALIAS_FLAG or Paint.FILTER_BITMAP_FLAG)
    private val badge = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = context.getColor(R.color.hmt_accent) }
    private val count = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = context.getColor(R.color.hmt_on_accent)
        textSize = resources.getDimension(R.dimen.hmt_text_xs)
        typeface = Typeface.DEFAULT_BOLD
        textAlign = Paint.Align.CENTER
    }

    private val diameter: Float
        get() = (if (active) ACTIVE_DP else MARKER_DP) * density

    /** The view's width and height: the circle and room for the stack and the count. */
    val side: Int
        get() = (diameter + 2 * PAD_DP * density).toInt()

    init {
        isClickable = true
        isFocusable = true
        elevation = 4 * density
        // The shadow is the circle's, not the square view's.
        outlineProvider = object : ViewOutlineProvider() {
            override fun getOutline(view: View, outline: Outline) {
                val r = diameter / 2
                val c = side / 2f
                outline.setOval((c - r).toInt(), (c - r).toInt(), (c + r).toInt(), (c + r).toInt())
            }
        }
    }

    fun bind(next: PhotoMarkerItem, groupIds: List<String>, isActive: Boolean) {
        val resized = isActive != active
        val sameImage = item?.let { it.thumbPath == next.thumbPath && it.thumb === next.thumb } == true
        item = next
        ids = groupIds
        active = isActive
        contentDescription = if (groupIds.size > 1) {
            resources.getQuantityString(R.plurals.photos_marker_group, groupIds.size, groupIds.size)
        } else {
            next.caption ?: resources.getString(R.string.photos_marker)
        }
        if (!sameImage) {
            bitmap = next.thumb ?: next.thumbPath?.let(PhotoImages::cached)
            val path = next.thumbPath
            if (bitmap == null && path != null) {
                PhotoImages.thumb(path) { loaded ->
                    if (item?.thumbPath == path) {
                        bitmap = loaded
                        invalidate()
                    }
                }
            }
        }
        if (resized) {
            requestLayout()
            invalidateOutline()
        }
        invalidate()
    }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) = setMeasuredDimension(side, side)

    override fun onDraw(canvas: Canvas) {
        val c = side / 2f
        val r = diameter / 2
        val surface = context.getColor(R.color.hmt_surface)
        if (ids.size > 1) {
            // Two cards behind, up and to the right, as the web's stacked box-shadows.
            for (step in intArrayOf(2, 1)) {
                val d = STACK_DP * step * density
                fill.color = surface
                canvas.drawCircle(c + d, c - d, r, fill)
                edge.color = context.getColor(R.color.hmt_border_strong)
                canvas.drawCircle(c + d, c - d, r, edge)
            }
        }
        val ringWidth = RING_DP * density
        fill.color = context.getColor(R.color.hmt_surface_tint_strong)
        canvas.drawCircle(c, c, r, fill)
        bitmap?.let { b ->
            val inner = r - ringWidth
            val scale = (2 * inner) / minOf(b.width, b.height)
            val matrix = Matrix().apply {
                setScale(scale, scale)
                postTranslate(c - b.width * scale / 2, c - b.height * scale / 2)
            }
            image.shader = BitmapShader(b, Shader.TileMode.CLAMP, Shader.TileMode.CLAMP).apply { setLocalMatrix(matrix) }
            canvas.drawCircle(c, c, inner, image)
        }
        ring.strokeWidth = ringWidth
        ring.color = if (active) context.getColor(R.color.hmt_accent) else surface
        canvas.drawCircle(c, c, r - ringWidth / 2, ring)
        if (ids.size > 1) {
            val text = ids.size.toString()
            val h = BADGE_DP * density
            val w = maxOf(h, count.measureText(text) + 8 * density)
            // The web's top: -8px, right: -10px, from the circle's box.
            val right = c + r + 10 * density
            val top = c - r - 8 * density
            canvas.drawRoundRect(RectF(right - w, top, right, top + h), h / 2, h / 2, badge)
            canvas.drawText(text, right - w / 2, top + h / 2 - (count.descent() + count.ascent()) / 2, count)
        }
    }

    private companion object {
        const val MARKER_DP = 36f
        const val ACTIVE_DP = 52f
        const val RING_DP = 2f
        const val STACK_DP = 3f
        const val BADGE_DP = 18f
        /** Room around the circle: the stack's 6dp and the badge's overhang, and a 48dp target. */
        const val PAD_DP = 12f
    }
}
