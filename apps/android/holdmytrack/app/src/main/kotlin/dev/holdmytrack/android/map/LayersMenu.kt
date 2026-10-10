package dev.holdmytrack.android.map

import android.annotation.SuppressLint
import android.os.SystemClock
import android.view.Gravity
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.CheckBox
import android.widget.PopupWindow
import android.widget.TextView
import androidx.core.view.isVisible
import com.google.android.material.button.MaterialButton
import com.google.android.material.materialswitch.MaterialSwitch
import dev.holdmytrack.android.R

/**
 * The Layers button on the map's rail and the menu it opens — the web's `OverlaysMenu`
 * (`docs/SPEC.md` FR-4.13): Paths — Trails, Tracks and Bike paths — and Points of interest, one
 * entry per [MapSpots.Category], each picked over any mode.
 *
 * The button is filled like the active map mode while picks show, with a badge on its corner
 * counting them, greyed while they're hidden. The menu opens with a switch that shows or hides
 * every pick at once and keeps them ([shown]) — picking an entry while it's off turns it back on,
 * or the pick would seem to do nothing; with nothing picked it can't be changed — and, where the
 * served style has imagery ([satellite] isn't null), Satellite (`MapSatellite`). A tap on the
 * button toggles the menu, and a tap outside it or Back closes it, like the Activities panel's
 * Type dropdown.
 */
class LayersMenu(
    private val button: MaterialButton,
    private val count: TextView,
    private val paths: () -> MapPaths.Paths,
    private val spots: () -> List<MapSpots.Category>,
    private val shown: () -> Boolean,
    private val onPaths: (MapPaths.Paths) -> Unit,
    private val onSpots: (List<MapSpots.Category>) -> Unit,
    private val onShown: (Boolean) -> Unit,
    private val satellite: () -> Boolean?,
    private val onSatellite: (Boolean) -> Unit,
) {
    private val res = button.resources
    private var popup: PopupWindow? = null
    private var master: MaterialSwitch? = null
    private var closedAt = 0L

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

    /** The filled look, the badge and the open menu's switch, from the saved choice. */
    fun render() {
        val picked = paths().count + spots().size
        val on = shown()
        master?.let {
            it.isChecked = on
            it.isEnabled = picked > 0
            it.tooltipText = if (picked > 0) null else res.getString(R.string.layers_master_empty)
        }
        button.isChecked = on && picked > 0
        count.isVisible = picked > 0
        count.alpha = if (on) 1f else OFF_ALPHA
        count.text = picked.toString()
        val label = res.getString(R.string.layers_button)
        button.contentDescription = if (picked > 0) "$label, $picked" else label
    }

    /** A pick turns the checkbox back on before it's applied, so it shows. */
    private fun picked(on: Boolean) {
        if (on && !shown()) onShown(true)
    }

    fun dismiss() {
        popup?.dismiss()
    }

    @SuppressLint("InflateParams") // A popup's content has no parent to inflate against.
    private fun show() {
        if (popup != null) return
        val content = LayoutInflater.from(button.context).inflate(R.layout.popup_layers, null)

        // A click, not a checked-change listener: render() sets the switch from the saved choice.
        master = content.findViewById<MaterialSwitch>(R.id.layers_master).apply {
            setOnClickListener {
                onShown(isChecked)
                render()
            }
        }
        content.findViewById<MaterialSwitch>(R.id.layers_satellite).apply {
            val on = satellite()
            isVisible = on != null
            isChecked = on == true
            setOnClickListener { onSatellite(isChecked) }
        }

        val current = paths()
        fun bind(id: Int, checked: Boolean, next: (MapPaths.Paths, Boolean) -> MapPaths.Paths) {
            content.findViewById<CheckBox>(id).apply {
                isChecked = checked
                setOnCheckedChangeListener { _, on ->
                    picked(on)
                    onPaths(next(paths(), on))
                    render()
                }
            }
        }
        bind(R.id.layers_trails, current.trails) { p, on -> p.copy(trails = on) }
        bind(R.id.layers_tracks, current.tracks) { p, on -> p.copy(tracks = on) }
        bind(R.id.layers_bike_paths, current.bikePaths) { p, on -> p.copy(bikePaths = on) }
        bind(R.id.layers_shared_paths, current.sharedPaths) { p, on -> p.copy(sharedPaths = on) }

        for ((category, id) in SPOT_BOXES) {
            content.findViewById<CheckBox>(id).apply {
                isChecked = category in spots()
                setOnCheckedChangeListener { _, on ->
                    picked(on)
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
            master = null
            closedAt = SystemClock.uptimeMillis()
        }
        popup = window
        render()
        // Under the button's panel, lined up with its end edge, 6dp below it.
        window.showAsDropDown(button.parent as View, 0, res.getDimensionPixelSize(R.dimen.hmt_space_6), Gravity.END)
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

        /** The badge while the checkbox is off: the web's greyed count. */
        const val OFF_ALPHA = 0.55f
    }
}
