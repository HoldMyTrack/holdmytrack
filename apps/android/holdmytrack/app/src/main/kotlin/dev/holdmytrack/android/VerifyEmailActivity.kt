package dev.holdmytrack.android

import android.content.Context
import android.content.Intent
import android.graphics.Typeface
import android.os.Bundle
import android.text.SpannableString
import android.text.Spanned
import android.text.style.StyleSpan
import android.view.View
import android.widget.Button
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import dev.holdmytrack.android.net.ApiException
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session

/**
 * "Check your email" — what a signed-in account whose address isn't confirmed yet sees instead
 * of the map, the app's counterpart of the web's `/verify-pending`. The server answers every
 * tile and sync request of such an account with `403 email_not_verified` (`requireVerified`,
 * `services/server/internal/httpapi/auth.go`), so the map would open with nothing on it and
 * nothing on screen saying why.
 *
 * The link in the email is clicked in a browser, which the app never hears about, so this
 * screen asks `GET /v1/auth/me` again on every resume — coming back from the mail app is the
 * moment it's most likely to have changed — and on Continue. Resend and Sign out are the web
 * page's other two ways forward; changing a mistyped address stays on the web, since the
 * app's local recordings and sync watermark are keyed by the email and would be orphaned.
 *
 * The root of its own task like [SignInActivity]: Back leaves the app rather than revealing a
 * map this account can't load yet.
 */
class VerifyEmailActivity : AppCompatActivity() {

    private lateinit var notice: TextView
    private lateinit var error: TextView
    private lateinit var buttons: List<Button>

    /** Guards against a second `GET /v1/auth/me` while one is in flight. */
    private var checking = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (!Session.isSignedIn) {
            SignInActivity.open(this)
            finish()
            return
        }
        setContentView(R.layout.activity_verify_email)

        findViewById<TextView>(R.id.verify_hint).text = hint(Session.email)
        notice = findViewById(R.id.verify_notice)
        error = findViewById(R.id.verify_error)
        val proceed: Button = findViewById(R.id.verify_continue)
        val resend: Button = findViewById(R.id.verify_resend)
        val signOut: Button = findViewById(R.id.verify_sign_out)
        buttons = listOf(proceed, resend, signOut)

        proceed.setOnClickListener { check(userAsked = true) }
        resend.setOnClickListener { resend() }
        signOut.setOnClickListener {
            setBusy(true)
            HoldMyTrackApi.signOut { SignInActivity.open(this) }
        }
    }

    override fun onResume() {
        super.onResume()
        if (Session.isSignedIn) check(userAsked = false)
    }

    /**
     * Confirmed → the map. A `401` means the session itself is gone, as on the map. Anything
     * else is reported only when the user asked, via Continue — the automatic check on resume
     * stays quiet, since the screen already says what to do.
     */
    private fun check(userAsked: Boolean) {
        if (checking) return
        checking = true
        if (userAsked) setBusy(true)
        HoldMyTrackApi.verifySession { result ->
            checking = false
            setBusy(false)
            result.onSuccess { emailVerified ->
                Session.markVerified(emailVerified)
                if (emailVerified) {
                    startActivity(
                        Intent(this, MainActivity::class.java)
                            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK),
                    )
                    finish()
                } else if (userAsked) {
                    show(getString(R.string.verify_email_not_yet), failed = false)
                }
            }.onFailure { failure ->
                if (failure is ApiException && failure.code == 401) {
                    Session.clear()
                    SignInActivity.open(this)
                    finish()
                } else if (userAsked) {
                    show(getString(R.string.verify_email_check_failed, failure.message.orEmpty()), failed = true)
                }
            }
        }
    }

    /** The server's own wording either way: its confirmation, or its refusal (rate limit). */
    private fun resend() {
        setBusy(true)
        HoldMyTrackApi.resendVerification { result ->
            setBusy(false)
            result.onSuccess { text -> show(text, failed = false) }
                .onFailure { failure -> show(failure.message.orEmpty(), failed = true) }
        }
    }

    private fun setBusy(busy: Boolean) {
        buttons.forEach { it.isEnabled = !busy }
        if (busy) {
            notice.visibility = View.GONE
            error.visibility = View.GONE
        }
    }

    /** A notice for what the server confirmed or reported, the error box for a failure — the
     *  web page's `.auth-card__notice` and `__error`. */
    private fun show(text: String, failed: Boolean) {
        val (shown, hidden) = if (failed) error to notice else notice to error
        shown.text = text
        shown.visibility = View.VISIBLE
        hidden.visibility = View.GONE
    }

    /** The hint with the address in bold, as the web's `<strong>` sets it off. */
    private fun hint(email: String): CharSequence {
        val text = getString(R.string.verify_email_hint, email)
        val start = text.indexOf(email)
        if (email.isEmpty() || start < 0) return text
        return SpannableString(text).apply {
            setSpan(StyleSpan(Typeface.BOLD), start, start + email.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
        }
    }

    companion object {
        /** Replaces the whole back stack with this screen, as [SignInActivity.open] does. */
        fun open(context: Context) {
            context.startActivity(
                Intent(context, VerifyEmailActivity::class.java)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK),
            )
        }
    }
}
