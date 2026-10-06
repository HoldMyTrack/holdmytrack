package dev.holdmytrack.android.panel

import android.view.View
import android.view.inputmethod.InputMethodManager
import android.widget.Button
import android.widget.TextView
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.textfield.TextInputEditText
import com.google.android.material.textfield.TextInputLayout
import androidx.appcompat.widget.TooltipCompat
import dev.holdmytrack.android.R
import dev.holdmytrack.android.map.EditPreview
import dev.holdmytrack.android.map.PhotoMarkerOverlay
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.ApiException
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Photo
import dev.holdmytrack.android.net.TrackPoint
import dev.holdmytrack.android.recording.ActivityTypePicker
import dev.holdmytrack.android.recording.RecordingTypes
import dev.holdmytrack.android.recording.TypeCount

/**
 * The Edit window, after the web's `apps/web/src/ui/EditActivityWindow.tsx` (`docs/SPEC.md`
 * FR-5.10, FR-5.14, FR-16.6): a card over the top of the map, opened from the Activities
 * toolbar's Edit over its target, with three tabs behind one Save and Cancel.
 *
 *  - **Activity**: one activity edits Type, Name and Description; several edit Type only —
 *    Name and Description have nothing to set consistently across different activities, so
 *    they're disabled with a note saying why rather than silently skipped.
 *  - **Track** ([TrackEditor]): one activity with a finished track, else disabled saying why.
 *    Opening it the first time starts the map's track session ([onStartTrack]: the other
 *    tracks away, the camera on this one), which lasts until the window closes.
 *  - **Photos** ([PhotosTab]): one activity with a track, else disabled saying why. Its
 *    changes are a draft, like the track edit, written by the same Save.
 *
 * Save writes what changed — the fields first (one `PATCH /v1/activities/{id}` each, one after
 * another, a group's resending each activity's own name and description), then the photos,
 * then the track edit — and closes; a refusal keeps the window open with the server's reason,
 * a retried Save skips what already went, and a later step failing still reports the earlier
 * ones saved. Cancel writes nothing; Back does the same, but asks first while there are photo
 * changes, which can be a lot of picking and sliding to lose to a reflex.
 */
class EditActivityWindow(
    private val card: View,
    /** The edit bar over the top of the map — Cancel, the title, Save — shown with [card]. */
    private val bar: View,
    /** The Track tab opened on [Activity] for the first time. */
    private val onStartTrack: (Activity) -> Unit,
    /** The track editor's overlay — see [TrackEditor]. */
    onDrawTrack: (visible: List<TrackPoint>?, lo: Int, hi: Int, preview: EditPreview, split: Int?) -> Unit,
    /** Add photos: open the system photo picker, and hand what's picked to [photosTab]. */
    onPickPhotos: () -> Unit,
    /** The Photos tab's draft on the map — see [PhotosTab]. */
    onPhotoOverlay: (PhotoMarkerOverlay?) -> Unit,
    /** The tab showing changed — the map hides the photos while the Track tab has it. */
    private val onTabChanged: () -> Unit,
    /** Called once the window closes: whether anything was written, whether that included a
     *  track edit, which leaves the activity Pending until its reprocess lands, and whether any
     *  photo was written, so the map reads them again. */
    private val onClose: (saved: Boolean, trackApplied: Boolean, photosSaved: Boolean) -> Unit,
) {
    private enum class Tab { ACTIVITY, TRACK, PHOTOS }
    private val context = card.context
    private val res = context.resources

    private val title: TextView = bar.findViewById(R.id.edit_title)
    private val subtitle: TextView = bar.findViewById(R.id.edit_subtitle)
    private val typeLayout: TextInputLayout = card.findViewById(R.id.edit_type_layout)
    private val typeField: TextInputEditText = card.findViewById(R.id.edit_type)
    private val nameLayout: TextInputLayout = card.findViewById(R.id.edit_name_layout)
    private val nameField: TextInputEditText = card.findViewById(R.id.edit_name)
    private val descriptionLayout: TextInputLayout = card.findViewById(R.id.edit_description_layout)
    private val descriptionField: TextInputEditText = card.findViewById(R.id.edit_description)
    private val multiNote: View = card.findViewById(R.id.edit_multi_note)
    private val error: TextView = card.findViewById(R.id.edit_error)
    private val cancel: Button = bar.findViewById(R.id.edit_cancel)
    private val save: Button = bar.findViewById(R.id.edit_save)
    private val tabActivity: TextView = card.findViewById(R.id.edit_tab_activity)
    private val tabTrack: TextView = card.findViewById(R.id.edit_tab_track)
    private val trackDot: View = card.findViewById(R.id.edit_tab_track_dot)
    private val tabPhotos: TextView = card.findViewById(R.id.edit_tab_photos)
    private val photosCount: TextView = card.findViewById(R.id.edit_tab_photos_count)
    private val photosDot: View = card.findViewById(R.id.edit_tab_photos_dot)
    private val activityPanel: View = card.findViewById(R.id.edit_activity_panel)
    private val trackPanel: View = card.findViewById(R.id.edit_track_panel)
    private val photosPanel: View = card.findViewById(R.id.edit_photos_panel)

    /** The Track tab's editor — `MapFragment` hands it Delete point's map taps. */
    val trackEditor = TrackEditor(trackPanel, onDrawTrack) { renderTabs() }

    /** The Photos tab — `MapFragment` hands it the saved photos and what the picker returns. */
    val photosTab = PhotosTab(photosPanel, onPickPhotos, onPhotoOverlay) {
        renderTabs()
        // A change to the photos answers whatever Save last refused about them.
        if (!saving) showError(null)
    }

    private var tab = Tab.ACTIVITY
    private var trackStarted = false

    /** Why the Track tab can't open, or null when it can. */
    private var trackUnavailable: String? = null

    /** Why the Photos tab can't open, or null when it can. */
    private var photosUnavailable: String? = null
    private var activities: List<Activity> = emptyList()
    private var known: List<TypeCount> = emptyList()

    /** The raw `activity_type` the Type field holds — the field itself shows its label. Empty
     *  for a group of mixed types until one is picked: empty keeps each activity's own. */
    private var activityType = ""

    /** The group's activities don't all share one type. */
    private var mixedTypes = false
    private var saving = false

    /** "Saving photos n of m…" while the photos are written. */
    private var photoProgress: Pair<Int, Int>? = null

    /** Set once the fields are written, so a Save retried after a failure doesn't write them
     *  twice, and a Cancel after one still reports that something changed. */
    private var fieldsSaved = false

    val isOpen: Boolean
        get() = card.visibility == View.VISIBLE

    /** The Track tab is the one showing — the photos are off the map meanwhile. */
    val showingTrack: Boolean
        get() = isOpen && tab == Tab.TRACK

    /** The one activity the window is open on, or null for a group. */
    val single: Activity?
        get() = activities.singleOrNull()?.takeIf { isOpen }

    init {
        typeField.setOnClickListener { openTypePicker() }
        typeLayout.setEndIconOnClickListener { openTypePicker() }
        cancel.setOnClickListener { close() }
        save.setOnClickListener { save() }
        tabActivity.setOnClickListener { showTab(Tab.ACTIVITY) }
        tabTrack.setOnClickListener { showTab(Tab.TRACK) }
        tabPhotos.setOnClickListener { showTab(Tab.PHOTOS) }
    }

    private fun showTab(next: Tab) {
        if (next == Tab.TRACK && trackUnavailable != null) return
        if (next == Tab.PHOTOS && photosUnavailable != null) return
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
        if (next == Tab.PHOTOS) photosTab.open(activities.single())
        photosTab.active = next == Tab.PHOTOS
        hideKeyboard()
        renderTabs()
        onTabChanged()
    }

    /** The selected tab underlined, the others dimmed; one that can't apply dimmer still,
     *  saying why — the web's disabled tab and its title. */
    private fun renderTabs() {
        val unavailable = mapOf(Tab.TRACK to trackUnavailable, Tab.PHOTOS to photosUnavailable)
        for ((view, which) in listOf(tabActivity to Tab.ACTIVITY, tabTrack to Tab.TRACK, tabPhotos to Tab.PHOTOS)) {
            val selected = tab == which
            if (selected) view.setBackgroundResource(R.drawable.bg_panel_tab_selected) else view.background = null
            view.alpha = when {
                selected -> 1f
                unavailable[which] != null -> DISABLED_TAB_ALPHA
                else -> UNSELECTED_TAB_ALPHA
            }
            view.isSelected = selected
        }
        tabTrack.isEnabled = trackUnavailable == null
        tabTrack.contentDescription = trackUnavailable?.let { res.getString(R.string.edit_tab_track) + ". " + it }
            ?: if (trackEditor.changed) res.getString(R.string.edit_track_unsaved) else null
        TooltipCompat.setTooltipText(tabTrack, trackUnavailable)
        trackDot.visibility = if (trackEditor.changed) View.VISIBLE else View.GONE

        val photosChanged = !photosTab.draft.isEmpty
        val count = photosTab.count
        tabPhotos.isEnabled = photosUnavailable == null
        tabPhotos.contentDescription = photosUnavailable?.let { res.getString(R.string.photos_tab) + ". " + it }
            ?: listOfNotNull(
                res.getString(if (photosChanged) R.string.photos_unsaved else R.string.photos_tab),
                count.takeIf { it > 0 }?.toString(),
            ).joinToString(", ")
        TooltipCompat.setTooltipText(tabPhotos, photosUnavailable)
        photosCount.text = count.toString()
        photosCount.visibility = if (photosUnavailable == null && count > 0) View.VISIBLE else View.GONE
        photosCount.alpha = tabPhotos.alpha
        photosDot.visibility = if (photosChanged) View.VISIBLE else View.GONE

        activityPanel.visibility = if (tab == Tab.ACTIVITY) View.VISIBLE else View.GONE
        trackPanel.visibility = if (tab == Tab.TRACK) View.VISIBLE else View.GONE
        photosPanel.visibility = if (tab == Tab.PHOTOS) View.VISIBLE else View.GONE
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
        photoProgress = null
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
            single.isPrivate -> res.getString(R.string.activity_private_note)
            single.bbox == null -> res.getString(R.string.edit_track_no_track)
            else -> null
        }
        // Photos belong to one activity, each with a place on its track.
        photosUnavailable = when {
            single == null -> res.getString(R.string.photos_check_one)
            single.isPrivate -> res.getString(R.string.activity_private_note)
            single.bbox == null -> res.getString(R.string.photos_no_track)
            else -> null
        }
        trackStarted = false
        tab = Tab.ACTIVITY
        showError(null)
        renderBusy()
        renderTabs()
        // Scrolls past most of the screen, so the map keeps a band to edit on above it.
        card.findViewById<MaxHeightScrollView>(R.id.edit_scroll).maxHeight =
            (res.displayMetrics.heightPixels * MAX_HEIGHT_FRACTION).toInt()
        card.visibility = View.VISIBLE
        bar.visibility = View.VISIBLE
    }

    /** The saved photos of the one activity the window is open on, or why they couldn't be read
     *  — `MapFragment` owns them, since they're also the map's markers. */
    fun setPhotos(photos: List<Photo>, failure: String?) = photosTab.setPhotos(photos, failure)

    /** Cancel: nothing more is written. */
    fun close() {
        if (saving) return
        finish(trackApplied = false)
    }

    /** Back: Cancel, but asking first while there are photo changes. */
    fun back() {
        if (saving) return
        if (photosTab.draft.isEmpty) {
            close()
            return
        }
        MaterialAlertDialogBuilder(context, R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(R.string.photos_discard_title)
            .setMessage(R.string.photos_discard_message)
            .setPositiveButton(R.string.photos_discard) { _, _ -> close() }
            .setNegativeButton(R.string.photos_keep_editing, null)
            .show()
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
        // Every new photo needs its place before anything is written, and a caption in bounds.
        photosTab.invalid()?.let {
            showError(it)
            showTab(Tab.PHOTOS)
            return
        }
        val changed = if (single != null) {
            type != single.activityType || name != single.name.orEmpty() || description != single.description.orEmpty()
        } else {
            type.isNotEmpty() && activities.any { it.activityType != type }
        }
        if (!changed || fieldsSaved) {
            savePhotos()
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
            savePhotos()
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

    /** The photo draft, once the fields are in — when the Photos tab changed anything. */
    private fun savePhotos() {
        val single = activities.singleOrNull()
        if (single == null || photosTab.draft.isEmpty) {
            saveTrack()
            return
        }
        saving = true
        showError(null)
        hideKeyboard()
        renderBusy()
        photosTab.save(single.id, onProgress = { done, total ->
            photoProgress = done to total
            renderBusy()
        }) { failure ->
            photoProgress = null
            if (failure == null) {
                saveTrack()
                return@save
            }
            saving = false
            renderBusy()
            showError(failure.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.edit_save_failed))
        }
    }

    /** The track edit, once the fields and photos are in — when the Track tab changed anything. */
    private fun saveTrack() {
        val single = activities.singleOrNull()
        if (single == null || !trackEditor.changed) {
            finish(trackApplied = false)
            return
        }
        saving = true
        renderBusy()
        sendTrack(single.id, allowHidden = false)
    }

    private fun sendTrack(id: String, allowHidden: Boolean) {
        saving = true
        renderBusy()
        HoldMyTrackApi.trackEdit(id, trackEditor.pending, trackEditor.splitAt, allowHidden) { result ->
            result.onSuccess { finish(trackApplied = true) }
                .onFailure { failure ->
                    saving = false
                    renderBusy()
                    if ((failure as? ApiException)?.errorCode == HoldMyTrackApi.SPLIT_HIDES_PART) {
                        // Everything before the track is written; the question is the split alone.
                        confirmHiddenSplit(id, failure.message.orEmpty())
                        return@onFailure
                    }
                    showError(failure.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.edit_save_failed))
                }
        }
    }

    /** A split leaving one part inside a Private location: the server's explanation, and the
     *  choice to go ahead (`docs/SPEC.md` FR-5.17 behavior 7). */
    private fun confirmHiddenSplit(id: String, message: String) {
        MaterialAlertDialogBuilder(context, R.style.ThemeOverlay_HoldMyTrack_Dialog)
            .setTitle(R.string.edit_track_split_hidden_title)
            .setMessage(message)
            .setPositiveButton(R.string.edit_track_split_hidden_confirm) { _, _ -> sendTrack(id, allowHidden = true) }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun finish(trackApplied: Boolean) {
        saving = false
        hideKeyboard()
        trackEditor.close()
        val photosSaved = photosTab.photosSaved
        photosTab.close()
        card.visibility = View.GONE
        bar.visibility = View.GONE
        onClose(fieldsSaved || trackApplied, trackApplied, photosSaved)
    }

    private fun renderBusy() {
        save.isEnabled = !saving
        cancel.isEnabled = !saving
        val progress = photoProgress
        save.text = when {
            progress != null -> res.getString(R.string.photos_saving_n, minOf(progress.first + 1, progress.second), progress.second)
            saving -> res.getString(R.string.edit_saving)
            else -> res.getString(R.string.edit_save)
        }
        typeLayout.isEnabled = !saving
        nameField.isEnabled = !saving && activities.size == 1
        descriptionField.isEnabled = !saving && activities.size == 1
        trackEditor.busy = saving
        photosTab.busy = saving
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
        /** The window's tallest, as a share of the screen, before it scrolls. */
        const val MAX_HEIGHT_FRACTION = 0.55f

        /** The server's own bounds (`activities.go`'s maxActivityTypeLen and friends). */
        const val MAX_TYPE = ActivityTypePicker.MAX_LENGTH
        const val MAX_NAME = 200
        const val MAX_DESCRIPTION = 2000
        const val UNSELECTED_TAB_ALPHA = 0.45f
        const val DISABLED_TAB_ALPHA = 0.3f
    }
}
