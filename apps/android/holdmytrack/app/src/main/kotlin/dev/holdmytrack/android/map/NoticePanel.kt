package dev.holdmytrack.android.map

import android.view.View
import android.widget.Button
import android.widget.TextView
import androidx.core.view.isVisible
import dev.holdmytrack.android.R

/**
 * The map's one notice, under the top row: a sentence, an optional smaller detail and at most one
 * action. When two apply the more important stays up ([MapNotice], [MapChromeRules.noticeReplaces]).
 */
class NoticePanel(
    /** The panel itself, which the compass also clears. */
    val view: View,
    /** Whether a recording has the map, and whether the Sync tab shows: the empty notice steps aside for both. */
    private val recording: () -> Boolean,
    private val onSync: () -> Boolean,
) {
    private val text: TextView = view.findViewById(R.id.map_notice_text)
    private val detail: TextView = view.findViewById(R.id.map_notice_detail)
    private val action: Button = view.findViewById(R.id.map_notice_action)

    /** Which notice is up, if any. */
    var shown: MapNotice? = null
        private set

    fun show(
        kind: MapNotice,
        text: String,
        detail: String? = null,
        failed: Boolean = false,
        action: Int? = null,
        onAction: (() -> Unit)? = null,
    ) {
        if (!MapChromeRules.noticeReplaces(shown, kind)) return
        shown = kind
        this.text.text = text
        this.text.setTextColor(view.context.getColor(if (failed) R.color.hmt_danger else R.color.hmt_ink))
        this.detail.text = detail
        this.detail.visibility = if (detail == null) View.GONE else View.VISIBLE
        this.action.visibility = if (action == null) View.GONE else View.VISIBLE
        if (action != null) this.action.setText(action)
        this.action.setOnClickListener { onAction?.invoke() }
        render()
    }

    /** Takes [kind] down if it's the one up; any other notice stays. */
    fun hide(kind: MapNotice) {
        if (shown != kind) return
        shown = null
        render()
    }

    fun render() {
        view.isVisible = MapChromeRules.noticeShown(shown, recording(), onSync())
    }
}
