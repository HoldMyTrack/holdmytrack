package dev.holdmytrack.android.export

import android.animation.ValueAnimator
import android.content.ClipData
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.PointF
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.provider.MediaStore
import android.util.Log
import android.view.LayoutInflater
import android.view.Menu
import android.view.View
import android.view.ViewGroup
import android.view.animation.DecelerateInterpolator
import android.widget.PopupMenu
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.content.res.AppCompatResources
import androidx.core.graphics.scale
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import com.google.android.material.button.MaterialButton
import dev.holdmytrack.android.R
import org.maplibre.android.camera.CameraPosition
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.Style
import org.json.JSONObject
import java.time.LocalDateTime
import java.time.format.DateTimeFormatter
import java.util.concurrent.Executors

/**
 * The map image export (`apps/android/docs/SPEC.md` FR-2.11, `IMPLEMENTATION.md` §3.5): the
 * viewfinder over the map, and what Capture does — draw the window on a hidden map
 * ([MapImageRenderer]), bake in the credit and the logo ([ImageMarks]), save it to Pictures and
 * open the share sheet on it. The geometry is [Viewfinder]'s.
 *
 * [host] is the map's own container, which the viewfinder covers whole; [prepare] puts the app's
 * layers on the hidden map's style the way the live map has them; [onShown] hides and brings back
 * the map's chrome, which would otherwise sit inside the window over the picture.
 */
class ExportViewfinder(
    private val host: ViewGroup,
    private val map: () -> MapLibreMap?,
    private val styleUrl: () -> String,
    private val prepare: (Style) -> Unit,
    private val darkBase: () -> Boolean,
    private val satelliteOn: () -> Boolean,
    private val onShown: (Boolean) -> Unit,
) {
    private val context: Context = host.context
    private val density = context.resources.displayMetrics.density
    private val main = Handler(Looper.getMainLooper())
    private val renderer = MapImageRenderer(host)

    private var root: View? = null
    private lateinit var frame: ExportViewfinderView
    private lateinit var toolbar: View
    private lateinit var choiceButton: MaterialButton
    private lateinit var captureButton: MaterialButton
    private lateinit var busyIndicator: View
    private lateinit var sizeLine: TextView
    private lateinit var errorLine: TextView

    private var choice: ExportChoice = lastChoice
    private var busy = false
    private var settling: ValueAnimator? = null
    private var splitting = true

    val isOpen: Boolean get() = root != null

    fun toggle() = if (isOpen) close() else open()

    fun open() {
        if (isOpen || map() == null) return
        val view = LayoutInflater.from(context).inflate(R.layout.view_export_viewfinder, host, false)
        root = view
        frame = view.findViewById(R.id.export_frame)
        frame.darkMap = darkBase()
        toolbar = view.findViewById(R.id.export_toolbar)
        choiceButton = view.findViewById(R.id.export_choice)
        captureButton = view.findViewById(R.id.export_capture)
        busyIndicator = view.findViewById(R.id.export_busy)
        sizeLine = view.findViewById(R.id.export_size)
        errorLine = view.findViewById(R.id.export_error)
        choiceButton.setOnClickListener { showChoices() }
        view.findViewById<View>(R.id.export_close).setOnClickListener { close() }
        captureButton.setOnClickListener { capture() }
        frame.cropOf = { start, corner, dx, dy -> Viewfinder.resize(start, corner, dx, dy, choice.aspect, area(), minWindow()) }
        frame.onCrop = { setWindow(it) }
        frame.onCropEnd = { start, end -> if (Viewfinder.moved(start, end)) settle(end) }
        // Every finger of a gesture goes where its first went: a pinch begun in the window keeps
        // the finger that lands on the dimmed map.
        splitting = host.isMotionEventSplittingEnabled
        host.isMotionEventSplittingEnabled = false
        host.addView(view)
        onShown(true)
        // A turned phone refits the window in its own shape.
        view.addOnLayoutChangeListener { _, l, t, r, b, ol, ot, or, ob ->
            if (ol != or && (r - l != or - ol || b - t != ob - ot)) setWindow(Viewfinder.fit(windowAspect(), area()))
        }
        renderChoice()
    }

    fun close() {
        val view = root ?: return
        settling?.cancel()
        renderer.cancel()
        busy = false
        root = null
        host.removeView(view)
        host.isMotionEventSplittingEnabled = splitting
        onShown(false)
    }

    /** The part of the map the window may take: clear of the status bar and the toolbar above,
     *  the size line and the navigation bar below. */
    private fun area(): Box {
        val insets = ViewCompat.getRootWindowInsets(host)?.getInsets(WindowInsetsCompat.Type.systemBars())
        val location = IntArray(2).also(host::getLocationOnScreen)
        val screenH = host.rootView.height
        val topInset = ((insets?.top ?: 0) - location[1]).coerceAtLeast(0)
        val bottomInset = (location[1] + host.height - (screenH - (insets?.bottom ?: 0))).coerceAtLeast(0)
        val margins = ViewfinderMargins(
            top = topInset + TOOLBAR_ROOM_DP * density,
            bottom = bottomInset + SIZE_ROOM_DP * density,
            side = SIDE_DP * density,
            minWindow = minWindow(),
        )
        return Viewfinder.area(host.width.toFloat(), host.height.toFloat(), margins)
    }

    private fun minWindow() = MIN_WINDOW_DP * density

    private fun setWindow(box: Box) {
        if (!isOpen) return
        frame.window = box
        place()
    }

    /** The window's shape: a preset's own, or a Custom window's as it is now. */
    private fun windowAspect(): Float? =
        choice.aspect ?: frame.window.takeIf { it.w > 0 && it.h > 0 }?.let { it.w / it.h }

    /** The image's size under the window, and everything placed around the window. */
    private fun place() {
        val box = frame.window
        val (w, h) = outputSize(choice, box.w, box.h)
        sizeLine.text = context.getString(R.string.export_size, w, h)
        placeAround()
        // The toolbar and the lines change width with their text: again once measured.
        toolbar.post { if (isOpen) placeAround() }
    }

    /** The toolbar centred over the window, the size line and the error centred under it. */
    private fun placeAround() {
        val box = frame.window
        val gap = 8 * density
        val side = 8 * density
        toolbar.x = (box.centerX - toolbar.width / 2f).coerceIn(side, (host.width - toolbar.width - side).coerceAtLeast(side))
        toolbar.y = box.y - toolbar.height - gap
        sizeLine.x = box.centerX - sizeLine.width / 2f
        sizeLine.y = box.y + box.h + gap
        errorLine.x = (box.centerX - errorLine.width / 2f).coerceAtLeast(side)
        errorLine.y = box.y + box.h + gap
    }

    /** A crop's return to full size, the map easing in by the same zoom alongside. */
    private fun settle(crop: Box) {
        val instance = map() ?: return
        val settled = Viewfinder.settle(crop, area())
        val (cx, cy) = Viewfinder.settledCenter(settled, host.width.toFloat(), host.height.toFloat())
        val camera = instance.cameraPosition
        val target = instance.projection.fromScreenLocation(PointF(cx, cy))
        instance.animateCamera(
            CameraUpdateFactory.newCameraPosition(
                CameraPosition.Builder().target(target).zoom(camera.zoom + settled.zoomDelta).bearing(camera.bearing).build(),
            ),
            SETTLE_MS.toInt(),
        )
        settling?.cancel()
        settling = ValueAnimator.ofFloat(0f, 1f).apply {
            duration = SETTLE_MS
            interpolator = DecelerateInterpolator()
            addUpdateListener { animation ->
                val t = animation.animatedValue as Float
                val to = settled.window
                setWindow(Box(crop.x + (to.x - crop.x) * t, crop.y + (to.y - crop.y) * t, crop.w + (to.w - crop.w) * t, crop.h + (to.h - crop.h) * t))
            }
            start()
        }
    }

    private fun label(choice: ExportChoice, full: Boolean): String = when (choice) {
        is ExportChoice.Custom ->
            if (full) context.getString(R.string.export_custom_full, choice.size.label, choice.size.longSidePx)
            else context.getString(R.string.export_custom, choice.size.label)
        is ExportChoice.Preset -> context.getString(R.string.export_preset, choice.preset.platform, context.getString(rowLabel(choice.preset.row)))
    }

    private fun rowLabel(row: PresetRow): Int = when (row) {
        PresetRow.SQUARE -> R.string.export_row_square
        PresetRow.PORTRAIT -> R.string.export_row_portrait
        PresetRow.LANDSCAPE -> R.string.export_row_landscape
        PresetRow.STORY -> R.string.export_row_story
    }

    /** The one menu: Custom at each size, then each platform's presets, a divider between. */
    private fun showChoices() {
        if (busy) return
        val popup = PopupMenu(context, choiceButton)
        popup.menu.setGroupDividerEnabled(true)
        ExportChoice.ALL.forEachIndexed { index, entry ->
            val group = when (entry) {
                is ExportChoice.Custom -> 0
                is ExportChoice.Preset -> 1 + listOf("Instagram", "Facebook", "X").indexOf(entry.preset.platform)
            }
            popup.menu.add(group, index, Menu.NONE, label(entry, full = true))
        }
        // One choice across the whole menu, so a radio mark, not a checkbox; set per group,
        // and only the chosen entry's group has a mark.
        (0..3).forEach { group -> popup.menu.setGroupCheckable(group, true, true) }
        popup.menu.findItem(ExportChoice.ALL.indexOf(choice))?.isChecked = true
        popup.setOnMenuItemClickListener { item ->
            pick(ExportChoice.ALL[item.itemId])
            true
        }
        popup.show()
    }

    /** Another Custom size keeps the window; any other entry fits a window of its shape. */
    private fun pick(next: ExportChoice) {
        val keepWindow = next is ExportChoice.Custom && choice is ExportChoice.Custom
        choice = next
        if (next is ExportChoice.Custom) lastChoice = next
        renderChoice(keepWindow)
    }

    private fun renderChoice(keepWindow: Boolean = false) {
        choiceButton.text = label(choice, full = false)
        errorLine.visibility = View.GONE
        sizeLine.visibility = View.VISIBLE
        if (keepWindow && frame.window.w > 0) {
            place()
        } else {
            host.post { if (isOpen) setWindow(Viewfinder.fit(choice.aspect, area())) }
        }
    }

    private fun setBusy(on: Boolean) {
        busy = on
        frame.locked = on
        captureButton.isEnabled = !on
        captureButton.icon = if (on) null else AppCompatResources.getDrawable(context, R.drawable.ic_camera)
        busyIndicator.visibility = if (on) View.VISIBLE else View.GONE
        choiceButton.isEnabled = !on
    }

    private fun capture() {
        val instance = map() ?: return
        if (busy) return
        setBusy(true)
        errorLine.visibility = View.GONE
        sizeLine.visibility = View.VISIBLE
        val box = frame.window
        val (widthPx, heightPx) = outputSize(choice, box.w, box.h)
        val camera = instance.cameraPosition
        val center = instance.projection.fromScreenLocation(PointF(box.centerX, box.centerY))
        val request = MapImageRenderer.Request(
            styleUrl = styleUrl(),
            center = center,
            zoom = camera.zoom,
            bearing = camera.bearing,
            windowDpW = box.w / density,
            widthPx = widthPx,
            heightPx = heightPx,
        )
        var rendered: Style? = null
        val dark = darkBase()
        val satellite = satelliteOn()
        renderer.render(
            request,
            prepare = { style ->
                prepare(style)
                rendered = style
            },
        ) { result ->
            result.fold(
                onSuccess = { bitmap ->
                    // From the served style document, the line the web bakes in too: a loaded
                    // source reports its tile archive's own credit instead, which leaves
                    // Protomaps out.
                    val json = rendered?.json
                    val credit = ImageMarks.creditText(
                        sourceAttribution(json, BASEMAP_SOURCE_ID),
                        if (satellite) sourceAttribution(json, SATELLITE_SOURCE_ID) else null,
                    )
                    finish(bitmap, widthPx, heightPx, credit, dark)
                },
                onFailure = { error -> fail(error) },
            )
        }
    }

    private fun sourceAttribution(styleJson: String?, sourceId: String): String? = runCatching {
        JSONObject(styleJson ?: return null).getJSONObject("sources").optJSONObject(sourceId)?.optString("attribution")
    }.getOrNull()

    /** The marks baked in, the image saved and the share sheet opened, off the main thread for
     *  the heavy part. */
    private fun finish(snapshot: Bitmap, widthPx: Int, heightPx: Int, credit: String, dark: Boolean) {
        io.execute {
            val saved = runCatching {
                val image = if (snapshot.width == widthPx && snapshot.height == heightPx && snapshot.isMutable) {
                    snapshot
                } else {
                    snapshot.scale(widthPx, heightPx).copy(Bitmap.Config.ARGB_8888, true)
                }
                ImageMarks.draw(context, image, credit, dark)
                save(image)
            }
            main.post {
                saved.fold(
                    onSuccess = { uri ->
                        close()
                        Toast.makeText(context, R.string.export_saved, Toast.LENGTH_SHORT).show()
                        share(uri)
                    },
                    onFailure = { error -> fail(error) },
                )
            }
        }
    }

    private fun fail(error: Throwable) {
        Log.w(TAG, "map export failed", error)
        if (!isOpen) return
        setBusy(false)
        errorLine.text = context.getString(R.string.export_failed)
        sizeLine.visibility = View.INVISIBLE
        errorLine.visibility = View.VISIBLE
        errorLine.post { if (isOpen) placeAround() }
    }

    /** Into Pictures/HoldMyTrack through MediaStore, which needs no permission for the app's
     *  own images; its URI is what the share sheet hands on. */
    private fun save(image: Bitmap): Uri {
        val resolver = context.contentResolver
        val values = ContentValues().apply {
            put(MediaStore.Images.Media.DISPLAY_NAME, "holdmytrack-${LocalDateTime.now().format(FILE_TIME)}.png")
            put(MediaStore.Images.Media.MIME_TYPE, "image/png")
            put(MediaStore.Images.Media.RELATIVE_PATH, "Pictures/HoldMyTrack")
            put(MediaStore.Images.Media.IS_PENDING, 1)
        }
        val uri = resolver.insert(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, values)
            ?: error("export: MediaStore gave no place for the image")
        try {
            resolver.openOutputStream(uri)?.use { out -> image.compress(Bitmap.CompressFormat.PNG, 100, out) }
                ?: error("export: could not write the image")
            resolver.update(uri, ContentValues().apply { put(MediaStore.Images.Media.IS_PENDING, 0) }, null, null)
        } catch (error: Throwable) {
            resolver.delete(uri, null, null)
            throw error
        }
        return uri
    }

    private fun share(uri: Uri) {
        val send = Intent(Intent.ACTION_SEND)
            .setType("image/png")
            .putExtra(Intent.EXTRA_STREAM, uri)
            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        // The share sheet's preview of the image.
        send.clipData = ClipData.newRawUri(null, uri)
        context.startActivity(Intent.createChooser(send, context.getString(R.string.export_share_title)))
    }

    companion object {
        private const val TAG = "ExportViewfinder"
        private const val TOOLBAR_ROOM_DP = 80f
        private const val SIZE_ROOM_DP = 40f
        private const val SIDE_DP = 16f
        private const val MIN_WINDOW_DP = 64f
        private const val SETTLE_MS = 300L
        /** The served style's basemap and imagery sources (`apps/web/src/map/style.ts`). */
        private const val BASEMAP_SOURCE_ID = "protomaps"
        private const val SATELLITE_SOURCE_ID = "satellite"
        private val FILE_TIME = DateTimeFormatter.ofPattern("yyyy-MM-dd-HHmmss")
        private val io = Executors.newSingleThreadExecutor()

        /** The Custom size last picked, kept while the app runs, the web's per-visit memory. */
        private var lastChoice: ExportChoice = ExportChoice.DEFAULT
    }
}
