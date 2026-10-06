package dev.holdmytrack.android.panel

import android.view.View
import android.widget.ImageView
import androidx.annotation.DrawableRes
import android.widget.PopupMenu
import android.widget.TextView
import androidx.appcompat.widget.TooltipCompat
import androidx.core.content.ContextCompat
import androidx.recyclerview.widget.RecyclerView
import android.content.res.ColorStateList
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.RecordingTypes

/** One row as drawn: the activity and its marks, so a diff rebinds only what changed — a
 *  tick, the selection moving, a refetch — rather than every row, which also cancelled a tap
 *  on another row landing mid-rebind. */
data class ActivityRowItem(
    val activity: Activity,
    val checked: Boolean,
    val focused: Boolean,
    val hidden: Boolean,
    /** Whether the list is selecting — a row's tap then checks or unchecks it. */
    val selecting: Boolean = false,
)

/** The icon for an activity's kind, on its row's tile and its card. */
@DrawableRes
fun kindIcon(kind: ActivityKind): Int = when (kind) {
    ActivityKind.FOOT -> R.drawable.ic_footprints
    ActivityKind.WHEELS -> R.drawable.ic_bike
    ActivityKind.MOTOR -> R.drawable.ic_car
    ActivityKind.OTHER -> R.drawable.ic_route
}

/**
 * One activity row over `item_activity_row` — the web's `ActivityRow.tsx`, shared the same way
 * by the Activities tab and an open Story's rows on the Stories tab ([StoriesTab]). The title
 * and the "[date ·] distance · duration · type" line, dimmed with a Pending or Hidden badge,
 * and a Story badge for the Stories it's in — all but [openStoryId]'s, since every row under
 * an open Story is in that one (`docs/SPEC.md` FR-5.1). The badge opens its Story, or, for
 * several, a menu of their names. The tile of its kind is its checkbox, as Gmail's avatar is: a
 * tap on it, or a long press on the row, is [onCheck], and a checked row's tile is a ✓ on the
 * accent; [onCheck] null leaves it a plain tile. [onRemove] shows the × that takes the row out
 * of the open Story (`docs/SPEC.md` FR-14.6 item 3).
 */
class ActivityRowHolder(view: View) : RecyclerView.ViewHolder(view) {
    private val res = view.resources
    private val tileTarget: View = view.findViewById(R.id.activity_tile_target)
    private val kindTile: ImageView = view.findViewById(R.id.activity_type_icon)
    private val text: View = view.findViewById(R.id.activity_text)
    private val title: TextView = view.findViewById(R.id.activity_title)
    private val meta: TextView = view.findViewById(R.id.activity_meta)
    private val badges: View = view.findViewById(R.id.activity_badges)
    private val pending: View = view.findViewById(R.id.activity_pending)
    private val hidden: View = view.findViewById(R.id.activity_hidden)
    private val privateBadge: TextView = view.findViewById<TextView>(R.id.activity_private).apply {
        // The web's lock before the word, at the badge's own text size and color.
        val size = textSize.toInt()
        val lock = ContextCompat.getDrawable(context, R.drawable.ic_lock)?.mutate()?.apply {
            setBounds(0, 0, size, size)
            setTint(currentTextColor)
        }
        setCompoundDrawablesRelative(lock, null, null, null)
    }
    private val story: TextView = view.findViewById(R.id.activity_story)
    private val remove: View = view.findViewById(R.id.activity_remove)

    fun bind(
        row: ActivityRowItem,
        openStoryId: String?,
        onCheck: ((String) -> Unit)?,
        onSelect: (String) -> Unit,
        onOpenStory: (String) -> Unit,
        onRemove: ((String) -> Unit)? = null,
    ) {
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
            // A private activity has no track, so no distance or duration: "0.0 km · 0m" would
            // read as broken data. Its badge says why instead.
            if (!activity.isPrivate) {
                append(PanelFormat.distance(res, activity.distanceMeters)).append(" · ")
                append(PanelFormat.duration(res, activity.durationSeconds)).append(" · ")
            }
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
        val isPrivate = activity.isPrivate && !activity.pending
        badges.visibility = if (activity.pending || isHidden || isPrivate || others.isNotEmpty()) View.VISIBLE else View.GONE
        pending.visibility = if (activity.pending) View.VISIBLE else View.GONE
        privateBadge.visibility = if (isPrivate) View.VISIBLE else View.GONE
        TooltipCompat.setTooltipText(privateBadge, res.getString(R.string.activity_private_note))
        hidden.visibility = if (isHidden) View.VISIBLE else View.GONE
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
        val storyLabel = if (others.size == 1) res.getString(R.string.panel_open_story, others[0].name) else storiesNote
        story.contentDescription = storyLabel
        TooltipCompat.setTooltipText(story, storyLabel)
        story.setOnClickListener {
            if (others.size == 1) {
                onOpenStory(others[0].id)
                return@setOnClickListener
            }
            val menu = PopupMenu(story.context, story)
            others.forEachIndexed { i, ref -> menu.menu.add(0, i, i, ref.name) }
            menu.setOnMenuItemClickListener { item ->
                onOpenStory(others[item.itemId].id)
                true
            }
            menu.show()
        }

        remove.visibility = if (onRemove == null) View.GONE else View.VISIBLE
        if (onRemove != null) {
            val demo = Session.isDemo
            remove.isEnabled = !demo
            val removeLabel = res.getString(if (demo) R.string.story_demo_remove else R.string.story_remove)
            remove.contentDescription = "$removeLabel: $label"
            TooltipCompat.setTooltipText(remove, removeLabel)
            remove.setOnClickListener { onRemove(activity.id) }
        }

        val context = itemView.context
        val ticked = isChecked && onCheck != null
        kindTile.setImageResource(if (ticked) R.drawable.ic_check else kindIcon(ActivityKind.of(activity.activityType)))
        kindTile.backgroundTintList = if (ticked) ColorStateList.valueOf(context.getColor(R.color.hmt_accent)) else null
        kindTile.imageTintList = ColorStateList.valueOf(context.getColor(if (ticked) R.color.hmt_on_accent else R.color.hmt_ink_secondary))
        val checkLabel = res.getString(if (isChecked) R.string.panel_row_uncheck else R.string.panel_row_check, label)
        // Pending can't be checked until its reprocess lands, though a checked one can still be
        // unchecked.
        val checkable = onCheck != null && (!activity.pending || isChecked)
        tileTarget.isClickable = checkable
        tileTarget.isEnabled = checkable
        tileTarget.importantForAccessibility =
            if (onCheck == null) View.IMPORTANT_FOR_ACCESSIBILITY_NO else View.IMPORTANT_FOR_ACCESSIBILITY_YES
        tileTarget.contentDescription = if (onCheck == null) null else checkLabel
        tileTarget.setOnClickListener(if (checkable) View.OnClickListener { onCheck?.invoke(activity.id) } else null)

        text.isEnabled = !activity.pending
        val action = if (row.selecting) checkLabel else res.getString(R.string.panel_fly_to, label)
        text.contentDescription = listOfNotNull(action, meta.text, storiesNote).joinToString(". ")
        text.setOnClickListener { onSelect(activity.id) }
        if (onCheck != null) {
            text.setOnLongClickListener {
                onCheck(activity.id)
                true
            }
        } else {
            text.setOnLongClickListener(null)
            text.isLongClickable = false
        }
    }

    private companion object {
        const val PENDING_ALPHA = 0.45f
        const val HIDDEN_ALPHA = 0.55f
    }
}
