package dev.holdmytrack.android

import android.content.Context
import android.content.Intent
import android.os.Bundle
import androidx.appcompat.app.AppCompatActivity
import dev.holdmytrack.android.net.Session

/**
 * The app's main window: it hosts the map ([MapFragment]) and hands each new intent to it.
 *
 * Never shows the map without a session: a signed-out visitor is handed straight to
 * `SignInActivity`, mirroring web's `AuthGate` (`docs/IMPLEMENTATION.md` §4.13), before any layout
 * or `MapView` exists. A bare basemap with none of the three user layers — all behind
 * `requireAuth` server-side — would be a weak first impression next to the Demo account that
 * screen offers one tap away.
 */
class MainActivity : AppCompatActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (!Session.isSignedIn) {
            SignInActivity.open(this)
            finish()
            return
        }
        // Likewise an account whose email isn't confirmed: the server refuses it every tile.
        if (!Session.emailVerified) {
            VerifyEmailActivity.open(this)
            finish()
            return
        }
        setContentView(R.layout.activity_main)
        // A recreation (rotation, theme, language) restores the fragment with its own state.
        if (savedInstanceState == null) {
            supportFragmentManager.beginTransaction()
                .add(R.id.main_content, MapFragment(), TAG_MAP)
                .commitNow()
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        (supportFragmentManager.findFragmentByTag(TAG_MAP) as? MapFragment)?.onNewIntent(intent)
    }

    companion object {
        private const val TAG_MAP = "map"
        internal const val EXTRA_VIEW_ACTIVITY = "dev.holdmytrack.android.VIEW_ACTIVITY"
        internal const val EXTRA_VIEW_STARTED_AT = "dev.holdmytrack.android.VIEW_STARTED_AT"

        /** Opens the map on [activityId], started at [startedAt] — the Sync screen's View on
         *  map. Clears whatever is over an existing map, which then selects it. */
        fun viewOnMap(context: Context, activityId: String, startedAt: String) {
            context.startActivity(
                Intent(context, MainActivity::class.java)
                    .putExtra(EXTRA_VIEW_ACTIVITY, activityId)
                    .putExtra(EXTRA_VIEW_STARTED_AT, startedAt)
                    .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP),
            )
        }
    }
}