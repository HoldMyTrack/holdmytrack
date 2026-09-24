package dev.holdmytrack.android

import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import dev.holdmytrack.android.net.Account
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session

/**
 * Sign in, create an account, or start a demo account — the three ways
 * `services/server/internal/httpapi/auth.go` mints a session.
 *
 * Deliberately plain: no icons, no type scale, no color tokens. The design freeze is Phase 3
 * of the root roadmap, after Mobile, and there is no design system to build against yet — so
 * this is built to work now and styled in that pass, rather than inventing a visual language
 * that would have to be thrown away.
 *
 * The app's first screen whenever no session is held, mirroring web's `AuthGate`: a bare
 * basemap with no tracks, fog or heatmap is a weak demonstration of what HoldMyTrack does, and the
 * Demo button here is a much stronger one, one tap away. So `MainActivity` never mounts the map
 * without a session — it hands off to [open] instead — and this screen is the root of its own
 * task, with nothing behind it to fall back to: Back leaves the app rather than revealing a map
 * that would have nothing of the user's on it.
 */
class SignInActivity : AppCompatActivity() {

    private lateinit var email: EditText
    private lateinit var password: EditText
    private lateinit var error: TextView
    private lateinit var buttons: List<Button>

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_sign_in)

        email = findViewById(R.id.email)
        password = findViewById(R.id.password)
        error = findViewById(R.id.error)

        val signIn: Button = findViewById(R.id.sign_in)
        val signUp: Button = findViewById(R.id.sign_up)
        val demo: Button = findViewById(R.id.demo)
        buttons = listOf(signIn, signUp, demo)

        signIn.setOnClickListener { withCredentials(HoldMyTrackApi::signIn) }
        signUp.setOnClickListener { withCredentials(HoldMyTrackApi::signUp) }
        demo.setOnClickListener {
            setBusy(true)
            HoldMyTrackApi.startDemo(::onResult)
        }
    }

    /**
     * The client-side check is only for the two fields being filled in at all. Everything else
     * — whether the address parses, whether the password is long enough, whether the account
     * exists — is the server's to judge, and its wording is what gets shown, so the two can
     * never disagree about what is acceptable.
     */
    private fun withCredentials(request: (String, String, (Result<Account>) -> Unit) -> Unit) {
        val address = email.text.toString().trim()
        val secret = password.text.toString()
        if (address.isEmpty() || secret.isEmpty()) {
            showError(getString(R.string.credentials_required))
            return
        }
        setBusy(true)
        request(address, secret, ::onResult)
    }

    private fun onResult(result: Result<Account>) {
        setBusy(false)
        result.onSuccess { account ->
            Session.start(account.token, account.email)
            startActivity(
                Intent(this, MainActivity::class.java)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK),
            )
            finish()
        }.onFailure { failure ->
            showError(failure.message ?: getString(R.string.sign_in_failed))
        }
    }

    private fun setBusy(busy: Boolean) {
        buttons.forEach { it.isEnabled = !busy }
        if (busy) error.visibility = View.GONE
    }

    private fun showError(message: String) {
        error.text = message
        error.visibility = View.VISIBLE
    }

    companion object {
        /** Replaces the whole back stack with this screen — used on a cold start with no
         *  session, when a stored token turns out to be revoked, and on sign-out, so no screen
         *  that needs a session is ever left underneath it. */
        fun open(context: Context) {
            context.startActivity(
                Intent(context, SignInActivity::class.java)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK),
            )
        }
    }
}
