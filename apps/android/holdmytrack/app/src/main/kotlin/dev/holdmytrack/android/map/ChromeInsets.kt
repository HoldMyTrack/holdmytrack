package dev.holdmytrack.android.map

import android.os.Bundle
import android.view.View
import android.view.ViewGroup.MarginLayoutParams
import android.view.WindowInsets
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.isVisible
import dev.holdmytrack.android.R
import dev.holdmytrack.android.panel.ActivitiesPanel
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapView

/**
 * Keeps the map's floating chrome and MapLibre's own controls clear of the system bars, the top
 * row, the sheet and the editors along the bottom. `MainActivity`'s bottom bar takes the gesture
 * inset, so the bottom system inset here is 0.
 *
 * Required from API 35, which draws every app edge to edge: the top row's `LinearLayout` has no
 * `fitsSystemWindows`, so without this it sits under the status bar's icons. The map itself fills
 * the screen. Insets are added to each view's laid-out margin, and returned unconsumed.
 */
class ChromeInsets(
    private val root: View,
    private val topBar: View,
    private val topChrome: View,
    private val notice: View,
    private val sheet: View,
    private val panel: ActivitiesPanel,
    private val map: () -> MapLibreMap?,
) {
    /** The Edit window and the Private location editor, along the bottom of the map. */
    private val editors: List<View> = listOf(root.findViewById(R.id.edit_window), root.findViewById(R.id.private_editor))

    /** The status bar's height. The inset and the map instance arrive in either order, so both
     *  paths call [applyCompassMargin]. */
    private var systemBarInsetTop = 0

    /** The compass's default top margin, captured once so repeated inset callbacks don't
     *  compound the status bar height. */
    private var compassBaseMarginTop = 0
    private var compassBaseMarginCaptured = false

    /** The logo's and attribution's default bottom margins, captured once. Trap: MapLibre
     *  restores its UI settings with the lift applied, so these are carried through [save]
     *  rather than captured again, which would lift twice. */
    private var logoBaseMarginBottom = 0
    private var attributionBaseMarginBottom = 0
    private var attributionBaseCaptured = false

    /** Starts following the insets and the layout of what the chrome must clear. */
    fun install() {
        val barTopMargin = (topBar.layoutParams as MarginLayoutParams).topMargin
        root.setOnApplyWindowInsetsListener { _, insets ->
            val bars = insets.getInsets(WindowInsets.Type.systemBars())
            (topBar.layoutParams as MarginLayoutParams).topMargin = barTopMargin + bars.top
            topBar.requestLayout()
            systemBarInsetTop = bars.top
            applyCompassMargin()
            insets
        }
        // The attribution sits above an editor, whose height is only known once it's laid out.
        for (editor in editors) {
            editor.addOnLayoutChangeListener { _, _, _, _, _, _, _, _, _ -> applyAttributionMargin() }
        }
        // The compass sits below the top row and any notice under it, whose heights are only
        // known once they're laid out; the expanded sheet stops under the row likewise. The
        // attribution follows the sheet's head (`MapFragment.onSheetChanged`).
        topChrome.addOnLayoutChangeListener { _, _, _, _, _, _, _, _, _ ->
            applyCompassMargin()
            placeEditors()
            panel.setExpandedOffset(topBar.bottom + root.resources.getDimensionPixelSize(R.dimen.hmt_space_8), root.height)
        }
        root.addOnLayoutChangeListener { _, _, top, _, bottom, _, oldTop, _, oldBottom ->
            if (bottom - top != oldBottom - oldTop) {
                panel.setExpandedOffset(topBar.bottom + root.resources.getDimensionPixelSize(R.dimen.hmt_space_8), bottom - top)
            }
        }
    }

    /** Moves MapLibre's compass below the status bar and the top row, whose rail shares its top-end corner. */
    fun applyCompassMargin() {
        val instance = map() ?: return
        val settings = instance.uiSettings
        if (!compassBaseMarginCaptured) {
            compassBaseMarginTop = settings.compassMarginTop
            compassBaseMarginCaptured = true
        }
        // Below the top row and any notice; before the row is laid out, below the status bar.
        val belowTopBar = maxOf(topBar.bottom, if (notice.isVisible) notice.bottom else 0)
        settings.setCompassMargins(
            settings.compassMarginLeft,
            compassBaseMarginTop + maxOf(systemBarInsetTop, belowTopBar),
            settings.compassMarginRight,
            settings.compassMarginBottom,
        )
    }

    /**
     * Lifts MapLibre's logo and attribution above the collapsed sheet or an open editor: the
     * attribution is the basemap's ODbL credit and must stay visible (`FR-2.4`). An expanded
     * sheet covers them as it covers the map.
     */
    fun applyAttributionMargin() {
        val instance = map() ?: return
        val settings = instance.uiSettings
        if (!attributionBaseCaptured) {
            logoBaseMarginBottom = settings.logoMarginBottom
            attributionBaseMarginBottom = settings.attributionMarginBottom
            attributionBaseCaptured = true
        }
        val editor = editors.firstOrNull { it.isVisible }
        val above = when {
            editor != null -> root.height - editor.top
            sheet.isVisible -> panel.peekHeight
            else -> 0
        }
        settings.setLogoMargins(settings.logoMarginLeft, settings.logoMarginTop, settings.logoMarginRight, logoBaseMarginBottom + above)
        settings.setAttributionMargins(
            settings.attributionMarginLeft,
            settings.attributionMarginTop,
            settings.attributionMarginRight,
            attributionBaseMarginBottom + above,
        )
    }

    /**
     * Pads MapLibre's 21dp attribution "i" out to the 48dp touch target. It has no id, so it's
     * found by its content description. Padding goes on its right and top only, so the icon stays
     * put. Trap: the margins belong to [applyAttributionMargin]; padding evenly through them put
     * the icon over the MapLibre logo.
     */
    fun enlargeAttributionTarget(mapView: MapView) {
        val found = ArrayList<View>()
        mapView.findViewsWithText(
            found,
            mapView.context.getString(org.maplibre.android.R.string.maplibre_attributionsIconContentDescription),
            View.FIND_VIEWS_WITH_CONTENT_DESCRIPTION,
        )
        val icon = found.firstOrNull() ?: return
        val target = minTouchTargetPx(root)
        val padX = (target - icon.width).coerceAtLeast(0)
        val padY = (target - icon.height).coerceAtLeast(0)
        if (padX == 0 && padY == 0) return
        icon.setPadding(0, padY, padX, 0)
    }

    /** The editors sit at the screen's bottom edge (the bottom bar hides meanwhile), so their
     *  margin clears the gesture bar. */
    fun placeEditors() {
        val insets = ViewCompat.getRootWindowInsets(root)
        val gesture = insets?.getInsets(WindowInsetsCompat.Type.navigationBars())?.bottom ?: 0
        val bottom = root.resources.getDimensionPixelSize(R.dimen.hmt_space_10) + gesture
        for (card in editors) {
            val params = card.layoutParams as MarginLayoutParams
            if (params.bottomMargin != bottom) {
                params.bottomMargin = bottom
                card.layoutParams = params
            }
        }
    }

    fun save(out: Bundle) {
        if (!attributionBaseCaptured) return
        out.putInt(STATE_LOGO_MARGIN, logoBaseMarginBottom)
        out.putInt(STATE_ATTRIBUTION_MARGIN, attributionBaseMarginBottom)
    }

    fun restore(saved: Bundle?) {
        if (saved?.containsKey(STATE_LOGO_MARGIN) != true) return
        logoBaseMarginBottom = saved.getInt(STATE_LOGO_MARGIN)
        attributionBaseMarginBottom = saved.getInt(STATE_ATTRIBUTION_MARGIN)
        attributionBaseCaptured = true
    }

    companion object {
        /** Material's and the platform's minimum touch target. */
        private const val MIN_TOUCH_TARGET_DP = 48

        private const val STATE_LOGO_MARGIN = "logo_margin"
        private const val STATE_ATTRIBUTION_MARGIN = "attribution_margin"

        /** The minimum touch target in pixels, rounded up. */
        fun minTouchTargetPx(view: View): Int = kotlin.math.ceil(MIN_TOUCH_TARGET_DP * view.resources.displayMetrics.density).toInt()
    }
}
