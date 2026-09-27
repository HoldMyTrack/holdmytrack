package dev.holdmytrack.android.panel

import android.view.View
import android.view.inputmethod.InputMethodManager
import android.widget.Button
import android.widget.TextView
import com.google.android.material.textfield.TextInputEditText
import com.google.android.material.textfield.TextInputLayout
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.recording.ActivityTypePicker
import dev.holdmytrack.android.recording.RecordingTypes
import dev.holdmytrack.android.recording.TypeCount

/**
 * The Edit window, the web's `apps/web/src/ui/EditActivityWindow.tsx` Activity tab
 * (`docs/SPEC.md` FR-5.10): a card over the top of the map, opened from the Activities
 * toolbar's Edit over its target. One activity edits Type, Name and Description; several edit
 * Type only — Name and Description have nothing to set consistently across different
 * activities, so they're disabled with a note saying why rather than silently skipped. Save
 * writes only what changed (one `PATCH /v1/activities/{id}` each, one after another, a
 * group's resending each activity's own name and description) and closes; a refusal keeps the
 * window open with the server's reason. Cancel, or Back, closes without writing.
 *
 * The web's Track tab (FR-5.14) isn't built here yet.
 */
class EditActivityWindow(
    private val card: View,
    /** Called once the window closes; `saved` is whether anything was written. */
    private val onClose: (saved: Boolean) -> Unit,
) {
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

    private var activities: List<Activity> = emptyList()
    private var known: List<TypeCount> = emptyList()

    /** The raw `activity_type` the Type field holds — the field itself shows its label. */
    private var activityType = ""
    private var saving = false

    /** Set once the fields are written, so a Save retried after a failure doesn't write them
     *  twice, and a Cancel after one still reports that something changed. */
    private var fieldsSaved = false

    val isOpen: Boolean
        get() = card.visibility == View.VISIBLE

    init {
        typeField.setOnClickListener { openTypePicker() }
        typeLayout.setEndIconOnClickListener { openTypePicker() }
        cancel.setOnClickListener { close() }
        save.setOnClickListener { save() }
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
        activityType = group.first().activityType
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
        typeField.setText(RecordingTypes.format(res, activityType))
        nameField.setText(single?.name.orEmpty())
        descriptionField.setText(single?.description.orEmpty())
        nameLayout.isEnabled = single != null
        descriptionLayout.isEnabled = single != null
        multiNote.visibility = if (single == null) View.VISIBLE else View.GONE
        showError(null)
        renderBusy()
        card.visibility = View.VISIBLE
    }

    /** Cancel: nothing more is written. */
    fun close() {
        if (saving) return
        hideKeyboard()
        card.visibility = View.GONE
        onClose(fieldsSaved)
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
            type.isEmpty() -> res.getString(R.string.edit_type_required)
            type.length > MAX_TYPE -> res.getString(R.string.edit_type_too_long, MAX_TYPE)
            name.length > MAX_NAME -> res.getString(R.string.edit_name_too_long, MAX_NAME)
            description.length > MAX_DESCRIPTION -> res.getString(R.string.edit_description_too_long, MAX_DESCRIPTION)
            else -> null
        }
        if (invalid != null) {
            showError(invalid)
            return
        }
        val changed = if (single != null) {
            type != single.activityType || name != single.name.orEmpty() || description != single.description.orEmpty()
        } else {
            activities.any { it.activityType != type }
        }
        if (!changed || fieldsSaved) {
            finish()
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
            finish()
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

    private fun finish() {
        saving = false
        hideKeyboard()
        card.visibility = View.GONE
        onClose(fieldsSaved)
    }

    private fun renderBusy() {
        save.isEnabled = !saving
        cancel.isEnabled = !saving
        save.setText(if (saving) R.string.edit_saving else R.string.edit_save)
        typeLayout.isEnabled = !saving
        nameField.isEnabled = !saving && activities.size == 1
        descriptionField.isEnabled = !saving && activities.size == 1
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
    }
}
