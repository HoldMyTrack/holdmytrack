package dev.holdmytrack.android.map

import android.view.View
import android.widget.TextView
import androidx.core.view.AccessibilityDelegateCompat
import androidx.core.view.ViewCompat
import androidx.core.view.accessibility.AccessibilityNodeInfoCompat
import androidx.core.view.isVisible
import com.google.android.material.button.MaterialButton
import dev.holdmytrack.android.R
import dev.holdmytrack.android.ui.LargeText

/** What the mode bar and its notices depend on, read at each render ([MapChromeRules]). */
data class ModeBarState(
    val mode: MapMode,
    val sessionReady: Boolean,
    val recording: Boolean,
    val editOpen: Boolean,
    val selectionShown: Boolean,
    val exportOpen: Boolean,
    /** Fog and Heatmap are still being re-rendered (`CoverageWatch`). */
    val coverageRendering: Boolean,
)

/**
 * The Normal / Fog of War / Heatmap toggle floating top-start, and what it says about the mode:
 * the chip naming what Fog or Heatmap shows, the level in view, and that coverage is still being
 * updated (`docs/SPEC.md` FR-4.2).
 */
class ModeBar(root: View, onPick: (MapMode) -> Unit, private val state: () -> ModeBarState) {
    private val bar: View = root.findViewById(R.id.mode_bar)
    private val layersPanel: View = root.findViewById(R.id.layers_panel)
    private val chip: TextView = root.findViewById(R.id.mode_chip)
    private val coverageNotice: View = root.findViewById(R.id.coverage_notice)
    private val zoomLevelNotice = ZoomLevelNotice(root.findViewById(R.id.zoom_level_notice))
    private val buttons: Map<MapMode, MaterialButton> = mapOf(
        MapMode.NORMAL to root.findViewById(R.id.mode_normal),
        MapMode.FOG to root.findViewById(R.id.mode_fog),
        MapMode.HEATMAP to root.findViewById(R.id.mode_heatmap),
    )

    init {
        // One choice of three: each reads as a radio button, "1 of 3" (map/MapModeButton).
        buttons.values.forEachIndexed { index, button -> (button as? MapModeButton)?.position = index }
        ViewCompat.setAccessibilityDelegate(bar, object : AccessibilityDelegateCompat() {
            override fun onInitializeAccessibilityNodeInfo(host: View, info: AccessibilityNodeInfoCompat) {
                super.onInitializeAccessibilityNodeInfo(host, info)
                info.setCollectionInfo(
                    AccessibilityNodeInfoCompat.CollectionInfoCompat.obtain(1, buttons.size, false, AccessibilityNodeInfoCompat.CollectionInfoCompat.SELECTION_MODE_SINGLE),
                )
            }
        })
        buttons.forEach { (value, button) ->
            button.setOnClickListener { onPick(value) }
            button.minWidth = ChromeInsets.minTouchTargetPx(button)
            button.minimumWidth = ChromeInsets.minTouchTargetPx(button)
        }
    }

    /** Checks [mode]'s button. */
    fun check(mode: MapMode) {
        buttons.forEach { (value, button) ->
            val active = value == mode
            button.isChecked = active
            renderLabel(button, active)
        }
    }

    /** The toggle shows once the session allows, hidden while recording, editing or checking rows.
     *  Layers stays while recording: the base map and paths are about where the recording goes. */
    fun render() {
        val s = state()
        if (!s.sessionReady) return
        bar.isVisible = MapChromeRules.modeBarShown(s.recording, s.editOpen, s.selectionShown)
        layersPanel.visibility = View.VISIBLE
        renderNotices()
    }

    /** The level in view, the coverage notice and the chip. */
    fun renderNotices() {
        val s = state()
        zoomLevelNotice.render(s.mode, active = s.sessionReady && !s.recording)
        renderCoverage()
        renderChip()
    }

    /** "Your map is still being updated…" (`docs/SPEC.md` FR-4.2), in Fog and Heatmap while coverage renders. */
    fun renderCoverage() {
        val s = state()
        coverageNotice.isVisible = MapChromeRules.coverageNoticeShown(s.coverageRendering, s.mode, s.sessionReady, s.recording)
    }

    /** Names what Fog or Heatmap shows, since neither takes the date range. */
    fun renderChip() {
        val s = state()
        val text = when (s.mode) {
            MapMode.NORMAL -> null
            MapMode.FOG -> R.string.mode_chip_fog
            MapMode.HEATMAP -> R.string.mode_chip_heatmap
        }
        chip.isVisible = MapChromeRules.modeChipShown(s.mode, s.sessionReady, s.recording, s.exportOpen)
        if (text != null) chip.setText(text)
    }

    fun onCameraIdle(tier: ZoomTier) = zoomLevelNotice.onCameraIdle(tier)

    /**
     * At a large font size only the active mode keeps its label; the others show icons
     * (`ui/LargeText`), still named for TalkBack. The layout's label is kept in the button's tag.
     */
    private fun renderLabel(button: MaterialButton, active: Boolean) {
        val label = (button.tag as? CharSequence) ?: button.text.also { button.tag = it }
        button.contentDescription = label
        if (!LargeText.isOn(button.resources)) return
        button.text = if (active) label else ""
        button.iconPadding = if (active) button.resources.getDimensionPixelSize(R.dimen.hmt_space_6) else 0
    }
}
