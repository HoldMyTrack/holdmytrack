package dev.holdmytrack.android.settings

import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.lifecycle.lifecycleScope
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.textfield.TextInputEditText
import com.google.android.material.textfield.TextInputLayout
import dev.holdmytrack.android.R
import dev.holdmytrack.android.SignInActivity
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import dev.holdmytrack.android.sync.SyncCursor
import kotlinx.coroutines.launch

/**
 * Delete account (root `docs/SPEC.md` FR-1.11), from the You tab, as on the web: a dialog asks
 * for the account's email, the server closes the account, and this device forgets it.
 */
object AccountDeletion {

    /** The email typed into a dialog, as the web's form asks for it. The server says whether it
     *  matched; the dialog stays up with its error until it does or is cancelled. */
    fun confirm(activity: AppCompatActivity) {
        val view = activity.layoutInflater.inflate(R.layout.dialog_delete_account, null)
        val layout = view.findViewById<TextInputLayout>(R.id.delete_email_layout)
        val field = view.findViewById<TextInputEditText>(R.id.delete_email)
        view.findViewById<TextView>(R.id.delete_message).text = activity.getString(R.string.settings_delete_confirm, Session.email)
        // What goes, above how to confirm: the You tab's button says no more than "Delete account…".
        val dialog = MaterialAlertDialogBuilder(activity, R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(R.string.settings_delete_title)
            .setMessage(R.string.settings_delete_hint)
            .setView(view)
            .setNegativeButton(android.R.string.cancel, null)
            .setPositiveButton(R.string.settings_delete_submit, null)
            .show()
        val confirm = dialog.getButton(android.app.AlertDialog.BUTTON_POSITIVE)
        confirm.setOnClickListener {
            layout.error = null
            confirm.isEnabled = false
            HoldMyTrackApi.deleteAccount(field.text.toString()) { result ->
                result.onSuccess {
                    dialog.dismiss()
                    forget(activity)
                }.onFailure {
                    confirm.isEnabled = true
                    layout.error = it.message.orEmpty()
                }
            }
        }
    }

    /**
     * What this device keeps for the deleted account goes too: its unsynced recordings and its
     * Health Connect watermark — both keyed by the email, which a new account can take again —
     * then the session, and on to the sign-in screen, which says the account was deleted.
     */
    private fun forget(activity: AppCompatActivity) {
        val email = Session.email
        val context = activity.applicationContext
        activity.lifecycleScope.launch {
            RecordedActivityStore(context).deleteAll()
            SyncCursor(context, email).reset()
            Session.clear()
            SignInActivity.open(activity, R.string.account_deleted)
        }
    }
}
