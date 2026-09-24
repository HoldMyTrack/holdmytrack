package dev.holdmytrack.android

import android.os.Bundle
import android.widget.Button
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session

/**
 * The account half of the burger menu: who is signed in, and the one action available from
 * here — sign out. Only reachable with a session (the map itself is gated behind one), so
 * signing out replaces the whole back stack with `SignInActivity` rather than returning to a
 * map that would have nothing left to show.
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
        action.isEnabled = true
        action.setText(R.string.sign_out)
        status.text = if (Session.isDemo) {
            getString(R.string.signed_in_demo)
        } else {
            getString(R.string.signed_in_as, Session.email)
        }
    }

    private fun onAction() {
        action.isEnabled = false
        HoldMyTrackApi.signOut { SignInActivity.open(this) }
    }
}
