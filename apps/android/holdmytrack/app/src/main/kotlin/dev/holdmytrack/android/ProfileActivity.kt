package dev.holdmytrack.android

import android.os.Bundle
import android.view.View
import android.widget.Button
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import com.google.android.material.imageview.ShapeableImageView
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session

/**
 * The account half of the burger menu: who is signed in — with the avatar and name Settings
 * holds, read fresh from `GET /v1/auth/me` — and the one action available from here, sign
 * out. Only reachable with a session (the map itself is gated behind one), so
 * signing out replaces the whole back stack with `SignInActivity` rather than returning to a
 * map that would have nothing left to show.
 */
class ProfileActivity : AppCompatActivity() {

    private lateinit var status: TextView
    private lateinit var name: TextView
    private lateinit var avatar: ShapeableImageView
    private lateinit var action: Button

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_profile)

        status = findViewById(R.id.profile_status)
        name = findViewById(R.id.profile_name)
        avatar = findViewById(R.id.profile_avatar)
        action = findViewById(R.id.profile_action)
        action.setOnClickListener { onAction() }
    }

    override fun onResume() {
        super.onResume()
        render()
        // Returning from Settings is a resume: the name and avatar may have just changed.
        HoldMyTrackApi.verifySession { result ->
            result.onSuccess { profile ->
                name.text = profile.displayName
                name.visibility = if (profile.displayName.isEmpty()) View.GONE else View.VISIBLE
                if (profile.avatarUrl.isNotEmpty()) {
                    HoldMyTrackApi.avatar(profile.avatarUrl) { image ->
                        image.onSuccess { bitmap ->
                            avatar.setPadding(0, 0, 0, 0)
                            avatar.imageTintList = null
                            avatar.setImageBitmap(bitmap)
                        }
                    }
                }
            }
        }
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
