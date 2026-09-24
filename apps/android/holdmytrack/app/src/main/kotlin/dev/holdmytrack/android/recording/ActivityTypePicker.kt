package dev.holdmytrack.android.recording

import android.app.AlertDialog
import android.content.Context
import android.graphics.Typeface
import android.text.Editable
import android.text.InputType
import android.text.TextWatcher
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.inputmethod.EditorInfo
import android.widget.BaseAdapter
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ListView
import android.widget.TextView
import dev.holdmytrack.android.R
import java.text.Normalizer

/** One type the account already uses, and how many activities use it — the same facet the
 *  web client's Activities panel and Type picker are built from (`activityFacets.ts`). */
data class TypeCount(val type: String, val count: Int)

/**
 * `RecordingActivity`'s Type field — the Android counterpart of the web edit dialog's
 * `ActivityTypePicker.tsx`: a searchable list of the types this account already uses
 * (formatted, with how many activities use each), filtered as you type, and open-ended —
 * `activity_type` is free-form, not a controlled vocabulary, so any search text that doesn't
 * exactly match an existing type offers itself as a last "Add …" row and saves exactly as
 * typed, up to the server's 50-character limit (`maxActivityTypeLen`,
 * `services/server/internal/httpapi/activities.go`). A current value missing from [known] is
 * still listed, so the current choice is always there to see.
 *
 * A plain `AlertDialog` over a `ListView` rather than a popup under the field: on a phone the
 * soft keyboard takes half the screen, and a dialog is what keeps the list and the search field
 * both visible above it.
 */
object ActivityTypePicker {

    const val MAX_LENGTH = 50

    private data class Option(val value: String, val label: String, val detail: String)

    fun show(context: Context, current: String, known: List<TypeCount>, onPick: (String) -> Unit) {
        val options = buildList {
            if (current.isNotEmpty() && known.none { it.type == current }) add(option(current, null))
            known.forEach { add(option(it.type, it.count)) }
        }

        val padding = (16 * context.resources.displayMetrics.density).toInt()
        val search = EditText(context).apply {
            hint = context.getString(R.string.recording_type_search_hint)
            inputType = InputType.TYPE_CLASS_TEXT
            imeOptions = EditorInfo.IME_ACTION_DONE
            isSingleLine = true
        }
        val list = ListView(context)
        val empty = TextView(context).apply {
            setPadding(0, padding, 0, padding)
            visibility = View.GONE
        }
        val content = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(padding, padding / 2, padding, 0)
            addView(search)
            addView(empty)
            addView(list)
        }

        val adapter = OptionAdapter(context, current)
        adapter.shown = options
        list.adapter = adapter

        val dialog = AlertDialog.Builder(context)
            .setTitle(R.string.recording_type_hint)
            .setView(content)
            .setNegativeButton(android.R.string.cancel, null)
            .create()

        fun pick(value: String) {
            dialog.dismiss()
            if (value != current) onPick(value)
        }

        list.setOnItemClickListener { _, _, position, _ -> pick(adapter.shown[position].value) }
        search.addTextChangedListener(object : TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
            override fun afterTextChanged(s: Editable?) {
                val text = s.toString().trim()
                adapter.shown = filter(context, options, text)
                empty.text = context.getString(R.string.recording_type_no_match, text)
                empty.visibility = if (adapter.shown.isEmpty()) View.VISIBLE else View.GONE
            }
        })
        // The keyboard's Done picks the top row, like Enter on the web picker's active row.
        search.setOnEditorActionListener { _, actionId, _ ->
            if (actionId != EditorInfo.IME_ACTION_DONE) return@setOnEditorActionListener false
            adapter.shown.firstOrNull()?.let { pick(it.value) }
            true
        }

        dialog.show()
        list.setSelection(options.indexOfFirst { it.value == current }.coerceAtLeast(0))
    }

    private fun option(type: String, count: Int?) =
        Option(value = type, label = RecordingTypes.format(type).ifEmpty { type }, detail = count?.takeIf { it > 0 }?.toString().orEmpty())

    /** `SearchPicker.tsx`'s ranking: an exact whole-label match first ("walk" → Walk before
     *  Dog Walk), then labels with a word starting with the query, then anything containing it,
     *  keeping [options]' own order within each rank — plus the "Add …" row while nothing
     *  matches exactly. */
    private fun filter(context: Context, options: List<Option>, text: String): List<Option> {
        if (text.isEmpty()) return options
        val q = fold(text)
        val ranked = List(3) { mutableListOf<Option>() }
        for (o in options) {
            val label = fold(o.label)
            when {
                label == q -> ranked[0] += o
                label.startsWith(q) || label.contains(" $q") -> ranked[1] += o
                label.contains(q) || fold(o.value).contains(q) -> ranked[2] += o
            }
        }
        val matches = ranked.flatten()
        val exact = options.any { fold(it.value) == q || fold(it.label) == q }
        if (exact || text.length > MAX_LENGTH) return matches
        return matches + Option(text, context.getString(R.string.recording_type_add, text), context.getString(R.string.recording_type_new))
    }

    /** Case- and accent-insensitive, matching `SearchPicker.tsx`'s own `fold`. */
    private fun fold(s: String): String =
        Normalizer.normalize(s, Normalizer.Form.NFD).replace(Regex("\\p{Mn}+"), "").lowercase()

    private class OptionAdapter(private val context: Context, private val current: String) : BaseAdapter() {
        var shown: List<Option> = emptyList()
            set(value) {
                field = value
                notifyDataSetChanged()
            }

        override fun getCount() = shown.size
        override fun getItem(position: Int) = shown[position]
        override fun getItemId(position: Int) = position.toLong()

        override fun getView(position: Int, convertView: View?, parent: ViewGroup): View {
            val row = convertView as? LinearLayout ?: LinearLayout(context).apply {
                orientation = LinearLayout.HORIZONTAL
                gravity = Gravity.CENTER_VERTICAL
                val vertical = (12 * context.resources.displayMetrics.density).toInt()
                setPadding(0, vertical, 0, vertical)
                addView(TextView(context).apply { textSize = 16f }, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f))
                addView(TextView(context).apply { textSize = 13f; alpha = 0.6f })
            }
            val option = shown[position]
            (row.getChildAt(0) as TextView).apply {
                text = option.label
                setTypeface(null, if (option.value == current) Typeface.BOLD else Typeface.NORMAL)
            }
            (row.getChildAt(1) as TextView).text = option.detail
            return row
        }
    }
}
