package dev.holdmytrack.android.settings

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
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import dev.holdmytrack.android.R
import java.text.Normalizer

/**
 * Settings' Country and Timezone fields: a long list to pick one entry from, with a search field
 * over it — the web page's native `<select>` with type-to-find, as a dialog, the way
 * `recording/ActivityTypePicker` does the Type field, and for the same reason (a dialog keeps the
 * list and the search field both above the keyboard). Closed, unlike the Type picker: only the
 * listed values can be chosen, since those are what the server accepts.
 *
 * A [Choice]'s [Choice.detail] is shown at the row's end and searched too — a timezone's region,
 * so "Europe" finds every European zone.
 */
object ChoicePicker {

    data class Choice(val value: String, val label: String, val detail: String = "")

    fun show(context: Context, title: String, current: String, choices: List<Choice>, onPick: (Choice) -> Unit) {
        val density = context.resources.displayMetrics.density
        val padding = context.resources.getDimensionPixelSize(R.dimen.hmt_space_16)
        val search = EditText(context).apply {
            hint = context.getString(R.string.settings_search_hint)
            inputType = InputType.TYPE_CLASS_TEXT
            imeOptions = EditorInfo.IME_ACTION_DONE
            isSingleLine = true
            // After isSingleLine, which resets the minimum to one line: 48dp, the touch target.
            minHeight = (48 * density).toInt()
        }
        val list = ListView(context)
        val empty = TextView(context).apply {
            setPadding(0, padding, 0, padding)
            setText(R.string.settings_no_match)
            visibility = View.GONE
        }
        val content = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(padding, padding / 2, padding, 0)
            addView(search)
            addView(empty)
            addView(list)
        }

        val adapter = ChoiceAdapter(context, current)
        adapter.shown = choices
        list.adapter = adapter

        val dialog = MaterialAlertDialogBuilder(context)
            .setTitle(title)
            .setView(content)
            .setNegativeButton(android.R.string.cancel, null)
            .create()

        fun pick(choice: Choice) {
            dialog.dismiss()
            if (choice.value != current) onPick(choice)
        }

        list.setOnItemClickListener { _, _, position, _ -> pick(adapter.shown[position]) }
        search.addTextChangedListener(object : TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
            override fun afterTextChanged(s: Editable?) {
                adapter.shown = filter(choices, s.toString().trim())
                empty.visibility = if (adapter.shown.isEmpty()) View.VISIBLE else View.GONE
            }
        })
        search.setOnEditorActionListener { _, actionId, _ ->
            if (actionId != EditorInfo.IME_ACTION_DONE) return@setOnEditorActionListener false
            adapter.shown.firstOrNull()?.let { pick(it) }
            true
        }

        dialog.show()
        list.setSelection(choices.indexOfFirst { it.value == current }.coerceAtLeast(0))
    }

    /** Labels starting with the query (or with a word that does) first, then any containing
     *  it in the label, the detail or the value — the Type picker's own ranking, closed. */
    private fun filter(choices: List<Choice>, text: String): List<Choice> {
        if (text.isEmpty()) return choices
        val q = fold(text)
        val first = mutableListOf<Choice>()
        val rest = mutableListOf<Choice>()
        for (c in choices) {
            val label = fold(c.label)
            when {
                label.startsWith(q) || label.contains(" $q") -> first += c
                label.contains(q) || fold(c.detail).contains(q) || fold(c.value).contains(q) -> rest += c
            }
        }
        return first + rest
    }

    /** Case- and accent-insensitive, as the Type picker folds. */
    private fun fold(s: String): String =
        Normalizer.normalize(s, Normalizer.Form.NFD).replace(Regex("\\p{Mn}+"), "").lowercase()

    private class ChoiceAdapter(private val context: Context, private val current: String) : BaseAdapter() {
        var shown: List<Choice> = emptyList()
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
                val vertical = context.resources.getDimensionPixelSize(R.dimen.hmt_space_12)
                setPadding(0, vertical, 0, vertical)
                addView(
                    TextView(context).apply { setTextAppearance(R.style.TextAppearance_HoldMyTrack_BodyLarge) },
                    LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f),
                )
                addView(
                    TextView(context).apply {
                        setTextAppearance(R.style.TextAppearance_HoldMyTrack_BodyMedium)
                        alpha = 0.6f
                    },
                )
            }
            val choice = shown[position]
            (row.getChildAt(0) as TextView).apply {
                text = choice.label
                // By weight within the theme's own family, as the Type picker bolds.
                typeface = Typeface.create(typeface, if (choice.value == current) 700 else 400, false)
            }
            (row.getChildAt(1) as TextView).text = choice.detail
            return row
        }
    }
}
