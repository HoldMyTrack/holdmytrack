package dev.holdmytrack.android.map

import android.annotation.SuppressLint
import android.os.SystemClock
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.CheckBox
import android.widget.PopupWindow
import android.widget.RadioGroup
import android.widget.TextView
import androidx.core.view.isVisible
import com.google.android.material.button.MaterialButton
import dev.holdmytrack.android.R

/**
 * The Layers button under the burger and the menu it opens — the web's `OverlaysMenu`
 * (`docs/SPEC.md` FR-4.13, FR-4.14): the Base map, Map or Satellite, when the served style
 * carries imagery; then Paths — Trails, Tracks and Bike paths — and Points of interest, one
 * entry per [MapSpots.Category], each switched on and off over any mode.
 *
 * The button is an icon, filled like the active map mode while any path or place is on, with a
 * badge on its corner counting them; the Base map is a choice between two, not an overlay, so
 * neither counts it. A tap on the button toggles the
 * menu, and a tap outside it or Back closes it, like the Activities panel's Type dropdown.
 */
class LayersMenu(
    private val button: MaterialButton,
    private val count: TextView,
    private val paths: () -> MapPaths.Paths,
    private val satellite: () -> Boolean,
    private val spots: () -> List<MapSpots.Category>,
    private val onPaths: (MapPaths.Paths) -> Unit,
    private val onSatellite: (Boolean) -> Unit,
    private val onSpots: (List<MapSpots.Category>) -> Unit,
) {
    private val res = button.resources
    private var popup: PopupWindow? = null
    private var closedAt = 0L

    /** Whether the served style carries imagery; without it the menu has no Base map group. */
    var satelliteAvailable = false
        set(value) {
            field = value
            dismiss()
        }

    init {
        button.setOnClickListener {
            // The button is checkable for the filled look, so a tap just flipped it; that follows
            // the paths, not taps.
            render()
            // A tap on the button while the menu is open already closed it, as a tap outside the
            // popup, just before this click lands; reopening it would make the button unable to
            // close its own menu.
            if (SystemClock.uptimeMillis() - closedAt < REOPEN_GUARD_MS) return@setOnClickListener
            show()
        }
        render()
    }

    /** The filled look and the badge, from the saved choice. */
    fun render() {
        val on = paths().count + spots().size
        button.isChecked = on > 0
        count.isVisible = on > 0
        count.text = on.toString()
        val label = res.getString(R.string.layers_button)
        button.contentDescription = if (on > 0) "$label, $on" else label
    }

    fun dismiss() {
        popup?.dismiss()
    }

    @SuppressLint("InflateParams") // A popup's content has no parent to inflate against.
    private fun show() {
        if (popup != null) return
        val content = LayoutInflater.from(button.context).inflate(R.layout.popup_layers, null)

        content.findViewById<View>(R.id.layers_basemap_group).isVisible = satelliteAvailable
        content.findViewById<RadioGroup>(R.id.layers_basemap).apply {
            check(if (satellite()) R.id.layers_basemap_satellite else R.id.layers_basemap_map)
            setOnCheckedChangeListener { _, id -> onSatellite(id == R.id.layers_basemap_satellite) }
        }

        val current = paths()
        fun bind(id: Int, checked: Boolean, next: (MapPaths.Paths, Boolean) -> MapPaths.Paths) {
            content.findViewById<CheckBox>(id).apply {
                isChecked = checked
                setOnCheckedChangeListener { _, on ->
                    onPaths(next(paths(), on))
                    render()
                }
            }
        }
        bind(R.id.layers_trails, current.trails) { p, on -> p.copy(trails = on) }
        bind(R.id.layers_tracks, current.tracks) { p, on -> p.copy(tracks = on) }
        bind(R.id.layers_bike_paths, current.bikePaths) { p, on -> p.copy(bikePaths = on) }

        for ((category, id) in SPOT_BOXES) {
            content.findViewById<CheckBox>(id).apply {
                isChecked = category in spots()
                setOnCheckedChangeListener { _, on ->
                    val before = spots()
                    onSpots(MapSpots.Category.entries.filter { if (it == category) on else it in before })
                    render()
                }
            }
        }

        // The Tracks explanation: shown and hidden again by its info button, without ticking the
        // box; it closes with the menu, since each opening inflates the menu afresh.
        val info = content.findViewById<TextView>(R.id.layers_tracks_info)
        content.findViewById<View>(R.id.layers_tracks_info_button).setOnClickListener {
            info.isVisible = !info.isVisible
        }

        // A fixed width, the web panel's 14rem, so the Tracks explanation wraps inside it rather
        // than widening the menu when it opens.
        val width = (MENU_WIDTH_DP * res.displayMetrics.density).toInt()
        val window = PopupWindow(content, width, ViewGroup.LayoutParams.WRAP_CONTENT, true)
        window.elevation = res.getDimension(R.dimen.hmt_space_8)
        window.setOnDismissListener {
            popup = null
            closedAt = SystemClock.uptimeMillis()
        }
        popup = window
        // Under the button's panel, lined up with its edge, the row's own 6dp gap below it.
        window.showAsDropDown(button.parent as View, 0, res.getDimensionPixelSize(R.dimen.hmt_space_6))
    }

    private companion object {
        val SPOT_BOXES = listOf(
            MapSpots.Category.PLAYGROUND to R.id.layers_spots_playground,
            MapSpots.Category.DOG_PARK to R.id.layers_spots_dog_park,
            MapSpots.Category.MONUMENT to R.id.layers_spots_monument,
            MapSpots.Category.VIEWPOINT to R.id.layers_spots_viewpoint,
            MapSpots.Category.HISTORY to R.id.layers_spots_history,
        )

        const val REOPEN_GUARD_MS = 300L
        const val MENU_WIDTH_DP = 224
    }
}
