package dev.holdmytrack.android.panel

import android.animation.ValueAnimator
import android.annotation.SuppressLint
import android.view.GestureDetector
import android.view.LayoutInflater
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.widget.ImageButton
import android.widget.LinearLayout
import android.widget.PopupMenu
import android.widget.PopupWindow
import android.widget.ScrollView
import android.widget.TextView
import androidx.appcompat.widget.TooltipCompat
import androidx.recyclerview.widget.ConcatAdapter
import androidx.recyclerview.widget.DiffUtil
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.ListAdapter
import androidx.recyclerview.widget.RecyclerView
import androidx.appcompat.app.AlertDialog
import com.google.android.material.checkbox.MaterialCheckBox
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.slider.RangeSlider
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.Story
import dev.holdmytrack.android.recording.RecordingTypes
import kotlin.math.abs

/** The panel's three tabs, the web's `PanelTab`. */
enum class PanelTab { ACTIVITIES, STORIES, PRIVACY }

/**
 * The map's Activities panel, as the web draws it at phone width
 * (`apps/web/src/ui/ActivitiesPanel.tsx` and index.css's phone layer): a bottom sheet over the
 * date-range footer, collapsed to its tab row, expanded to most of the screen by the chevron at
 * the row's end or a tab. The Activities tab
 * holds the Type dropdown and the DISTANCE slider, a toolbar over the toolbar's target, the
 * rows and the target's summary; the Stories tab, the account's Stories with one open on the
 * map ([StoriesTab]); the Privacy tab, the Private locations ([PrivacyTab], which
 * `MainActivity` owns, since it works on the map).
 *
 * The rules live in [PanelState]; this draws it and turns taps into state changes.
 * `MainActivity` owns the map and the fetch: it hands over each list ([setActivities]) and
 * hears about every change that touches the map through [onMapChanged] (what to hide, what's
 * selected) and [onFly] (the activities to frame); Edit is [onEdit]'s to open, and a finished
 * Delete is reported through [onDeleted]. Which Story is open is `MainActivity`'s too
 * ([onOpenStory], [onCloseStory]): it narrows the map and the list, which this then draws.
 */
class ActivitiesPanel(
    private val sheet: View,
    private val state: PanelState,
    /** The sheet's height when expanded — the web's 78% of the screen, less the footer under
     *  it — asked each time, since the screen can rotate. */
    private val expandedHeight: () -> Int,
    private val onMapChanged: () -> Unit,
    private val onFly: (List<Activity>) -> Unit,
    private val onEdit: (List<Activity>) -> Unit,
    /** Every id the toolbar's Delete removed — the map, the list and the footer need
     *  fetching again. */
    private val onDeleted: (List<String>) -> Unit,
    /** The tab changed — the Privacy tab has the map to itself while it shows, and leaving the
     *  Stories tab closes its Story. */
    private val onTabChanged: (PanelTab) -> Unit,
    /** The Stories tab opened a Story (`docs/SPEC.md` FR-14.6). */
    private val onOpenStory: (storyId: String) -> Unit,
    /** The Stories tab has no Story left to open. */
    private val onCloseStory: () -> Unit,
    /** Add to story's New story… made one of the toolbar's target (`docs/SPEC.md` FR-5.16). */
    private val onStoryCreated: (Story) -> Unit,
    /** A Story renamed, deleted or added to: the rows' Story badges are out of date. */
    private val onStoriesChanged: () -> Unit,
    /** An activity was taken out of the open Story on the Stories tab. */
    private val onRemovedFromStory: (activityId: String) -> Unit,
) {
    private val context = sheet.context
    private val res = context.resources

    private val count: TextView = sheet.findViewById(R.id.panel_count)
    private val tabActivities: View = sheet.findViewById(R.id.panel_tab_activities)
    private val tabStories: View = sheet.findViewById(R.id.panel_tab_stories)
    private val storiesContent: View = sheet.findViewById(R.id.panel_stories_content)
    private val activitiesContent: View = sheet.findViewById(R.id.panel_activities_content)
    private val tabPrivacy: View = sheet.findViewById(R.id.panel_tab_privacy)
    private val privacyContent: View = sheet.findViewById(R.id.panel_privacy_content)
    private val head: View = sheet.findViewById(R.id.panel_head)
    private val toggle: ImageButton = sheet.findViewById(R.id.panel_toggle)
    private val subtext: TextView = sheet.findViewById(R.id.panel_subtext)
    private val typeTrigger: View = sheet.findViewById(R.id.panel_type_trigger)
    private val typeDot: View = sheet.findViewById(R.id.panel_type_dot)
    private val distance: View = sheet.findViewById(R.id.panel_distance)
    private val distanceReadout: TextView = sheet.findViewById(R.id.panel_distance_readout)
    private val distanceSlider: RangeSlider = sheet.findViewById(R.id.panel_distance_slider)
    private val distanceMin: TextView = sheet.findViewById(R.id.panel_distance_min)
    private val distanceMax: TextView = sheet.findViewById(R.id.panel_distance_max)
    private val resetFilters: View = sheet.findViewById(R.id.panel_reset_filters)
    private val checkAll: MaterialCheckBox = sheet.findViewById(R.id.panel_check_all)
    private val selectMenu: ImageButton = sheet.findViewById(R.id.panel_select_menu)
    private val visibility: ImageButton = sheet.findViewById(R.id.panel_visibility)
    private val edit: ImageButton = sheet.findViewById(R.id.panel_edit)
    private val addToStory: ImageButton = sheet.findViewById(R.id.panel_add_to_story)
    private val delete: ImageButton = sheet.findViewById(R.id.panel_delete)
    private val focus: ImageButton = sheet.findViewById(R.id.panel_focus)
    private val list: RecyclerView = sheet.findViewById(R.id.panel_list)
    private val footer: TextView = sheet.findViewById(R.id.panel_footer)

    private val adapter = RowAdapter()
    var tab = PanelTab.ACTIVITIES
        private set

    private val noteAdapter = NoteAdapter()

    /** The Stories tab — its rows are this panel's list, narrowed to the open Story. */
    val storiesTab = StoriesTab(
        storiesContent,
        onOpen = { id, collapse ->
            // The expanded sheet would cover the Story's tracks.
            if (collapse) setExpanded(false)
            onOpenStory(id)
        },
        onClose = { onCloseStory() },
        onSelect = ::select,
        onClearFocus = ::clearFocus,
        onBadgesChanged = { onStoriesChanged() },
        onActivityRemoved = { id -> onRemovedFromStory(id) },
    )

    private var loading = false
    private var error: String? = null

    var expanded = false
        private set

    /** Forced down to the peek strip from outside without forgetting [expanded] — a recording
     *  or an edit window over the map; the sheet comes back as it was. */
    private var held = false
    private var heightAnimator: ValueAnimator? = null

    /** Where [heightAnimator] is taking the sheet. */
    private var heightTarget = 0
    private var typePopup: PopupWindow? = null

    /** Add to story's menu while it's open, and the target it was opened over — a different
     *  target closes it, as on the web. */
    private var storyPopup: PopupWindow? = null
    private var storyPopupTarget: List<String> = emptyList()

    init {
        list.layoutManager = LinearLayoutManager(context)
        // The note — loading, nothing matching, the read failing — is the list's last item,
        // after whatever rows there are, as the web's is.
        list.adapter = ConcatAdapter(adapter, noteAdapter)
        list.itemAnimator = null
        clearFocusOnEmptyTap()

        toggle.setOnClickListener { setExpanded(!expanded) }
        tabActivities.setOnClickListener { selectTab(PanelTab.ACTIVITIES) }
        tabStories.setOnClickListener { selectTab(PanelTab.STORIES) }
        tabPrivacy.setOnClickListener { selectTab(PanelTab.PRIVACY) }
        // The collapsed height is the tab row's bottom edge, whatever the font scale makes of it —
        // applied when that height changes, and after the layout pass rather than inside it: a
        // new height set mid-layout isn't laid out until something else asks for a layout, which
        // left an expand started by the chevron (whose own icon change re-lays out the row) at
        // the collapsed height until the next tap.
        head.addOnLayoutChangeListener { _, left, top, right, bottom, oldLeft, oldTop, oldRight, oldBottom ->
            if (bottom - top != oldBottom - oldTop || right - left != oldRight - oldLeft) head.post { applyHeight(animate = false) }
        }

        typeTrigger.setOnClickListener { showTypeFilter() }
        distanceSlider.addOnChangeListener { slider, _, fromUser ->
            if (!fromUser) return@addOnChangeListener
            val bounds = ActivityFacets.distanceBounds(state.activities) ?: return@addOnChangeListener
            // The slider holds Floats, which can't carry every distance exactly: a knob at an
            // end is snapped back onto that end's own activity, or the band would leave it out.
            fun snap(value: Float): Double = when {
                abs(value - bounds.min) < SNAP_METERS -> bounds.min
                abs(value - bounds.max) < SNAP_METERS -> bounds.max
                else -> value.toDouble()
            }
            state.setDistanceFilter(DistanceRange(snap(slider.values[0]), snap(slider.values[1])))
            changed()
        }
        resetFilters.setOnClickListener {
            state.resetFilters()
            changed()
        }

        checkAll.setOnClickListener {
            val listed = state.listed
            val checked = listed.count { it.id in state.checked }
            if (checked > 0) state.clearChecked() else state.checkAll()
            changed()
        }
        selectMenu.setOnClickListener { showSelectMenu() }
        TooltipCompat.setTooltipText(selectMenu, res.getString(R.string.panel_select_menu))
        visibility.setOnClickListener {
            state.toggleTargetVisibility()
            changed()
        }
        focus.setOnClickListener {
            val hidden = state.mapHidden
            onFly(state.targets.filter { it.id !in hidden })
        }
        edit.setOnClickListener { state.targets.takeIf { it.isNotEmpty() }?.let(onEdit) }
        addToStory.setOnClickListener { if (storyPopup == null) showAddToStory() else storyPopup?.dismiss() }
        delete.setOnClickListener { confirmDelete() }
        render()
    }

    /** A tab tapped: the sheet opens onto it, as the web's phone sheet does — collapsed, the
     *  tab row is all there is of it. */
    private fun selectTab(next: PanelTab) {
        showTab(next)
        if (!expanded) setExpanded(true)
    }

    private fun showTab(next: PanelTab) {
        if (tab == next) return
        tab = next
        dismissPopups()
        if (next == PanelTab.STORIES) storiesTab.start() else storiesTab.stop()
        render()
        onTabChanged(next)
    }

    /** Onto the Stories tab from outside — a Story Create story just made, which
     *  `MainActivity` opens straight away. */
    fun showStories() = showTab(PanelTab.STORIES)

    /** The Activities tab, collapsed — the Sync screen's View on map, which is about the map. */
    fun showActivities() {
        showTab(PanelTab.ACTIVITIES)
        setExpanded(false)
    }

    /** A new list is on its way — for a new range, or the same one again. */
    fun setLoading() {
        loading = true
        error = null
        render()
    }

    fun setActivities(activities: List<Activity>) {
        loading = false
        error = null
        state.setActivities(activities)
        changed()
    }

    fun setError(message: String) {
        loading = false
        error = message
        render()
    }

    /** A track tapped on the map: selects it, as a row tap does, and scrolls its row to the
     *  middle of the list — without expanding a collapsed sheet (`docs/SPEC.md` §19). */
    fun focusFromMap(id: String) {
        select(id)
        if (tab == PanelTab.STORIES) storiesTab.scrollToRow(id) else scrollToFocused()
    }

    /** A tap on empty map, or on the list's empty space: the selection goes, the group stays. */
    fun clearFocus() {
        if (state.focused == null) return
        state.clearFocus()
        changed()
    }

    fun setExpanded(next: Boolean) {
        expanded = next
        applyHeight(animate = true)
        render()
    }

    /** See [held]. */
    fun hold(down: Boolean) {
        if (held == down) return
        held = down
        applyHeight(animate = true)
        render()
    }

    fun dismissPopups() {
        typePopup?.dismiss()
        storyPopup?.dismiss()
    }

    /** The collapsed sheet's height: the tab row. */
    val peekHeight: Int
        get() = head.bottom

    private fun select(id: String) {
        state.focus(id)
        val hidden = state.mapHidden
        onFly(state.activities.filter { it.id == id && it.id !in hidden })
        changed()
    }

    private fun changed() {
        onMapChanged()
        render()
    }

    private fun applyHeight(animate: Boolean) {
        val peek = peekHeight
        if (peek == 0) return
        val target = if (expanded && !held) maxOf(expandedHeight(), peek) else peek
        val params = sheet.layoutParams
        if (params.height == target) return
        // Already on its way there: let it arrive rather than jump.
        if (heightAnimator?.isRunning == true && heightTarget == target) return
        heightAnimator?.cancel()
        heightTarget = target
        if (!animate || params.height <= 0) {
            params.height = target
            sheet.layoutParams = params
            return
        }
        heightAnimator = ValueAnimator.ofInt(params.height, target).apply {
            duration = SHEET_ANIMATION_MS
            addUpdateListener {
                params.height = it.animatedValue as Int
                sheet.layoutParams = params
            }
            start()
        }
    }

    private fun render() {
        val listed = state.listed
        // The Activities tab's own count: an open Story's rows are the list meanwhile, and
        // aren't what the badge counts, as on the web.
        if (tab != PanelTab.STORIES) count.text = PanelFormat.count(res, listed.size)
        renderTabs()
        subtext.visibility = if (tab == PanelTab.ACTIVITIES) View.GONE else View.VISIBLE
        subtext.setText(if (tab == PanelTab.PRIVACY) R.string.private_subtitle else R.string.story_subtext)
        // Held down under an edit, the sheet is collapsed whatever it was.
        val open = expanded && !held
        toggle.setImageResource(if (open) R.drawable.ic_chevron_down else R.drawable.ic_chevron_up)
        toggle.contentDescription = res.getString(if (open) R.string.panel_collapse else R.string.panel_expand)
        TooltipCompat.setTooltipText(toggle, toggle.contentDescription)

        typeDot.visibility = if (state.excludedTypes.isNotEmpty()) View.VISIBLE else View.GONE
        renderDistance()
        resetFilters.visibility = if (state.hasActiveFilters) View.VISIBLE else View.GONE
        renderToolbar(listed)

        val rows = listed.map { ActivityRowItem(it, checked = it.id in state.checked, focused = it.id == state.focused, hidden = it.id in state.hidden) }
        adapter.submitList(rows)
        if (tab == PanelTab.STORIES) storiesTab.setRows(rows, loading, error)
        noteAdapter.note = when {
            loading -> Note(res.getString(R.string.panel_loading), failed = false)
            error != null -> Note(error!!, failed = true)
            listed.isEmpty() -> Note(res.getString(R.string.panel_none_match), failed = false)
            else -> null
        }

        val targets = state.targets
        footer.text = res.getString(
            R.string.panel_footer,
            targets.size,
            PanelFormat.totalDistance(res, targets.sumOf { it.distanceMeters ?: 0.0 }),
        )
        typePopup?.let { renderTypeRows(it.contentView) }
        if (storyPopup != null && targets.map { it.id } != storyPopupTarget) storyPopup?.dismiss()
    }

    /** The selected tab at full strength over the accent underline, the other at 45% — the
     *  web's `.activities-panel__tab` — and its content in the sheet. */
    private fun renderTabs() {
        val tabs = listOf(tabActivities to PanelTab.ACTIVITIES, tabStories to PanelTab.STORIES, tabPrivacy to PanelTab.PRIVACY)
        for ((view, which) in tabs) {
            val selected = tab == which
            view.alpha = if (selected) 1f else UNSELECTED_TAB_ALPHA
            if (selected) view.setBackgroundResource(R.drawable.bg_panel_tab_selected) else view.background = null
            view.isSelected = selected
            view.contentDescription = null
        }
        activitiesContent.visibility = if (tab == PanelTab.ACTIVITIES) View.VISIBLE else View.GONE
        storiesContent.visibility = if (tab == PanelTab.STORIES) View.VISIBLE else View.GONE
        privacyContent.visibility = if (tab == PanelTab.PRIVACY) View.VISIBLE else View.GONE
    }

    /** DISTANCE, the web's `DistanceFilter`: gone while the list has no spread of distances. */
    private fun renderDistance() {
        val bounds = ActivityFacets.distanceBounds(state.activities)
        if (bounds == null || bounds.min >= bounds.max) {
            distance.visibility = View.GONE
            return
        }
        distance.visibility = View.VISIBLE
        val current = state.distanceFilter ?: bounds
        val lo = current.min.coerceIn(bounds.min, bounds.max).toFloat()
        val hi = current.max.coerceIn(bounds.min, bounds.max).toFloat()
        distanceSlider.valueFrom = bounds.min.toFloat()
        distanceSlider.valueTo = bounds.max.toFloat()
        if (distanceSlider.values != listOf(lo, hi)) distanceSlider.setValues(lo, hi)
        distanceReadout.text = if (state.distanceFilter == null) {
            res.getString(R.string.panel_any_distance)
        } else {
            res.getString(
                R.string.panel_distance_range,
                PanelFormat.distanceValue(res, current.min),
                PanelFormat.distanceValue(res, current.max),
                PanelFormat.unit(res),
            )
        }
        distanceMin.text = PanelFormat.distance(res, bounds.min)
        distanceMax.text = PanelFormat.distance(res, bounds.max)
    }

    private fun renderToolbar(listed: List<Activity>) {
        val checkedCount = listed.count { it.id in state.checked }
        val allChecked = listed.isNotEmpty() && checkedCount == listed.size
        checkAll.isEnabled = listed.isNotEmpty()
        checkAll.checkedState = when {
            allChecked -> MaterialCheckBox.STATE_CHECKED
            checkedCount > 0 -> MaterialCheckBox.STATE_INDETERMINATE
            else -> MaterialCheckBox.STATE_UNCHECKED
        }
        checkAll.contentDescription = res.getString(if (allChecked) R.string.panel_uncheck_all else R.string.panel_check_all)
        selectMenu.isEnabled = listed.isNotEmpty()

        // The toolbar names its target, so which of the group and the selected row it acts on
        // is never a guess: "3 checked activities", or the selected row's own label.
        val targets = state.targets
        val targetName = when {
            targets.isEmpty() -> null
            state.checked.isNotEmpty() -> res.getQuantityString(R.plurals.panel_checked_count, targets.size, targets.size)
            else -> res.getString(R.string.panel_target_one, PanelFormat.rowLabel(res, targets.first()))
        }
        val noTarget = res.getString(R.string.panel_no_target)
        val anyHidden = targets.any { it.id in state.hidden }
        visibility.setImageResource(if (anyHidden) R.drawable.ic_eye_off else R.drawable.ic_eye)
        describe(
            visibility,
            targetName?.let { res.getString(if (anyHidden) R.string.panel_show_target else R.string.panel_hide_target, it) } ?: noTarget,
            enabled = targetName != null,
        )
        // The demo account can look but not change (the server's requireNotDemo): both stay
        // visible, disabled, saying why — as the web's do.
        val demo = Session.isDemo
        describe(
            edit,
            when {
                demo -> res.getString(R.string.panel_demo_edit)
                targetName == null -> noTarget
                targets.size == 1 -> res.getString(R.string.panel_edit_one, targetName)
                else -> res.getString(R.string.panel_edit_many, targetName)
            },
            enabled = !demo && targetName != null,
        )
        describe(
            addToStory,
            when {
                demo -> res.getString(R.string.story_demo_add)
                targetName == null -> noTarget
                else -> res.getString(R.string.story_add_target, targetName)
            },
            enabled = !demo && targetName != null,
        )
        // A Pending row can't be deleted until its reprocess lands: the job would race it.
        describe(
            delete,
            when {
                demo -> res.getString(R.string.panel_demo_delete)
                targetName == null -> noTarget
                else -> res.getString(R.string.panel_delete_target, targetName)
            },
            enabled = !demo && targetName != null && targets.none { it.pending },
        )
        describe(focus, targetName?.let { res.getString(R.string.panel_focus_target, it) } ?: noTarget, enabled = targetName != null)
    }

    /** The master checkbox's ▾ (the web's `.select-menu`): All, None and Invert over the listed
     *  rows — All disabled once every one is checked, None once nothing is. */
    private fun showSelectMenu() {
        val listed = state.listed
        val checkedCount = listed.count { it.id in state.checked }
        val menu = PopupMenu(context, selectMenu)
        menu.menu.add(0, SELECT_ALL, 0, R.string.panel_select_all).isEnabled = checkedCount < listed.size
        menu.menu.add(0, SELECT_NONE, 1, R.string.panel_select_none).isEnabled = state.checked.isNotEmpty()
        menu.menu.add(0, SELECT_INVERT, 2, R.string.panel_select_invert)
        menu.setOnMenuItemClickListener { item ->
            when (item.itemId) {
                SELECT_ALL -> state.checkAll()
                SELECT_NONE -> state.clearChecked()
                SELECT_INVERT -> state.invertChecked()
            }
            changed()
            true
        }
        menu.show()
    }

    /** The toolbar's name for its target, for the delete confirmation: "3 checked activities
     *  (16 km)", or the selected row's own title and distance. */
    private fun targetSummary(targets: List<Activity>): String {
        val name = if (state.checked.isNotEmpty()) {
            res.getQuantityString(R.plurals.panel_checked_count, targets.size, targets.size)
        } else {
            res.getString(R.string.panel_target_one, PanelFormat.rowLabel(res, targets.first()))
        }
        return res.getString(R.string.panel_group_summary, name, PanelFormat.totalDistance(res, targets.sumOf { it.distanceMeters ?: 0.0 }))
    }

    /**
     * Add to story's menu under its button (`docs/SPEC.md` FR-5.16): New story…, then every
     * Story, newest first — read each time the menu opens — with how many activities it holds.
     * One that already holds all of the target is ticked and can't be picked. Picking a Story
     * adds the target to it in one request and closes the menu; a failure stays in the menu.
     */
    @SuppressLint("InflateParams") // A popup's content has no parent to inflate against.
    private fun showAddToStory() {
        val targets = state.targets
        if (targets.isEmpty() || Session.isDemo) return
        val ids = targets.map { it.id }
        val content = LayoutInflater.from(context).inflate(R.layout.popup_story_menu, null)
        val rows = content.findViewById<LinearLayout>(R.id.story_menu_rows)
        val note = content.findViewById<TextView>(R.id.story_menu_note)
        fun showNote(text: String, failed: Boolean) {
            note.text = text
            note.setTextColor(context.getColor(if (failed) R.color.hmt_danger else R.color.hmt_ink_meta))
            note.visibility = View.VISIBLE
        }
        val popup = PopupWindow(content, ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT, true)
        content.findViewById<View>(R.id.story_menu_new).setOnClickListener {
            popup.dismiss()
            StoryDialog.create(context, targetSummary(targets), ids, onStoryCreated)
        }
        showNote(res.getString(R.string.panel_loading), failed = false)
        HoldMyTrackApi.stories { result ->
            if (storyPopup !== popup) return@stories
            result.onSuccess { stories ->
                note.visibility = View.GONE
                content.findViewById<View>(R.id.story_menu_divider).visibility = if (stories.isEmpty()) View.GONE else View.VISIBLE
                for (story in stories) {
                    val row = LayoutInflater.from(context).inflate(R.layout.item_type_filter, rows, false)
                    val holdsAll = story.activityIds.containsAll(ids)
                    val count = row.findViewById<TextView>(R.id.type_filter_count)
                    row.findViewById<MaterialCheckBox>(R.id.type_filter_check).isChecked = holdsAll
                    row.findViewById<TextView>(R.id.type_filter_label).text = story.name
                    count.text = PanelFormat.count(res, story.activityIds.size)
                    row.isEnabled = !holdsAll
                    row.alpha = if (holdsAll) DISABLED_ROW_ALPHA else 1f
                    row.contentDescription = if (holdsAll) "${story.name}, ${res.getString(R.string.story_holds_all)}" else story.name
                    row.setOnClickListener {
                        count.setText(R.string.story_adding)
                        HoldMyTrackApi.changeStoryActivities(story.id, ids, add = true) { added ->
                            if (storyPopup !== popup) return@changeStoryActivities
                            added.onSuccess {
                                popup.dismiss()
                                onStoriesChanged()
                            }.onFailure { failure ->
                                count.text = PanelFormat.count(res, story.activityIds.size)
                                showNote(failure.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.story_save_failed), failed = true)
                            }
                        }
                    }
                    rows.addView(row)
                }
                // About twelve rows, then the list scrolls, as the Type dropdown's does.
                val scroll = content.findViewById<ScrollView>(R.id.story_menu_scroll)
                rows.measure(View.MeasureSpec.UNSPECIFIED, View.MeasureSpec.UNSPECIFIED)
                val max = (TYPE_LIST_MAX_DP * res.displayMetrics.density).toInt()
                scroll.layoutParams = scroll.layoutParams.apply {
                    height = if (rows.measuredHeight > max) max else ViewGroup.LayoutParams.WRAP_CONTENT
                }
            }.onFailure { failure ->
                showNote(failure.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.story_load_failed), failed = true)
            }
        }
        popup.elevation = res.getDimension(R.dimen.hmt_space_8)
        popup.setOnDismissListener {
            storyPopup = null
            storyPopupTarget = emptyList()
        }
        storyPopup = popup
        storyPopupTarget = ids
        popup.showAsDropDown(addToStory, 0, res.getDimensionPixelSize(R.dimen.hmt_space_8))
    }

    /**
     * Delete, over the toolbar's target, after a confirmation that says what goes with it
     * (`docs/SPEC.md` FR-5.11). The dialog stays up while the deletes run — one at a time, as
     * the web's are, rather than a burst racing each other's coverage re-render — and keeps a
     * failure on screen rather than closing over it.
     */
    private fun confirmDelete() {
        val targets = state.targets
        if (targets.isEmpty()) return
        val one = targets.size == 1
        val summary = targetSummary(targets)
        val dialog = MaterialAlertDialogBuilder(context, R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(if (one) R.string.panel_delete_one_title else R.string.panel_delete_title)
            .setMessage(res.getString(if (one) R.string.panel_delete_one_body else R.string.panel_delete_many_body, summary))
            .setPositiveButton(R.string.panel_delete_confirm, null)
            .setNegativeButton(android.R.string.cancel, null)
            .create()
        dialog.setOnShowListener {
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener { deleteNext(dialog, targets.map { it.id }, 0) }
        }
        dialog.show()
    }

    private fun deleteNext(dialog: AlertDialog, ids: List<String>, index: Int) {
        val confirm = dialog.getButton(AlertDialog.BUTTON_POSITIVE)
        val cancel = dialog.getButton(AlertDialog.BUTTON_NEGATIVE)
        if (index == ids.size) {
            dialog.dismiss()
            onDeleted(ids)
            return
        }
        dialog.setCancelable(false)
        confirm.isEnabled = false
        cancel.isEnabled = false
        confirm.setText(R.string.panel_deleting)
        HoldMyTrackApi.deleteActivity(ids[index]) { result ->
            result.onSuccess { deleteNext(dialog, ids, index + 1) }
                .onFailure { failure ->
                    // What did go is gone: report those, and say why the rest didn't.
                    if (index > 0) onDeleted(ids.take(index))
                    dialog.setCancelable(true)
                    confirm.isEnabled = true
                    cancel.isEnabled = true
                    confirm.setText(R.string.panel_delete_confirm)
                    dialog.setMessage(res.getString(R.string.panel_delete_failed, failure.message.orEmpty()))
                    confirm.setOnClickListener { deleteNext(dialog, ids, index) }
                }
        }
    }

    private fun describe(button: View, text: String, enabled: Boolean) {
        button.isEnabled = enabled
        button.contentDescription = text
        TooltipCompat.setTooltipText(button, text)
    }

    /** The Type dropdown, anchored under its trigger, closed by a tap outside it. */
    @SuppressLint("InflateParams") // A popup's content has no parent to inflate against.
    private fun showTypeFilter() {
        if (typePopup != null) return
        val content = LayoutInflater.from(context).inflate(R.layout.popup_type_filter, null)
        content.findViewById<View>(R.id.type_filter_all).apply {
            findViewById<TextView>(R.id.type_filter_label).apply {
                setText(R.string.panel_all_types)
                setTypeface(typeface, android.graphics.Typeface.BOLD)
            }
            findViewById<TextView>(R.id.type_filter_count).visibility = View.GONE
            setOnClickListener {
                state.includeAllTypes()
                changed()
            }
        }
        renderTypeRows(content)
        val popup = PopupWindow(content, ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT, true)
        popup.elevation = res.getDimension(R.dimen.hmt_space_8)
        popup.setOnDismissListener { typePopup = null }
        typePopup = popup
        popup.showAsDropDown(typeTrigger, 0, res.getDimensionPixelSize(R.dimen.hmt_space_8))
    }

    private fun renderTypeRows(content: View) {
        val facets = ActivityFacets.typeFacets(state.activities, state.distanceFilter)
        content.findViewById<View>(R.id.type_filter_empty).visibility = if (facets.isEmpty()) View.VISIBLE else View.GONE
        content.findViewById<View>(R.id.type_filter_body).visibility = if (facets.isEmpty()) View.GONE else View.VISIBLE
        content.findViewById<View>(R.id.type_filter_all)
            .findViewById<MaterialCheckBox>(R.id.type_filter_check).isChecked = state.excludedTypes.isEmpty()
        val rows = content.findViewById<LinearLayout>(R.id.type_filter_rows)
        rows.removeAllViews()
        val inflater = LayoutInflater.from(context)
        for (facet in facets) {
            val row = inflater.inflate(R.layout.item_type_filter, rows, false)
            val label = RecordingTypes.format(res, facet.type)
            val shown = facet.type !in state.excludedTypes
            row.findViewById<MaterialCheckBox>(R.id.type_filter_check).isChecked = shown
            row.findViewById<TextView>(R.id.type_filter_label).text = label
            row.findViewById<TextView>(R.id.type_filter_count).text = PanelFormat.count(res, facet.count)
            row.contentDescription = "$label, ${facet.count}"
            row.isSelected = shown
            row.setOnClickListener {
                state.toggleType(facet.type)
                changed()
            }
            rows.addView(row)
        }
        // About twelve rows, then the list scrolls (.activity-filters__type-list's max-height).
        val scroll = content.findViewById<ScrollView>(R.id.type_filter_scroll)
        rows.measure(View.MeasureSpec.UNSPECIFIED, View.MeasureSpec.UNSPECIFIED)
        val max = (TYPE_LIST_MAX_DP * res.displayMetrics.density).toInt()
        scroll.layoutParams = scroll.layoutParams.apply {
            height = if (rows.measuredHeight > max) max else ViewGroup.LayoutParams.WRAP_CONTENT
        }
    }

    /** Centres the selected row in the list — the list alone, never the sheet around it, so a
     *  collapsed sheet stays exactly as it was. */
    private fun scrollToFocused() {
        val position = adapter.currentList.indexOfFirst { it.activity.id == state.focused }
        if (position < 0) return
        list.post {
            val manager = list.layoutManager as LinearLayoutManager
            val rowHeight = list.getChildAt(0)?.height ?: 0
            manager.scrollToPositionWithOffset(position, (list.height - rowHeight) / 2)
        }
    }

    @SuppressLint("ClickableViewAccessibility") // A tap on no row at all; each row is its own target.
    private fun clearFocusOnEmptyTap() {
        val detector = GestureDetector(
            context,
            object : GestureDetector.SimpleOnGestureListener() {
                override fun onSingleTapUp(e: MotionEvent): Boolean {
                    if (list.findChildViewUnder(e.x, e.y) == null) clearFocus()
                    return false
                }
            },
        )
        list.setOnTouchListener { _, event ->
            detector.onTouchEvent(event)
            false
        }
    }

    private data class Note(val text: String, val failed: Boolean)

    /** The list's note, as its one last item — or no item at all. A tap on it clears the
     *  selection, like a tap on the list's empty space. */
    private inner class NoteAdapter : RecyclerView.Adapter<RecyclerView.ViewHolder>() {
        var note: Note? = null
            set(value) {
                if (field == value) return
                val had = field != null
                field = value
                when {
                    had && value == null -> notifyItemRemoved(0)
                    !had && value != null -> notifyItemInserted(0)
                    value != null -> notifyItemChanged(0)
                }
            }

        override fun getItemCount() = if (note == null) 0 else 1

        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): RecyclerView.ViewHolder {
            val view = LayoutInflater.from(parent.context).inflate(R.layout.item_panel_note, parent, false)
            view.setOnClickListener { clearFocus() }
            return object : RecyclerView.ViewHolder(view) {}
        }

        override fun onBindViewHolder(holder: RecyclerView.ViewHolder, position: Int) {
            val current = note ?: return
            (holder.itemView as TextView).apply {
                text = current.text
                setTextColor(context.getColor(if (current.failed) R.color.hmt_danger else R.color.panel_ink_50))
            }
        }
    }

    private inner class RowAdapter : ListAdapter<ActivityRowItem, ActivityRowHolder>(
        object : DiffUtil.ItemCallback<ActivityRowItem>() {
            override fun areItemsTheSame(old: ActivityRowItem, new: ActivityRowItem) = old.activity.id == new.activity.id
            override fun areContentsTheSame(old: ActivityRowItem, new: ActivityRowItem) = old == new
        },
    ) {
        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int) =
            ActivityRowHolder(LayoutInflater.from(parent.context).inflate(R.layout.item_activity_row, parent, false))

        override fun onBindViewHolder(holder: ActivityRowHolder, position: Int) = holder.bind(
            getItem(position),
            openStoryId = null,
            onCheck = { id ->
                state.toggleChecked(id)
                changed()
            },
            onSelect = ::select,
            onOpenStory = ::openStory,
        )
    }

    /** A row's Story badge: that Story, on the Stories tab, the sheet down so its tracks show. */
    private fun openStory(id: String) {
        setExpanded(false)
        onOpenStory(id)
    }

    private companion object {
        const val SHEET_ANIMATION_MS = 150L
        const val TYPE_LIST_MAX_DP = 192
        const val SNAP_METERS = 1.0
        const val UNSELECTED_TAB_ALPHA = 0.45f
        const val DISABLED_ROW_ALPHA = 0.5f
        const val SELECT_ALL = 1
        const val SELECT_NONE = 2
        const val SELECT_INVERT = 3
    }
}
