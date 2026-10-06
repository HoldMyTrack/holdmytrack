package dev.holdmytrack.android.map

import android.content.Context
import android.util.AttributeSet
import android.view.accessibility.AccessibilityNodeInfo
import android.widget.RadioButton
import com.google.android.material.R as MaterialR
import com.google.android.material.button.MaterialButton

/**
 * One of the map's Normal, Fog and Heatmap buttons: a checkable [MaterialButton] that TalkBack
 * reads as a radio button, since exactly one of the three is on. `MaterialButton` names itself a
 * toggle outside a `MaterialButtonToggleGroup`, and sets that last, so the name is set here after
 * it. [position] is its place in the row, read out as "1 of 3".
 */
class MapModeButton @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
    defStyleAttr: Int = MaterialR.attr.materialButtonStyle,
) : MaterialButton(context, attrs, defStyleAttr) {

    var position = 0

    override fun getAccessibilityClassName(): CharSequence = RadioButton::class.java.name

    override fun onInitializeAccessibilityNodeInfo(info: AccessibilityNodeInfo) {
        super.onInitializeAccessibilityNodeInfo(info)
        info.className = RadioButton::class.java.name
        info.collectionItemInfo = AccessibilityNodeInfo.CollectionItemInfo(0, 1, position, 1, false, isChecked)
    }
}
