package dev.holdmytrack.android.map

import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.text.method.ScrollingMovementMethod
import android.view.View
import android.widget.TextView
import androidx.core.net.toUri
import androidx.core.view.isVisible
import com.google.android.material.button.MaterialButton
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.Spot
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.maps.MapLibreMap
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/**
 * A spot's popup (`docs/SPEC.md` FR-15.3), the web's `SpotPopup.tsx`: [view] (`view_spot_popup`)
 * over the map, above the spot's badge — or under it when there's no room above, below [top] —
 * and following it as the camera moves ([place]). One at a time; [show] replaces it.
 *
 * MapFragment closes it on a tap elsewhere on the map, when its category is unticked and when a
 * track edit takes the map. Capture ([onCapture]) starts capture mode on the place
 * ([CaptureMode]) — offered for a place not yet captured, and not to a demo account, which
 * can't capture.
 */
class SpotPopup(private val view: View, private val top: () -> Int, private val onCapture: (Spot) -> Unit) {

    private val context = view.context
    private val res = view.resources
    private val title = view.findViewById<TextView>(R.id.spot_popup_title)
    private val meta = view.findViewById<TextView>(R.id.spot_popup_meta)
    private val description = view.findViewById<TextView>(R.id.spot_popup_description)
    private val inscription = view.findViewById<TextView>(R.id.spot_popup_inscription)
    private val address = view.findViewById<TextView>(R.id.spot_popup_address)
    private val captured = view.findViewById<TextView>(R.id.spot_popup_captured)
    private val copy = view.findViewById<MaterialButton>(R.id.spot_popup_copy)
    private val wikipedia = view.findViewById<MaterialButton>(R.id.spot_popup_wikipedia)
    private val capture = view.findViewById<MaterialButton>(R.id.spot_popup_capture)

    private var map: MapLibreMap? = null

    /** The open spot, or null. */
    var spot: Spot? = null
        private set

    private val resetCopy = Runnable { renderCopy(copied = false) }

    init {
        view.findViewById<View>(R.id.spot_popup_close).setOnClickListener { close() }
        // Past six lines a long description or inscription scrolls within the popup rather
        // than stretching it over the map.
        description.movementMethod = ScrollingMovementMethod()
        inscription.movementMethod = ScrollingMovementMethod()
        copy.setOnClickListener { spot?.let(::copyAddress) }
        wikipedia.setOnClickListener { spot?.wikipedia?.let(::openWikipedia) }
        capture.setOnClickListener { spot?.let(onCapture) }
        // The flag at the line's text size, not the vector's own 24dp.
        captured.compoundDrawablesRelative[0]?.let { flag ->
            val size = (FLAG_DP * res.displayMetrics.density).toInt()
            flag.setBounds(0, 0, size, size)
            captured.setCompoundDrawablesRelative(flag, null, null, null)
        }
        // Placed again once it has its size — the first placing of new content happens before.
        view.addOnLayoutChangeListener { _, _, top, _, bottom, _, oldTop, _, oldBottom ->
            if (bottom - top != oldBottom - oldTop) place()
        }
    }

    fun show(map: MapLibreMap, next: Spot) {
        this.map = map
        spot = next
        val category = MapSpots.Category.fromWire(next.category)?.let { res.getString(it.label) } ?: next.category
        title.text = next.name ?: category
        // Under the title: the category (unless it already is the title), the memorial type, the date.
        val line = listOfNotNull(
            category.takeIf { next.name != null },
            next.memorial?.let(SpotText::memorialLabel),
            next.startDate?.let { res.getString(R.string.spots_since, it) },
        )
        meta.text = line.joinToString(" · ")
        meta.isVisible = line.isNotEmpty()
        setText(description, next.description)
        setText(inscription, next.inscription)
        setText(address, next.address)
        renderCaptured()
        wikipedia.isVisible = next.wikipedia != null
        view.removeCallbacks(resetCopy)
        renderCopy(copied = false)
        view.isVisible = true
        place()
    }

    /** The captured line again — the account's captures arrived, or this place was just
     *  captured ([MapSpots.captured]). */
    fun renderCaptured() {
        val at = spot?.let { MapSpots.captured[it.id] }
        captured.text = at?.let {
            val date = DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM).withLocale(Locale.getDefault()).format(it.atZone(ZoneId.systemDefault()))
            res.getString(R.string.spots_captured_on, date)
        }
        captured.isVisible = at != null
        capture.isVisible = spot != null && at == null && !Session.isDemo
    }

    fun close() {
        spot = null
        view.removeCallbacks(resetCopy)
        view.isVisible = false
    }

    /** Over the spot again — on every camera move while it's open. */
    fun place() {
        val open = spot ?: return
        val instance = map ?: return
        val point = instance.projection.toScreenLocation(LatLng(open.lat, open.lon))
        val density = res.displayMetrics.density
        val parent = view.parent as? View ?: return
        val gutter = GUTTER_DP * density
        val offset = OFFSET_DP * density
        val width = view.width.takeIf { it > 0 } ?: (WIDTH_DP * density).toInt()
        val height = view.height
        view.translationX = (point.x - width / 2f).coerceIn(gutter, (parent.width - width - gutter).coerceAtLeast(gutter))
        val above = point.y - offset - height
        view.translationY = if (above >= top()) above else point.y + offset
    }

    private fun setText(target: TextView, value: String?) {
        target.text = value
        target.isVisible = value != null
        target.scrollTo(0, 0)
    }

    private fun renderCopy(copied: Boolean) {
        copy.setText(
            when {
                copied -> R.string.spots_copied
                spot?.address != null -> R.string.spots_copy_address
                else -> R.string.spots_copy_location
            },
        )
        copy.setIconResource(if (copied) R.drawable.ic_check else R.drawable.ic_copy)
    }

    private fun copyAddress(spot: Spot) {
        val clipboard = context.getSystemService(ClipboardManager::class.java) ?: return
        clipboard.setPrimaryClip(ClipData.newPlainText(res.getString(R.string.spots_copy_address), SpotText.address(spot)))
        renderCopy(copied = true)
        view.removeCallbacks(resetCopy)
        view.postDelayed(resetCopy, COPIED_MS)
    }

    private fun openWikipedia(tag: String) {
        try {
            context.startActivity(Intent(Intent.ACTION_VIEW, SpotText.wikipediaUrl(tag).toUri()))
        } catch (_: ActivityNotFoundException) {
            // No browser at all: nothing to open it in.
        }
    }

    private companion object {
        /** Half the 30dp badge, and a little air, between the spot and the popup's edge. */
        const val OFFSET_DP = 18f
        const val GUTTER_DP = 12f
        const val WIDTH_DP = 272f

        const val FLAG_DP = 14f

        /** How long "Copied" shows in place of Copy address. */
        const val COPIED_MS = 1500L
    }
}
