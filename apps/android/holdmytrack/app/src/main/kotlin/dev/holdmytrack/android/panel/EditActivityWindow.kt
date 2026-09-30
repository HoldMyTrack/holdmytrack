package dev.holdmytrack.android.panel

import android.view.LayoutInflater
import android.view.View
import android.view.accessibility.AccessibilityNodeInfo
import android.view.inputmethod.InputMethodManager
import android.widget.Button
import android.widget.CheckBox
import android.widget.LinearLayout
import android.widget.TextView
import com.google.android.material.checkbox.MaterialCheckBox
import com.google.android.material.textfield.TextInputEditText
import com.google.android.material.textfield.TextInputLayout
import androidx.appcompat.widget.TooltipCompat
import dev.holdmytrack.android.R
import dev.holdmytrack.android.map.EditPreview
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Story
import dev.holdmytrack.android.net.TrackPoint
import dev.holdmytrack.android.recording.ActivityTypePicker
import dev.holdmytrack.android.recording.RecordingTypes
import dev.holdmytrack.android.recording.TypeCount

/**
 * The Edit window, after the web's `apps/web/src/ui/EditActivityWindow.tsx` (`docs/SPEC.md`
 * FR-5.10, FR-5.14; its Stories tab is Android's own now, `apps/android/docs/SPEC.md` FR-2.7
 * item 17): a card over the top of the map, opened from the Activities
 * toolbar's Edit over its target, with three tabs behind one Save and Cancel.
 *
 *  - **Activity**: one activity edits Type, Name and Description; several edit Type only —
 *    Name and Description have nothing to set consistently across different activities, so
 *    they're disabled with a note saying why rather than silently skipped.
 *  - **Track** ([TrackEditor]): one activity with a finished track, else disabled saying why.
 *    Opening it the first time starts the map's track session ([onStartTrack]: the other
 *    tracks away, the camera on this one), which lasts until the window closes.
 *  - **Stories**: the account's Stories as checkboxes for the window's activities, one or the
 *    group ([StoryChanges] has the rules), read when the window opens. The only place an
 *    activity goes into or out of an existing Story.
 *
 * Save writes what changed — the fields first (one `PATCH /v1/activities/{id}` each, one after
 * another, a group's resending each activity's own name and description), then each changed
 * Story (one request with every activity in the window), then the track edit — and closes; a
 * refusal keeps the window open with the server's reason, a retried Save skips what already
 * went, and a later step failing still reports the earlier ones saved. Cancel, or Back, writes
 * nothing.
 */
class EditActivityWindow(
    private val card: View,
    /** The Track tab opened on [Activity] for the first time. */
    private val onStartTrack: (Activity) -> Unit,
    /** The track editor's overlay — see [TrackEditor]. */
    onDrawTrack: (visible: List<TrackPoint>?, lo: Int, hi: Int, preview: EditPreview) -> Unit,
    /** Called once the window closes: whether anything was written, and whether that
     *  included a track edit, which leaves the activity Pending until its reprocess lands. */
    private val onClose: (saved: Boolean, trackApplied: Boolean) -> Unit,
) {
    private enum class Tab { ACTIVITY, TRACK, STORIES }
    private val context = card.context
    private val res = context.resources

    private val title: TextView = card.findViewById(R.id.edit_title)
    private val subtitle: TextView = card.findViewById(R.id.edit_subtitle)
    private val typeLayout: TextInputLayout = card.findViewById(R.id.edit_type_layout)
    private val typeField: TextInputEditText = card.findViewById(R.id.edit_type)
    private val nameLayout: TextInputLayout = card.findViewById(R.id.edit_name_layout)
    private val nameField: TextInputEditText = card.findViewById(R.id.edit_name)
    private val descriptionLayout: TextInputLayout = card.findViewById(R.id.edit_description_layout)
    private val descriptionField: TextInputEditText = card.findViewById(R.id.edit_description)
    private val multiNote: View = card.findViewById(R.id.edit_multi_note)
    private val error: TextView = card.findViewById(R.id.edit_error)
    private val cancel: Button = card.findViewById(R.id.edit_cancel)
    private val save: Button = card.findViewById(R.id.edit_save)
    private val tabActivity: TextView = card.findViewById(R.id.edit_tab_activity)
    private val tabTrack: TextView = card.findViewById(R.id.edit_tab_track)
    private val trackDot: View = card.findViewById(R.id.edit_tab_track_dot)
    private val tabStories: TextView = card.findViewById(R.id.edit_tab_stories)
    private val storiesDot: View = card.findViewById(R.id.edit_tab_stories_dot)
    private val activityPanel: View = card.findViewById(R.id.edit_activity_panel)
    private val trackPanel: View = card.findViewById(R.id.edit_track_panel)
    private val storiesPanel: View = card.findViewById(R.id.edit_stories_panel)
    private val storiesNote: TextView = card.findViewById(R.id.edit_stories_note)
    private val storiesRows: LinearLayout = card.findViewById(R.id.edit_stories_rows)

    /** The Track tab's editor — `MainActivity` hands it Delete point's map taps. */
    val trackEditor = TrackEditor(trackPanel, onDrawTrack) { renderTabs() }

    private var tab = Tab.ACTIVITY
    private var trackStarted = false

    /** Why the Track tab can't open, or null when it can. */
    private var trackUnavailable: String? = null
    private var activities: List<Activity> = emptyList()
    private var known: List<TypeCount> = emptyList()

    /** The raw `activity_type` the Type field holds — the field itself shows its label. Empty
     *  for a group of mixed types until one is picked: empty keeps each activity's own, so a
     *  Save made for the Stories tab alone never retypes anything. */
    private var activityType = ""

    /** The group's activities don't all share one type. */
    private var mixedTypes = false
    private var saving = false

    /** Set once the fields are written, so a Save retried after a failure doesn't write them
     *  twice, and a Cancel after one still reports that something changed. */
    private var fieldsSaved = false

    /** The account's Stories, null while they load; the boxes changed since the window opened;
     *  and the Stories a Save already wrote, the same way as [fieldsSaved]. */
    private var stories: List<Story>? = null
    private var storiesError: String? = null
    private var storyChanges = StoryChanges(emptyList())
    private val storiesSaved = HashSet<String>()

    /** Bumped by [open], so a Stories read for an earlier window is dropped. */
    private var generation = 0

    val isOpen: Boolean
        get() = card.visibility == View.VISIBLE

    init {
        typeField.setOnClickListener { openTypePicker() }
        typeLayout.setEndIconOnClickListener { openTypePicker() }
        cancel.setOnClickListener { close() }
        save.setOnClickListener { save() }
        tabActivity.setOnClickListener { showTab(Tab.ACTIVITY) }
        tabTrack.setOnClickListener { showTab(Tab.TRACK) }
        tabStories.setOnClickListener { showTab(Tab.STORIES) }
    }

    private fun showTab(next: Tab) {
        if (next == Tab.TRACK && trackUnavailable != null) return
        tab = next
        if (next == Tab.TRACK) {
            val single = activities.single()
            if (!trackStarted) {
                trackStarted = true
                onStartTrack(single)
            }
            trackEditor.open(single)
        } else {
            trackEditor.hide()
        }
        hideKeyboard()
        renderTabs()
    }

    /** The selected tab underlined, the other dimmed; Track dimmer still, saying why, when it
     *  can't apply — the web's disabled tab and its title. */
    private fun renderTabs() {
        for ((view, which) in listOf(tabActivity to Tab.ACTIVITY, tabTrack to Tab.TRACK, tabStories to Tab.STORIES)) {
            val selected = tab == which
            if (selected) view.setBackgroundResource(R.drawable.bg_panel_tab_selected) else view.background = null
            view.alpha = when {
                selected -> 1f
                which == Tab.TRACK && trackUnavailable != null -> DISABLED_TAB_ALPHA
                else -> UNSELECTED_TAB_ALPHA
            }
            view.isSelected = selected
        }
        tabTrack.isEnabled = trackUnavailable == null
        tabTrack.contentDescription = trackUnavailable?.let { res.getString(R.string.edit_tab_track) + ". " + it }
            ?: if (trackEditor.changed) res.getString(R.string.edit_track_unsaved) else null
        TooltipCompat.setTooltipText(tabTrack, trackUnavailable)
        trackDot.visibility = if (trackEditor.changed) View.VISIBLE else View.GONE
        val storiesChanged = storyChanges.changes.isNotEmpty()
        storiesDot.visibility = if (storiesChanged) View.VISIBLE else View.GONE
        tabStories.contentDescription = if (storiesChanged) res.getString(R.string.edit_stories_unsaved) else null
        activityPanel.visibility = if (tab == Tab.ACTIVITY) View.VISIBLE else View.GONE
        trackPanel.visibility = if (tab == Tab.TRACK) View.VISIBLE else View.GONE
        storiesPanel.visibility = if (tab == Tab.STORIES) View.VISIBLE else View.GONE
    }

    /** The Stories tab: the hint, or why there's nothing to tick, and a row per Story. */
    private fun renderStories() {
        val list = stories
        storiesNote.text = when {
            storiesError != null -> storiesError
            list == null -> res.getString(R.string.panel_loading)
            list.isEmpty() -> res.getString(R.string.edit_stories_empty)
            activities.size == 1 -> res.getString(R.string.edit_stories_hint_one)
            else -> res.getQuantityString(R.plurals.edit_stories_hint, activities.size, activities.size)
        }
        storiesNote.setTextColor(context.getColor(if (storiesError != null) R.color.hmt_danger else R.color.hmt_ink_hint))
        storiesRows.removeAllViews()
        val inflater = LayoutInflater.from(context)
        for (story in list.orEmpty()) {
            val row = inflater.inflate(R.layout.item_edit_story, storiesRows, false)
            val shown = storyChanges.shown(story)
            row.findViewById<MaterialCheckBox>(R.id.edit_story_check).apply {
                checkedState = when (shown) {
                    StoryMembership.ALL -> MaterialCheckBox.STATE_CHECKED
                    StoryMembership.SOME -> MaterialCheckBox.STATE_INDETERMINATE
                    StoryMembership.NONE -> MaterialCheckBox.STATE_UNCHECKED
                }
                isEnabled = !saving
            }
            row.findViewById<TextView>(R.id.edit_story_name).text = story.name
            val count = res.getQuantityString(R.plurals.story_activity_count, story.activityIds.size, story.activityIds.size)
            row.findViewById<TextView>(R.id.edit_story_count).text = count
            row.isEnabled = !saving
            row.contentDescription = "${story.name}, $count"
            // Checked and not checked are the node's own state (below); partly is said in words.
            row.stateDescription = if (shown == StoryMembership.SOME) res.getString(R.string.edit_stories_some) else null
            // Read as the checkbox it is: the row is the tap target, the box only shows it.
            row.accessibilityDelegate = object : View.AccessibilityDelegate() {
                override fun onInitializeAccessibilityNodeInfo(host: View, info: AccessibilityNodeInfo) {
                    super.onInitializeAccessibilityNodeInfo(host, info)
                    info.className = CheckBox::class.java.name
                    info.isCheckable = true
                    info.isChecked = shown == StoryMembership.ALL
                }
            }
            if (shown == StoryMembership.SOME) TooltipCompat.setTooltipText(row, res.getString(R.string.edit_stories_some))
            row.setOnClickListener {
                if (saving) return@setOnClickListener
                storyChanges.toggle(story)
                renderStories()
                renderTabs()
            }
            storiesRows.addView(row)
        }
    }

    private fun loadStories() {
        val gen = generation
        HoldMyTrackApi.stories { result ->
            if (gen != generation) return@stories
            result.onSuccess { stories = it }
                .onFailure { storiesError = it.message?.takeIf { m -> m.isNotBlank() } ?: res.getString(R.string.story_load_failed) }
            renderStories()
        }
    }

    /**
     * Opens over [group] — at least one. [knownTypes] is what the Type picker lists: the
     * range's types with how many of its activities use each, the same facets the Type filter
     * is built from, as on the web.
     */
    fun open(group: List<Activity>, knownTypes: List<TypeFacet>) {
        activities = group
        known = knownTypes.map { TypeCount(it.type, it.count) }
        val single = group.singleOrNull()
        fieldsSaved = false
        saving = false
        generation += 1
        stories = null
        storiesError = null
        storyChanges = StoryChanges(group.map { it.id })
        storiesSaved.clear()
        mixedTypes = group.any { it.activityType != group.first().activityType }
        activityType = if (mixedTypes) "" else group.first().activityType
        title.text = if (single != null) {
            res.getString(R.string.edit_title_one)
        } else {
            res.getQuantityString(R.plurals.edit_title_many, group.size, group.size)
        }
        subtitle.text = if (single != null) {
            single.name?.trim()?.takeIf { it.isNotEmpty() } ?: PanelFormat.startedAt(res, single.startedAt)
        } else {
            res.getString(R.string.edit_multi_subtitle)
        }
        typeField.setText(if (activityType.isEmpty()) "" else RecordingTypes.format(res, activityType))
        // "Mixed types" stands in the empty field, shown without focus, until a type is picked.
        typeLayout.isExpandedHintEnabled = !mixedTypes
        typeLayout.placeholderText = if (mixedTypes) res.getString(R.string.edit_type_mixed) else null
        nameField.setText(single?.name.orEmpty())
        descriptionField.setText(single?.description.orEmpty())
        nameLayout.isEnabled = single != null
        descriptionLayout.isEnabled = single != null
        multiNote.visibility = if (single == null) View.VISIBLE else View.GONE
        // The Track tab edits one activity's points, so it needs exactly one — with a
        // finished track.
        trackUnavailable = when {
            single == null -> res.getString(R.string.edit_track_check_one)
            single.pending -> res.getString(R.string.edit_track_processing)
            single.bbox == null -> res.getString(R.string.edit_track_no_track)
            else -> null
        }
        trackStarted = false
        tab = Tab.ACTIVITY
        showError(null)
        renderBusy()
        renderTabs()
        renderStories()
        card.visibility = View.VISIBLE
        loadStories()
    }

    /** Cancel: nothing more is written. */
    fun close() {
        if (saving) return
        finish(trackApplied = false)
    }

    private fun openTypePicker() {
        if (saving) return
        ActivityTypePicker.show(context, activityType, known) { picked ->
            activityType = picked
            typeField.setText(RecordingTypes.format(res, picked))
            showError(null)
        }
    }

    private fun save() {
        val single = activities.singleOrNull()
        val type = activityType.trim()
        val name = nameField.text?.toString().orEmpty().trim()
        val description = descriptionField.text?.toString().orEmpty()
        val invalid = when {
            type.isEmpty() && !mixedTypes -> res.getString(R.string.edit_type_required)
            type.length > MAX_TYPE -> res.getString(R.string.edit_type_too_long, MAX_TYPE)
            name.length > MAX_NAME -> res.getString(R.string.edit_name_too_long, MAX_NAME)
            description.length > MAX_DESCRIPTION -> res.getString(R.string.edit_description_too_long, MAX_DESCRIPTION)
            else -> null
        }
        if (invalid != null) {
            showError(invalid)
            showTab(Tab.ACTIVITY)
            return
        }
        val changed = if (single != null) {
            type != single.activityType || name != single.name.orEmpty() || description != single.description.orEmpty()
        } else {
            type.isNotEmpty() && activities.any { it.activityType != type }
        }
        if (!changed || fieldsSaved) {
            saveStories()
            return
        }
        saving = true
        showError(null)
        renderBusy()
        hideKeyboard()
        // One at a time, as the web does: a group is a handful of rows picked by hand.
        val writes = activities.map { activity ->
            if (single != null) {
                Triple(activity.id, name, description)
            } else {
                Triple(activity.id, activity.name.orEmpty(), activity.description.orEmpty())
            }
        }
        writeNext(writes, 0, type)
    }

    private fun writeNext(writes: List<Triple<String, String, String>>, index: Int, type: String) {
        if (index == writes.size) {
            fieldsSaved = true
            saveStories()
            return
        }
        val (id, name, description) = writes[index]
        HoldMyTrackApi.updateActivity(id, type, name, description) { result ->
            result.onSuccess { writeNext(writes, index + 1, type) }
                .onFailure { failure ->
                    saving = false
                    // A group that failed partway has written some rows; the list reload on
                    // close shows which.
                    if (index > 0) fieldsSaved = true
                    renderBusy()
                    showError(failure.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.edit_save_failed))
                }
        }
    }

    /** Each changed Story, one at a time, once the fields are in: a ticked box puts every one of
     *  the window's activities in it (any already there stay), a cleared one takes them all out.
     *  The activities themselves don't change. */
    private fun saveStories() {
        val next = storyChanges.changes.entries.firstOrNull { it.key !in storiesSaved }
        if (next == null) {
            saveTrack()
            return
        }
        saving = true
        showError(null)
        renderBusy()
        hideKeyboard()
        val add = next.value == StoryMembership.ALL
        HoldMyTrackApi.changeStoryActivities(next.key, activities.map { it.id }, add) { result ->
            result.onSuccess {
                storiesSaved += next.key
                saveStories()
            }.onFailure { failure ->
                saving = false
                renderBusy()
                showError(failure.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.edit_save_failed))
            }
        }
    }

    /** The track edit, once the fields are in — when the Track tab changed anything. */
    private fun saveTrack() {
        val single = activities.singleOrNull()
        if (single == null || !trackEditor.changed) {
            finish(trackApplied = false)
            return
        }
        saving = true
        renderBusy()
        HoldMyTrackApi.trackEdit(single.id, trackEditor.pending) { result ->
            result.onSuccess { finish(trackApplied = true) }
                .onFailure { failure ->
                    saving = false
                    renderBusy()
                    showError(failure.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.edit_save_failed))
                }
        }
    }

    private fun finish(trackApplied: Boolean) {
        saving = false
        hideKeyboard()
        trackEditor.close()
        card.visibility = View.GONE
        onClose(fieldsSaved || storiesSaved.isNotEmpty() || trackApplied, trackApplied)
    }

    private fun renderBusy() {
        save.isEnabled = !saving
        cancel.isEnabled = !saving
        save.setText(if (saving) R.string.edit_saving else R.string.edit_save)
        typeLayout.isEnabled = !saving
        nameField.isEnabled = !saving && activities.size == 1
        descriptionField.isEnabled = !saving && activities.size == 1
        trackEditor.busy = saving
        renderStories()
    }

    private fun showError(message: String?) {
        error.text = message
        error.visibility = if (message == null) View.GONE else View.VISIBLE
    }

    private fun hideKeyboard() {
        val imm = context.getSystemService(InputMethodManager::class.java)
        imm?.hideSoftInputFromWindow(card.windowToken, 0)
        card.findFocus()?.clearFocus()
    }

    private companion object {
        /** The server's own bounds (`activities.go`'s maxActivityTypeLen and friends). */
        const val MAX_TYPE = ActivityTypePicker.MAX_LENGTH
        const val MAX_NAME = 200
        const val MAX_DESCRIPTION = 2000
        const val UNSELECTED_TAB_ALPHA = 0.45f
        const val DISABLED_TAB_ALPHA = 0.3f
    }
}
