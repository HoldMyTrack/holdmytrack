package dev.fitmap.android

import android.os.Bundle
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import dev.fitmap.android.net.Account
import dev.fitmap.android.net.FitMapApi
import dev.fitmap.android.net.Session

/**
 * Sign in, create an account, or start a demo account — the three ways
 * `services/server/internal/httpapi/auth.go` mints a session.
 *
 * Deliberately plain: no icons, no type scale, no color tokens. The design freeze is Phase 3
 * of the root roadmap, after Mobile, and there is no design system to build against yet — so
 * this is built to work now and styled in that pass, rather than inventing a visual language
 * that would have to be thrown away.
 *
 * Reachable only from the map, and dismissable back to it, because the map is the app: the
 * style endpoint is unauthenticated and the basemap archive is a plain `pmtiles://` URL, so a
 * signed-out FitMap is a working map with no user layers — not a login wall.
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

        signIn.setOnClickListener { withCredentials(FitMapApi::signIn) }
        signUp.setOnClickListener { withCredentials(FitMapApi::signUp) }
        demo.setOnClickListener {
            setBusy(true)
            FitMapApi.startDemo(::onResult)
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
}
