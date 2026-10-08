package dev.holdmytrack.android.panel

import android.annotation.SuppressLint
import android.content.Context
import android.text.Editable
import android.text.TextWatcher
import android.view.LayoutInflater
import android.view.View
import android.view.inputmethod.EditorInfo
import android.widget.TextView
import androidx.appcompat.app.AlertDialog
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.textfield.TextInputEditText
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Story

/**
 * Send a copy, the web's `SendCopyDialog.tsx` (`docs/SPEC.md` FR-14.6 item 14, FR-14.7): a
 * Story folder's send button, asking for the email address to send a copy of the Story to,
 * over `dialog_send_copy`. Send stays disabled until the field has something in it, and the
 * keyboard's Send does the same as tapping it. Once sent it stays up on "Sent to …", with the
 * same words whether or not the address has an account — the server answers the same either
 * way — and the field cleared for another address; a refusal stays on screen as typed.
 */
object SendCopyDialog {

    @SuppressLint("InflateParams") // A dialog's content has no parent to inflate against.
    fun show(context: Context, story: Story) {
        val content = LayoutInflater.from(context).inflate(R.layout.dialog_send_copy, null)
        val email: TextInputEditText = content.findViewById(R.id.send_copy_email)
        val status: TextView = content.findViewById(R.id.send_copy_status)
        val dialog = MaterialAlertDialogBuilder(context)
            .setTitle(context.getString(R.string.story_send_title, story.name))
            .setView(content)
            .setPositiveButton(R.string.story_send_confirm, null)
            .setNegativeButton(android.R.string.cancel, null)
            .create()
        var sending = false
        var sent = false

        fun address() = email.text?.toString().orEmpty().trim()
        fun renderButtons() {
            val positive = dialog.getButton(AlertDialog.BUTTON_POSITIVE)
            positive.isEnabled = !sending && address().isNotEmpty()
            positive.setText(if (sending) R.string.story_sending else R.string.story_send_confirm)
            dialog.getButton(AlertDialog.BUTTON_NEGATIVE).apply {
                isEnabled = !sending
                setText(if (sent) R.string.story_send_close else android.R.string.cancel)
            }
            email.isEnabled = !sending
            dialog.setCancelable(!sending)
        }

        fun submit() {
            val to = address()
            if (sending || to.isEmpty()) return
            sending = true
            status.visibility = View.GONE
            renderButtons()
            HoldMyTrackApi.sendStory(story.id, to) { result ->
                sending = false
                result.onSuccess {
                    sent = true
                    email.setText("")
                    status.text = context.getString(R.string.story_send_done, to)
                    status.setTextColor(context.getColor(R.color.hmt_ink))
                }.onFailure { failure ->
                    status.text = failure.message?.takeIf { it.isNotBlank() } ?: context.getString(R.string.story_save_failed)
                    status.setTextColor(context.getColor(R.color.hmt_danger))
                }
                status.visibility = View.VISIBLE
                renderButtons()
            }
        }

        email.addTextChangedListener(object : TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
            override fun afterTextChanged(s: Editable?) = renderButtons()
        })
        email.setOnEditorActionListener { _, actionId, _ ->
            if (actionId != EditorInfo.IME_ACTION_SEND) return@setOnEditorActionListener false
            submit()
            true
        }
        dialog.setOnShowListener {
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener { submit() }
            renderButtons()
        }
        dialog.show()
        email.requestFocus()
    }
}