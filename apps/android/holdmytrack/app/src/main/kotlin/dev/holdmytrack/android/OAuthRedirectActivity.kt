package dev.holdmytrack.android

import android.app.Activity
import android.content.Intent
import android.os.Bundle

/**
 * Where a browser-tab sign-in comes back to the app: the server's callback redirects to
 * `holdmytrack://oauth?code=…` (or `?error=…`), and the browser hands that to this activity —
 * the only exported entry point for it, so [SignInActivity] itself stays unexported.
 *
 * It shows nothing and forwards the URI straight to [SignInActivity] with CLEAR_TOP, which
 * brings the existing sign-in screen back to the front and, in doing so, removes the Custom Tab
 * that was open on top of it. Any other app can send this URI too; that's harmless, since a
 * code is only redeemable with the verifier [SignInActivity] generated and kept to itself
 * (`docs/adr/0016-native-sign-in.md`).
 */
class OAuthRedirectActivity : Activity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        startActivity(
            Intent(this, SignInActivity::class.java)
                .setData(intent.data)
                .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP),
        )
        finish()
    }

    companion object {
        /** The redirect URI's scheme and host — `services/server/internal/httpapi/oauth.go`'s
         *  appRedirectURI, and this activity's intent filter in the manifest. */
        const val SCHEME = "holdmytrack"
        const val HOST = "oauth"
    }
}
