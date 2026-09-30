package dev.holdmytrack.android.panel

import android.annotation.SuppressLint
import android.util.TypedValue
import android.view.GestureDetector
import android.view.LayoutInflater
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.widget.ImageButton
import android.widget.ImageView
import android.widget.TableLayout
import android.widget.TableRow
import android.widget.TextView
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.widget.TooltipCompat
import androidx.recyclerview.widget.DiffUtil
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.ListAdapter
import androidx.recyclerview.widget.RecyclerView
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.ApiException
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.Story
import dev.holdmytrack.android.net.StoryTotals
import dev.holdmytrack.android.recording.RecordingTypes

/**
 * The Activities panel's Stories tab, the web's `apps/web/src/ui/StoriesTab.tsx` and the Story
 * half of `MapView.tsx` (`docs/SPEC.md` FR-14.6): every Story as a folder, newest first,
 * exactly one of them open while the tab shows — the newest, until another is tapped. Opening
 * one is viewing it: `MainActivity` shows the tracks and the list of all its activities,
 * whatever the date range ([onOpen]), and this tab lists those rows inside the folder — tap to
 * select, × to take one out of the Story — with the whole Story's statistics in the footer. A
 * folder's pencil and bin rename and delete it.
 *
 * The list is read each time the tab opens ([start]), so a Story made or changed since —
 * Add to story's included — is there; the open Story is read on its own ([setOpen]) for the
 * footer's numbers.
 */
class StoriesTab(
    private val content: View,
    /** Open [storyId] — tapped ([collapse]: the sheet goes down so its tracks can be seen), or
     *  the newest opening with the tab. */
    private val onOpen: (storyId: String, collapse: Boolean) -> Unit,
    /** Nothing left to open: the last Story was deleted. */
    private val onClose: () -> Unit,
    private val onSelect: (activityId: String) -> Unit,
    private val onClearFocus: () -> Unit,
    /** A Story renamed or deleted — the rows' Story badges name it. */
    private val onBadgesChanged: () -> Unit,
    /** [activityId] was taken out of the open Story: the list and the tracks are out of date. */
    private val onActivityRemoved: (activityId: String) -> Unit,
) {
    private val context = content.context
    private val res = context.resources
    private val list: RecyclerView = content.findViewById(R.id.stories_list)
    private val stats: View = content.findViewById(R.id.stories_stats)
    private val statsLine: TextView = content.findViewById(R.id.stories_stats_line)
    private val types: TableLayout = content.findViewById(R.id.stories_types)
    private val adapter = ItemAdapter()

    private var showing = false
    private var stories: List<Story> = emptyList()

    /** A list read since the tab last opened is in hand. */
    private var ready = false
    private var error: String? = null

    /** The open Story's id, and the Story as `GET /v1/stories/{id}` last answered. */
    var openId: String? = null
        private set
    private var openStory: Story? = null
    private var openError: String? = null

    /** The open Story's rows, as the panel last drew them. */
    private var rows: List<ActivityRowItem> = emptyList()
    private var rowsLoading = false
    private var rowsError: String? = null

    /** The last × that failed, with the server's reason — shown above the rows until the next
     *  one, or another Story opening. */
    private var removeError: String? = null

    init {
        list.layoutManager = LinearLayoutManager(context)
        list.adapter = adapter
        list.itemAnimator = null
        clearFocusOnEmptyTap()
    }

    /** The tab opened: the list read afresh, and the newest opened once it's in. */
    fun start() {
        showing = true
        ready = false
        error = null
        render()
        HoldMyTrackApi.stories { result ->
            if (!showing) return@stories
            result.onSuccess { next ->
                stories = next
                ready = true
                openNewest()
            }.onFailure { error = it.message?.takeIf { m -> m.isNotBlank() } ?: res.getString(R.string.story_load_failed) }
            render()
        }
    }

    /** The tab closed — `MainActivity` has closed its Story. */
    fun stop() {
        showing = false
    }

    /** [id] is the open Story now (or none): its own copy is read for the footer. */
    fun setOpen(id: String?) {
        if (id != openId) {
            openId = id
            openStory = null
            removeError = null
        }
        openError = null
        render()
        if (id != null) reloadOpen()
    }

    /** The open Story read again — after anything that changes its activities' numbers. A
     *  reload keeps the numbers shown until the new copy lands. */
    fun reloadOpen() {
        val id = openId ?: return
        HoldMyTrackApi.story(id) { result ->
            if (id != openId) return@story
            result.onSuccess { story ->
                openStory = story
                openError = null
                stories = stories.map { if (it.id == story.id) story else it }
            }.onFailure { failure ->
                openStory = null
                openError = if (failure is ApiException && failure.code == 404) {
                    res.getString(R.string.story_not_found)
                } else {
                    failure.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.story_load_failed)
                }
            }
            render()
        }
    }

    /** The open Story's rows, from the panel's own render. */
    fun setRows(next: List<ActivityRowItem>, loading: Boolean, error: String?) {
        rows = next
        rowsLoading = loading
        rowsError = error
        render()
    }

    /** Centres [activityId]'s row in the list — a track tapped on the map — the list alone,
     *  never the sheet around it. */
    fun scrollToRow(activityId: String) {
        list.post {
            val position = adapter.currentList.indexOfFirst { it is Item.Row && it.row.activity.id == activityId }
            if (position < 0) return@post
            val rowHeight = list.getChildAt(0)?.height ?: 0
            (list.layoutManager as LinearLayoutManager).scrollToPositionWithOffset(position, (list.height - rowHeight) / 2)
        }
    }

    /** One Story is always open on the tab: the newest, until another is picked. */
    private fun openNewest() {
        if (!showing || openId != null) return
        stories.firstOrNull()?.let { onOpen(it.id, false) }
    }

    private fun render() {
        val items = ArrayList<Item>()
        val openListed = stories.any { it.id == openId }
        if (openError != null && !openListed) items += Item.Note(openError!!, failed = true)
        for (listed in stories) {
            val open = listed.id == openId
            // The open Story's own copy is the fresher one.
            val story = if (open && openStory?.id == listed.id) openStory!! else listed
            items += Item.Folder(story, open)
            if (!open) continue
            if (story.description.isNotEmpty()) items += Item.Description(story.id, story.description)
            removeError?.let { items += Item.Note(it, failed = true) }
            rows.forEach { items += Item.Row(it, story.id) }
            when {
                rowsLoading -> items += Item.Note(res.getString(R.string.panel_loading), failed = false)
                rowsError != null -> items += Item.Note(rowsError!!, failed = true)
                rows.isEmpty() -> items += Item.Note(
                    res.getString(if (story.stats.count == 0) R.string.story_no_activities else R.string.panel_none_match),
                    failed = false,
                )
            }
        }
        when {
            error != null -> items += Item.Note(error!!, failed = true)
            !ready && stories.isEmpty() -> items += Item.Note(res.getString(R.string.panel_loading), failed = false)
            ready && stories.isEmpty() -> items += Item.Note(res.getString(R.string.story_empty), failed = false)
        }
        adapter.submitList(items)
        renderStats()
    }

    /** The whole Story, whatever the date range selects (FR-14.1). */
    private fun renderStats() {
        val story = openStory?.takeIf { it.id == openId }
        stats.visibility = if (story == null) View.GONE else View.VISIBLE
        types.removeAllViews()
        if (story == null) return
        if (story.stats.count == 0) {
            statsLine.setText(R.string.story_no_activities)
            return
        }
        statsLine.text = statsLine(story.stats)
        val gap = res.getDimensionPixelSize(R.dimen.hmt_space_12)
        for (type in story.byType) {
            val row = TableRow(context)
            row.addView(typeCell(RecordingTypes.format(res, type.activityType)).apply { setPadding(0, 0, gap, 0) })
            row.addView(typeCell(statsLine(type)))
            types.addView(row)
        }
    }

    private fun typeCell(text: String) = TextView(context).apply {
        this.text = text
        setTextColor(context.getColor(R.color.hmt_ink_secondary))
        setTextSize(TypedValue.COMPLEX_UNIT_PX, res.getDimension(R.dimen.hmt_text_sm))
    }

    /** "3 activities · 58 km · 3h 26m moving" — the Story's, or one type's share of it. */
    private fun statsLine(totals: StoryTotals): String = res.getString(
        R.string.story_stats,
        res.getQuantityString(R.plurals.story_activity_count, totals.count, totals.count),
        PanelFormat.totalDistance(res, totals.distanceMeters),
        PanelFormat.duration(res, totals.movingSeconds),
    )

    private fun confirmDelete(story: Story) {
        val dialog = MaterialAlertDialogBuilder(context, R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(R.string.story_delete_title)
            .setMessage(res.getString(R.string.story_delete_confirm, story.name))
            .setPositiveButton(R.string.story_delete_button, null)
            .setNegativeButton(android.R.string.cancel, null)
            .create()
        dialog.setOnShowListener {
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener { delete(dialog, story) }
        }
        dialog.show()
    }

    /** The dialog stays up while it runs, and keeps a failure on screen with a retry. */
    private fun delete(dialog: AlertDialog, story: Story) {
        val confirm = dialog.getButton(AlertDialog.BUTTON_POSITIVE)
        val cancel = dialog.getButton(AlertDialog.BUTTON_NEGATIVE)
        dialog.setCancelable(false)
        confirm.isEnabled = false
        cancel.isEnabled = false
        confirm.setText(R.string.panel_deleting)
        HoldMyTrackApi.deleteStory(story.id) { result ->
            result.onSuccess {
                dialog.dismiss()
                deleted(story.id)
            }.onFailure { failure ->
                dialog.setCancelable(true)
                confirm.isEnabled = true
                cancel.isEnabled = true
                confirm.setText(R.string.story_delete_button)
                dialog.setMessage(res.getString(R.string.story_delete_failed, failure.message.orEmpty()))
            }
        }
    }

    /** Dropped from the list in hand rather than read again. Deleting the open Story opens the
     *  next newest in its place; deleting the last leaves none open. */
    private fun deleted(id: String) {
        stories = stories.filter { it.id != id }
        onBadgesChanged()
        if (id != openId) {
            render()
            return
        }
        val next = stories.firstOrNull()
        if (next != null) onOpen(next.id, false) else onClose()
        render()
    }

    /** The row's ×: out of the Story at once, with no confirmation — the activity itself stays,
     *  and Add to story puts it back (`docs/SPEC.md` FR-14.6 item 3). */
    private fun remove(storyId: String, activityId: String) {
        HoldMyTrackApi.changeStoryActivities(storyId, listOf(activityId), add = false) { result ->
            result.onSuccess { story ->
                removeError = null
                stories = stories.map { if (it.id == story.id) story else it }
                if (story.id == openId) openStory = story
                onActivityRemoved(activityId)
            }.onFailure { failure ->
                removeError = failure.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.story_save_failed)
            }
            render()
        }
    }

    private fun edited(story: Story) {
        stories = stories.map { if (it.id == story.id) story else it }
        if (story.id == openId) openStory = story
        onBadgesChanged()
        render()
    }

    @SuppressLint("ClickableViewAccessibility") // A tap on no item at all; each item is its own target.
    private fun clearFocusOnEmptyTap() {
        val detector = GestureDetector(
            context,
            object : GestureDetector.SimpleOnGestureListener() {
                override fun onSingleTapUp(e: MotionEvent): Boolean {
                    if (list.findChildViewUnder(e.x, e.y) == null) onClearFocus()
                    return false
                }
            },
        )
        list.setOnTouchListener { _, event ->
            detector.onTouchEvent(event)
            false
        }
    }

    /** What the list draws, in order: folders, the open one's description and rows, notes. */
    private sealed class Item(val key: String) {
        data class Folder(val story: Story, val open: Boolean) : Item("folder:" + story.id)
        data class Description(val storyId: String, val text: String) : Item("description:$storyId")
        data class Row(val row: ActivityRowItem, val storyId: String) : Item("row:" + row.activity.id)
        data class Note(val text: String, val failed: Boolean) : Item("note:$text")
    }

    private inner class ItemAdapter : ListAdapter<Item, RecyclerView.ViewHolder>(
        object : DiffUtil.ItemCallback<Item>() {
            override fun areItemsTheSame(old: Item, new: Item) = old.key == new.key
            override fun areContentsTheSame(old: Item, new: Item) = old == new
        },
    ) {
        override fun getItemViewType(position: Int) = when (getItem(position)) {
            is Item.Folder -> TYPE_FOLDER
            is Item.Description -> TYPE_DESCRIPTION
            is Item.Row -> TYPE_ROW
            is Item.Note -> TYPE_NOTE
        }

        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): RecyclerView.ViewHolder {
            val inflater = LayoutInflater.from(parent.context)
            return when (viewType) {
                TYPE_FOLDER -> FolderHolder(inflater.inflate(R.layout.item_story_folder, parent, false))
                TYPE_DESCRIPTION -> object : RecyclerView.ViewHolder(inflater.inflate(R.layout.item_story_description, parent, false)) {}
                TYPE_ROW -> {
                    val view = inflater.inflate(R.layout.item_activity_row, parent, false)
                    // Set in under the folder's name, which is what makes them its contents.
                    val indent = (ROW_INDENT_DP * res.displayMetrics.density).toInt()
                    view.setPaddingRelative(indent, view.paddingTop, view.paddingEnd, view.paddingBottom)
                    ActivityRowHolder(view)
                }
                else -> object : RecyclerView.ViewHolder(inflater.inflate(R.layout.item_panel_note, parent, false)) {}
            }
        }

        override fun onBindViewHolder(holder: RecyclerView.ViewHolder, position: Int) {
            when (val item = getItem(position)) {
                is Item.Folder -> (holder as FolderHolder).bind(item)
                is Item.Description -> (holder.itemView as TextView).text = item.text
                // Badged only for the row's other Stories: every row here is in this one.
                is Item.Row -> (holder as ActivityRowHolder).bind(
                    item.row,
                    item.storyId,
                    onCheck = null,
                    onSelect = onSelect,
                    onOpenStory = { id -> onOpen(id, true) },
                    onRemove = { activityId -> remove(item.storyId, activityId) },
                )
                is Item.Note -> (holder.itemView as TextView).apply {
                    text = item.text
                    setTextColor(context.getColor(if (item.failed) R.color.hmt_danger else R.color.panel_ink_50))
                    setOnClickListener { onClearFocus() }
                }
            }
        }
    }

    private inner class FolderHolder(view: View) : RecyclerView.ViewHolder(view) {
        private val toggle: View = view.findViewById(R.id.story_toggle)
        private val chevron: ImageView = view.findViewById(R.id.story_chevron)
        private val name: TextView = view.findViewById(R.id.story_name)
        private val edit: ImageButton = view.findViewById(R.id.story_edit)
        private val delete: ImageButton = view.findViewById(R.id.story_delete)

        fun bind(item: Item.Folder) {
            val story = item.story
            name.text = story.name
            // The triangle points right while folded and down once open, in the accent.
            chevron.rotation = if (item.open) 90f else 0f
            chevron.setColorFilter(context.getColor(if (item.open) R.color.hmt_accent_strong else R.color.hmt_ink_meta))
            toggle.isSelected = item.open
            toggle.contentDescription = story.name
            toggle.stateDescription = res.getString(if (item.open) R.string.story_open else R.string.story_folded)
            // Tapping the open Story does nothing: there is no way to fold them all.
            toggle.setOnClickListener { if (!item.open) onOpen(story.id, true) }

            // The demo account sees both, disabled, saying why (FR-14.6 item 12), as it does
            // the rows' ×.
            val demo = Session.isDemo
            val editLabel = res.getString(if (demo) R.string.story_demo_edit else R.string.story_edit)
            val deleteLabel = res.getString(if (demo) R.string.story_demo_delete else R.string.story_delete)
            edit.isEnabled = !demo
            delete.isEnabled = !demo
            edit.contentDescription = "$editLabel: ${story.name}"
            delete.contentDescription = "$deleteLabel: ${story.name}"
            TooltipCompat.setTooltipText(edit, editLabel)
            TooltipCompat.setTooltipText(delete, deleteLabel)
            edit.setOnClickListener { StoryDialog.edit(context, story, ::edited) }
            delete.setOnClickListener { confirmDelete(story) }
        }
    }

    private companion object {
        const val TYPE_FOLDER = 0
        const val TYPE_DESCRIPTION = 1
        const val TYPE_ROW = 2
        const val TYPE_NOTE = 3

        /** The web's 36px `.stories-tab__rows` indent. */
        const val ROW_INDENT_DP = 36
    }
}
