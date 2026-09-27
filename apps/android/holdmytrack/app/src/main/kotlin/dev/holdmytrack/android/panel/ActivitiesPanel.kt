package dev.holdmytrack.android.panel

import android.animation.ValueAnimator
import android.annotation.SuppressLint
import android.view.GestureDetector
import android.view.LayoutInflater
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.widget.ImageButton
import android.widget.ImageView
import android.widget.LinearLayout
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
import dev.holdmytrack.android.net.Duplicate
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.RecordingTypes
import kotlin.math.abs

/**
 * The map's Activities panel, as the web draws it at phone width
 * (`apps/web/src/ui/ActivitiesPanel.tsx` and index.css's phone layer): a bottom sheet over the
 * date-range footer, collapsed to its tab row and a "… loaded" line, expanded to most of the
 * screen by tapping that line. Under it: the Type dropdown and the DISTANCE slider, a toolbar
 * over the toolbar's target, the rows, the target's summary, and the duplicates disclosure.
 *
 * The rules live in [PanelState]; this draws it and turns taps into state changes.
 * `MainActivity` owns the map and the fetch: it hands over each list ([setActivities]) and
 * hears about every change that touches the map through [onMapChanged] (what to hide, what's
 * selected) and [onFly] (the activities to frame); Edit is [onEdit]'s to open, and a finished
 * Delete is reported through [onDeleted].
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
) {
    private val context = sheet.context
    private val res = context.resources

    private val count: TextView = sheet.findViewById(R.id.panel_count)
    private val toggle: View = sheet.findViewById(R.id.panel_toggle)
    private val subtext: TextView = sheet.findViewById(R.id.panel_subtext)
    private val chevron: ImageView = sheet.findViewById(R.id.panel_chevron)
    private val typeTrigger: View = sheet.findViewById(R.id.panel_type_trigger)
    private val typeDot: View = sheet.findViewById(R.id.panel_type_dot)
    private val distance: View = sheet.findViewById(R.id.panel_distance)
    private val distanceReadout: TextView = sheet.findViewById(R.id.panel_distance_readout)
    private val distanceSlider: RangeSlider = sheet.findViewById(R.id.panel_distance_slider)
    private val distanceMin: TextView = sheet.findViewById(R.id.panel_distance_min)
    private val distanceMax: TextView = sheet.findViewById(R.id.panel_distance_max)
    private val resetFilters: View = sheet.findViewById(R.id.panel_reset_filters)
    private val checkAll: MaterialCheckBox = sheet.findViewById(R.id.panel_check_all)
    private val invert: ImageButton = sheet.findViewById(R.id.panel_invert)
    private val visibility: ImageButton = sheet.findViewById(R.id.panel_visibility)
    private val edit: ImageButton = sheet.findViewById(R.id.panel_edit)
    private val delete: ImageButton = sheet.findViewById(R.id.panel_delete)
    private val focus: ImageButton = sheet.findViewById(R.id.panel_focus)
    private val list: RecyclerView = sheet.findViewById(R.id.panel_list)
    private val footer: TextView = sheet.findViewById(R.id.panel_footer)
    private val duplicatesBox: View = sheet.findViewById(R.id.panel_duplicates)
    private val duplicatesLabel: TextView = sheet.findViewById(R.id.panel_duplicates_label)
    private val duplicatesChevron: ImageView = sheet.findViewById(R.id.panel_duplicates_chevron)
    private val duplicatesList: LinearLayout = sheet.findViewById(R.id.panel_duplicates_list)

    private val adapter = RowAdapter()
    private val noteAdapter = NoteAdapter()

    private var loading = false
    private var error: String? = null
    private var duplicates: List<Duplicate> = emptyList()
    private var duplicatesFailed = false
    private var duplicatesOpen = false

    var expanded = false
        private set

    /** Forced down to the peek strip from outside without forgetting [expanded] — a recording
     *  or an edit window over the map; the sheet comes back as it was. */
    private var held = false
    private var heightAnimator: ValueAnimator? = null
    private var typePopup: PopupWindow? = null

    init {
        list.layoutManager = LinearLayoutManager(context)
        // The note — loading, nothing matching, the read failing — is the list's last item,
        // after whatever rows there are, as the web's is.
        list.adapter = ConcatAdapter(adapter, noteAdapter)
        list.itemAnimator = null
        clearFocusOnEmptyTap()

        toggle.setOnClickListener { setExpanded(!expanded) }
        // The collapsed height is the toggle row's bottom edge, whatever the font scale makes
        // of the tab row above it.
        toggle.addOnLayoutChangeListener { _, _, _, _, _, _, _, _, _ -> applyHeight(animate = false) }

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
        invert.setOnClickListener {
            state.invertChecked()
            changed()
        }
        TooltipCompat.setTooltipText(invert, res.getString(R.string.panel_invert))
        visibility.setOnClickListener {
            state.toggleTargetVisibility()
            changed()
        }
        focus.setOnClickListener {
            val hidden = state.mapHidden
            onFly(state.targets.filter { it.id !in hidden })
        }
        edit.setOnClickListener { state.targets.takeIf { it.isNotEmpty() }?.let(onEdit) }
        delete.setOnClickListener { confirmDelete() }
        sheet.findViewById<View>(R.id.panel_duplicates_toggle).setOnClickListener {
            duplicatesOpen = !duplicatesOpen
            renderDuplicates()
        }
        render()
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

    /** Null when the read failed: the disclosure then says so rather than disappearing. */
    fun setDuplicates(next: List<Duplicate>?) {
        duplicatesFailed = next == null
        duplicates = next.orEmpty()
        renderDuplicates()
    }

    /** A track tapped on the map: selects it, as a row tap does, and scrolls its row to the
     *  middle of the list — without expanding a collapsed sheet (`docs/SPEC.md` §17). */
    fun focusFromMap(id: String) {
        select(id)
        scrollToFocused()
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
    }

    fun dismissPopups() {
        typePopup?.dismiss()
    }

    /** The collapsed sheet's height: the tab row and the toggle line. */
    val peekHeight: Int
        get() = toggle.bottom

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
        heightAnimator?.cancel()
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
        count.text = PanelFormat.count(res, listed.size)
        subtext.text = if (loading && state.activities.isEmpty()) {
            res.getString(R.string.panel_loading)
        } else {
            // The whole range, whatever TYPE and DISTANCE narrow the rows to — the web's
            // range summary, which the two filters don't touch either.
            res.getString(R.string.panel_loaded, PanelFormat.totalDistance(res, state.activities.sumOf { it.distanceMeters ?: 0.0 }))
        }
        chevron.setImageResource(if (expanded) R.drawable.ic_chevron_down else R.drawable.ic_chevron_up)
        toggle.contentDescription = res.getString(
            if (expanded) R.string.panel_collapse else R.string.panel_expand,
        ) + ". " + subtext.text

        typeDot.visibility = if (state.excludedTypes.isNotEmpty()) View.VISIBLE else View.GONE
        renderDistance()
        resetFilters.visibility = if (state.hasActiveFilters) View.VISIBLE else View.GONE
        renderToolbar(listed)

        adapter.submitList(
            listed.map { Row(it, checked = it.id in state.checked, focused = it.id == state.focused, hidden = it.id in state.hidden) },
        )
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
        invert.isEnabled = listed.isNotEmpty()

        // The toolbar names its target, so which of the group and the selected row it acts on
        // is never a guess: "3 checked activities", or the selected row's own label.
        val targets = state.targets
        val targetName = when {
            targets.isEmpty() -> null
            state.checked.isNotEmpty() -> res.getQuantityString(R.plurals.panel_checked_count, targets.size, targets.size)
            else -> res.getString(R.string.panel_target_one, rowLabel(targets.first()))
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

    /** The toolbar's name for its target, for the delete confirmation: "3 checked activities
     *  (16 km)", or the selected row's own title and distance. */
    private fun targetSummary(targets: List<Activity>): String {
        val name = if (state.checked.isNotEmpty()) {
            res.getQuantityString(R.plurals.panel_checked_count, targets.size, targets.size)
        } else {
            res.getString(R.string.panel_target_one, rowLabel(targets.first()))
        }
        return res.getString(R.string.panel_group_summary, name, PanelFormat.totalDistance(res, targets.sumOf { it.distanceMeters ?: 0.0 }))
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

    private fun renderDuplicates() {
        if (duplicates.isEmpty() && !duplicatesFailed) {
            duplicatesBox.visibility = View.GONE
            return
        }
        duplicatesBox.visibility = View.VISIBLE
        duplicatesLabel.text = if (duplicatesFailed) {
            res.getString(R.string.panel_duplicates_failed)
        } else {
            res.getQuantityString(R.plurals.panel_duplicates_found, duplicates.size, duplicates.size)
        }
        duplicatesChevron.setImageResource(if (duplicatesOpen) R.drawable.ic_chevron_down else R.drawable.ic_chevron_up)
        duplicatesList.visibility = if (duplicatesOpen && !duplicatesFailed) View.VISIBLE else View.GONE
        duplicatesList.removeAllViews()
        if (!duplicatesOpen) return
        duplicates.forEachIndexed { i, d ->
            val line = buildString {
                append(PanelFormat.startedAt(res, d.startedAt))
                append(" · ")
                append(RecordingTypes.format(res, d.activityType))
                d.distanceMeters?.let { append(" · ").append(PanelFormat.distance(res, it)) }
                append('\n')
                append(res.getString(R.string.panel_duplicate_from, sourceName(d.source), sourceName(d.supersededBySource)))
            }
            duplicatesList.addView(
                TextView(context).apply {
                    text = line
                    setTextColor(context.getColor(R.color.hmt_ink_hint))
                    textSize = 11f
                    val pad = res.getDimensionPixelSize(R.dimen.hmt_space_6)
                    setPadding(0, pad, 0, pad)
                    if (i > 0) setBackgroundResource(R.drawable.bg_list_top)
                },
            )
        }
    }

    /** The `source` values, said the way a person would say them (the web's
     *  `formatIngestSource`). */
    private fun sourceName(source: String): String = when (source) {
        "healthconnect" -> res.getString(R.string.source_health_connect)
        "healthkit" -> res.getString(R.string.source_health_kit)
        "upload" -> res.getString(R.string.source_upload)
        "takeout" -> res.getString(R.string.source_takeout)
        "recorded" -> res.getString(R.string.source_recorded)
        else -> source
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

    /** A row's primary line: its name when it has one, else its start time — also how the
     *  toolbar names one selected activity. */
    private fun rowLabel(activity: Activity): String =
        activity.name?.trim()?.takeIf { it.isNotEmpty() } ?: PanelFormat.startedAt(res, activity.startedAt)

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

    /** One row as drawn: the activity and its marks, so a diff rebinds only what changed —
     *  a tick, the selection moving, a refetch — rather than every row, which also cancelled
     *  a tap on another row landing mid-rebind. */
    private data class Row(val activity: Activity, val checked: Boolean, val focused: Boolean, val hidden: Boolean)

    private inner class RowAdapter : ListAdapter<Row, RowHolder>(
        object : DiffUtil.ItemCallback<Row>() {
            override fun areItemsTheSame(old: Row, new: Row) = old.activity.id == new.activity.id
            override fun areContentsTheSame(old: Row, new: Row) = old == new
        },
    ) {
        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int) =
            RowHolder(LayoutInflater.from(parent.context).inflate(R.layout.item_activity_row, parent, false))

        override fun onBindViewHolder(holder: RowHolder, position: Int) = holder.bind(getItem(position))
    }

    private inner class RowHolder(view: View) : RecyclerView.ViewHolder(view) {
        private val check: MaterialCheckBox = view.findViewById(R.id.activity_check)
        private val text: View = view.findViewById(R.id.activity_text)
        private val title: TextView = view.findViewById(R.id.activity_title)
        private val meta: TextView = view.findViewById(R.id.activity_meta)
        private val badges: View = view.findViewById(R.id.activity_badges)
        private val pending: View = view.findViewById(R.id.activity_pending)
        private val hidden: View = view.findViewById(R.id.activity_hidden)

        fun bind(row: Row) {
            val activity = row.activity
            val label = rowLabel(activity)
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
            badges.visibility = if (activity.pending || isHidden) View.VISIBLE else View.GONE
            pending.visibility = if (activity.pending) View.VISIBLE else View.GONE
            hidden.visibility = if (isHidden) View.VISIBLE else View.GONE

            check.setOnCheckedChangeListener(null)
            check.isChecked = isChecked
            // Pending is disabled until its reprocess lands, except that a checked one can
            // still be unchecked.
            check.isEnabled = !activity.pending || isChecked
            check.contentDescription = res.getString(if (isChecked) R.string.panel_row_uncheck else R.string.panel_row_check, label)
            check.setOnCheckedChangeListener { _, _ ->
                state.toggleChecked(activity.id)
                changed()
            }

            text.isEnabled = !activity.pending
            text.contentDescription = res.getString(R.string.panel_fly_to, label) + ". " + meta.text
            text.setOnClickListener { select(activity.id) }
        }
    }

    private companion object {
        const val SHEET_ANIMATION_MS = 150L
        const val PENDING_ALPHA = 0.45f
        const val HIDDEN_ALPHA = 0.55f
        const val TYPE_LIST_MAX_DP = 192
        const val SNAP_METERS = 1.0
    }
}
