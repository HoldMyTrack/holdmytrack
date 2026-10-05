package dev.holdmytrack.android.map

import android.graphics.Typeface
import android.widget.TextView
import androidx.appcompat.content.res.AppCompatResources
import androidx.core.view.isVisible
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Spot
import org.maplibre.android.maps.MapLibreMap
import java.text.NumberFormat

/**
 * "Show in this area" (`docs/SPEC.md` FR-15.5), the web's `ui/ShowInArea.tsx`: the Spots tiles
 * start at [MapSpots.MIN_ZOOM], and between [MapSpots.IN_AREA_MIN_ZOOM] (a metro area in view)
 * and there a view holds too many places to load on every pan. So there they load on request,
 * for the visible map and the ticked categories, and stay drawn until the next request
 * ([MapSpots.setInArea]).
 *
 * [view] is the button, and, once a load is current — the map not moved and no category ticked
 * since — the line saying what it found, in the same pill. MapFragment tells it the camera
 * settled ([onCameraIdle]) and the categories changed ([setCategories]).
 */
class ShowInArea(private val view: TextView, private val map: MapLibreMap, private val onLoaded: (List<Spot>) -> Unit) {

    private val res = view.resources

    /** The web's 14px map-pin, in the label's color. */
    private val pin = AppCompatResources.getDrawable(view.context, R.drawable.ic_map_pin)!!.mutate().apply {
        val size = (PIN_DP * res.displayMetrics.density).toInt()
        setBounds(0, 0, size, size)
    }

    private var categories: List<MapSpots.Category> = emptyList()
    private var zoom = map.cameraPosition.zoom

    /** What the last load found, and for which categories. */
    private data class Loaded(val categories: List<MapSpots.Category>, val shown: Int, val total: Int)

    private var loaded: Loaded? = null
    private var moved = false
    private var loading = false
    private var failed = false

    /** Bumped per request, so only the latest one's answer lands (the web aborts the older). */
    private var request = 0

    init {
        view.setOnClickListener { load() }
        render()
    }

    fun setCategories(next: List<MapSpots.Category>) {
        categories = next
        render()
    }

    fun onCameraIdle() {
        zoom = map.cameraPosition.zoom
        moved = true
        render()
    }

    private fun load() {
        if (loading) return
        val bounds = map.projection.visibleRegion.latLngBounds
        val wanted = categories
        val id = ++request
        loading = true
        failed = false
        render()
        HoldMyTrackApi.spotsInArea(
            bounds.longitudeWest, bounds.latitudeSouth, bounds.longitudeEast, bounds.latitudeNorth,
            wanted.map { it.wire },
        ) { result ->
            if (id != request) return@spotsInArea
            loading = false
            result.onSuccess { found ->
                onLoaded(found.spots)
                loaded = Loaded(wanted, found.spots.size, found.total)
                moved = false
            }.onFailure { failed = true }
            render()
        }
    }

    private fun status(): String? {
        val last = loaded ?: return null
        if (moved || failed || !last.categories.containsAll(categories)) return null
        val format = NumberFormat.getIntegerInstance()
        return when {
            last.total == 0 -> res.getString(R.string.spots_in_area_none)
            last.shown < last.total -> res.getString(R.string.spots_in_area_truncated, format.format(last.shown), format.format(last.total))
            else -> res.getQuantityString(R.plurals.spots_in_area_count, last.total, format.format(last.total))
        }
    }

    private fun render() {
        val inRange = categories.isNotEmpty() && zoom >= MapSpots.IN_AREA_MIN_ZOOM && zoom < MapSpots.MIN_ZOOM
        view.isVisible = inRange
        if (!inRange) return
        val line = status()
        if (line != null) {
            view.text = line
            view.setTypeface(null, Typeface.NORMAL)
            view.setTextColor(view.context.getColor(R.color.hmt_ink_secondary))
            view.setCompoundDrawablesRelative(null, null, null, null)
            view.isClickable = false
            return
        }
        view.setText(
            when {
                loading -> R.string.spots_in_area_loading
                failed -> R.string.spots_in_area_failed
                else -> R.string.spots_in_area
            },
        )
        view.setTypeface(null, Typeface.BOLD)
        val color = view.context.getColor(if (loading) R.color.hmt_ink_muted else R.color.hmt_ink)
        view.setTextColor(color)
        pin.setTint(color)
        view.setCompoundDrawablesRelative(pin, null, null, null)
        view.isClickable = !loading
    }

    private companion object {
        const val PIN_DP = 14f
    }
}
