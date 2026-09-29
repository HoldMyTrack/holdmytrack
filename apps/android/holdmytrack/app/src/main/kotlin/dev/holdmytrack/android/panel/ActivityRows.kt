package dev.holdmytrack.android.panel

import android.view.View
import android.widget.TextView
import androidx.appcompat.widget.TooltipCompat
import androidx.recyclerview.widget.RecyclerView
import com.google.android.material.checkbox.MaterialCheckBox
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.recording.RecordingTypes

/** One row as drawn: the activity and its marks, so a diff rebinds only what changed — a
 *  tick, the selection moving, a refetch — rather than every row, which also cancelled a tap
 *  on another row landing mid-rebind. */
data class ActivityRowItem(val activity: Activity, val checked: Boolean, val focused: Boolean, val hidden: Boolean)

/**
 * One activity row over `item_activity_row` — the web's `ActivityRow.tsx`, shared the same way
 * by the Activities tab and an open Story's rows on the Stories tab ([StoriesTab]). The title
 * and the "[date ·] distance · duration · type" line, dimmed with a Pending or Hidden badge,
 * and a Story badge for the Stories it's in — all but [openStoryId]'s, since every row under
 * an open Story is in that one (`docs/SPEC.md` FR-5.1). [onCheck] null leaves the checkbox out.
 */
class ActivityRowHolder(view: View) : RecyclerView.ViewHolder(view) {
    private val res = view.resources
    private val check: MaterialCheckBox = view.findViewById(R.id.activity_check)
    private val text: View = view.findViewById(R.id.activity_text)
    private val title: TextView = view.findViewById(R.id.activity_title)
    private val meta: TextView = view.findViewById(R.id.activity_meta)
    private val badges: View = view.findViewById(R.id.activity_badges)
    private val pending: View = view.findViewById(R.id.activity_pending)
    private val hidden: View = view.findViewById(R.id.activity_hidden)
    private val story: TextView = view.findViewById(R.id.activity_story)

    fun bind(row: ActivityRowItem, openStoryId: String?, onCheck: ((String) -> Unit)?, onSelect: (String) -> Unit) {
        val activity = row.activity
        val label = PanelFormat.rowLabel(res, activity)
        val named = activity.name?.trim()?.isNotEmpty() == true
        val isChecked = row.checked
        val isHidden = row.hidden

        itemView.isSelected = row.focused
        title.text = label
        // The date moves down here once a name has taken the title.
        meta.text = buildString {
            if (named) append(PanelFormat.startedAt(res, activity.startedAt)).append(" · ")
            append(PanelFormat.distance(res, activity.distanceMeters)).append(" · ")
            append(PanelFormat.duration(res, activity.durationSeconds)).append(" · ")
            append(RecordingTypes.format(res, activity.activityType))
        }
        val dim = when {
            activity.pending -> PENDING_ALPHA
            isHidden -> HIDDEN_ALPHA
            else -> 1f
        }
        title.alpha = dim
        meta.alpha = dim
        val others = activity.stories.filter { it.id != openStoryId }
        badges.visibility = if (activity.pending || isHidden || others.isNotEmpty()) View.VISIBLE else View.GONE
        pending.visibility = if (activity.pending) View.VISIBLE else View.GONE
        hidden.visibility = if (isHidden) View.VISIBLE else View.GONE
        // Only a label: a tap on it does what a tap on the row's text does.
        val names = others.joinToString(", ") { it.name }
        val storiesNote = when {
            others.isEmpty() -> null
            others.size == 1 -> res.getString(if (openStoryId != null) R.string.panel_in_other_story else R.string.panel_in_story, names)
            else -> res.getQuantityString(
                if (openStoryId != null) R.plurals.panel_in_other_stories else R.plurals.panel_in_stories,
                others.size,
                others.size,
                names,
            )
        }
        story.visibility = if (others.isEmpty()) View.GONE else View.VISIBLE
        story.text = if (others.size == 1) {
            res.getString(R.string.panel_story_badge_one)
        } else {
            res.getQuantityString(R.plurals.panel_story_badge, others.size, others.size)
        }
        TooltipCompat.setTooltipText(story, storiesNote)

        check.visibility = if (onCheck == null) View.GONE else View.VISIBLE
        check.setOnCheckedChangeListener(null)
        check.isChecked = isChecked
        // Pending is disabled until its reprocess lands, except that a checked one can still
        // be unchecked.
        check.isEnabled = !activity.pending || isChecked
        check.contentDescription = res.getString(if (isChecked) R.string.panel_row_uncheck else R.string.panel_row_check, label)
        if (onCheck != null) check.setOnCheckedChangeListener { _, _ -> onCheck(activity.id) }

        text.isEnabled = !activity.pending
        text.contentDescription = listOfNotNull(res.getString(R.string.panel_fly_to, label), meta.text, storiesNote)
            .joinToString(". ")
        text.setOnClickListener { onSelect(activity.id) }
    }

    private companion object {
        const val PENDING_ALPHA = 0.45f
        const val HIDDEN_ALPHA = 0.55f
    }
}
