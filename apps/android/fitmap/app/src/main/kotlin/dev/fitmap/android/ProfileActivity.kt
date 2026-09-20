package dev.fitmap.android

import android.content.Intent
import android.os.Bundle
import android.widget.Button
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import dev.fitmap.android.net.FitMapApi
import dev.fitmap.android.net.Session

/**
 * The account half of the burger menu: who is signed in, and the one action available from
 * here — sign in or sign out. `MainActivity` re-reads `Session` on every resume (returning from
 * here is a resume), so nothing further has to be pushed back to the map explicitly.
 */
class ProfileActivity : AppCompatActivity() {

    private lateinit var status: TextView
    private lateinit var action: Button

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_profile)

        status = findViewById(R.id.profile_status)
        action = findViewById(R.id.profile_action)
        action.setOnClickListener { onAction() }
    }

    override fun onResume() {
        super.onResume()
        render()
    }

    private fun render() {
        val signedIn = Session.isSignedIn
        action.isEnabled = true
        action.setText(if (signedIn) R.string.sign_out else R.string.sign_in)
        status.text = when {
            !signedIn -> getString(R.string.signed_out)
            Session.email.isEmpty() -> getString(R.string.signed_in_demo)
            else -> getString(R.string.signed_in_as, Session.email)
        }
    }

    private fun onAction() {
        if (!Session.isSignedIn) {
            startActivity(Intent(this, SignInActivity::class.java))
            return
        }
        action.isEnabled = false
        FitMapApi.signOut { render() }
    }
}
