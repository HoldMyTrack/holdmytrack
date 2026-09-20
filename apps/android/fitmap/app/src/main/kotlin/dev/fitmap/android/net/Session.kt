package dev.fitmap.android.net

import android.content.Context
import android.content.SharedPreferences

/**
 * The credential the app holds for the API: an opaque `sessions` row id the server minted at
 * sign-in and hands back as `session_token` (`services/server/internal/httpapi/auth.go`),
 * presented afterwards as `Authorization: Bearer <id>`.
 *
 * Process-wide rather than per-Activity because two unrelated HTTP surfaces read it — the
 * app's own API calls, and MapLibre Native's internal tile fetching, which never goes through
 * the app's client at all. That split is exactly why the roadmap chose a bearer token over a
 * cookie jar (`apps/android/docs/ROADMAP.md`, "Decide the mobile auth surface"): one
 * per-request interceptor can reach both, a cookie jar reaches only the first.
 *
 * The token is mirrored in a `@Volatile` field as well as in SharedPreferences because the
 * interceptor reads it on every tile request, on MapLibre's own threads — a disk-backed read
 * per tile would be both slow and needlessly unsynchronized.
 *
 * Storage is plain `MODE_PRIVATE` SharedPreferences, not an encrypted store. The file lives in
 * the app's private data directory, which is unreadable by other apps, and the threat an
 * encrypted store defends against — an attacker with physical read access to that directory —
 * already has everything else the app holds. Revocation is what actually bounds a leaked
 * token: signing out deletes the row server-side, so a copied token stops working immediately.
 */
object Session {

    private const val PREFS_NAME = "fitmap.session"
    private const val KEY_TOKEN = "session_token"
    private const val KEY_EMAIL = "email"

    private lateinit var prefs: SharedPreferences

    @Volatile
    private var cachedToken: String? = null

    /**
     * Whether the stored token has been checked against the server in this process. A token
     * restored from disk may have been revoked (a sign-out elsewhere) or expired past its
     * 30-day TTL while the app was closed, and the first thing that would notice is a wall of
     * 401s on tile requests — which surface only in logcat, as a blank map. So the map waits
     * for `GET /v1/auth/me` to confirm the token before attaching any layer that needs it.
     * A token that was just minted by a sign-in is verified by definition.
     */
    @Volatile
    var verified: Boolean = false
        private set

    fun init(context: Context) {
        prefs = context.applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        cachedToken = prefs.getString(KEY_TOKEN, null)
    }

    val token: String? get() = cachedToken

    val isSignedIn: Boolean get() = cachedToken != null

    /** Empty for a demo account, whose real email the server deliberately never returns. */
    val email: String get() = prefs.getString(KEY_EMAIL, "").orEmpty()

    /** The one client-side signal for "this is the read-only demo account" — the same
     *  emptiness [email] already carries, named so every caller that needs to gate a mutating
     *  action (sync, in particular — `requireNotDemo`, `services/server/internal/httpapi/
     *  auth.go`, rejects it server-side regardless) doesn't re-derive the check its own way. */
    val isDemo: Boolean get() = isSignedIn && email.isEmpty()

    fun start(token: String, email: String) {
        cachedToken = token
        verified = true
        prefs.edit().putString(KEY_TOKEN, token).putString(KEY_EMAIL, email).apply()
    }

    fun markVerified() {
        verified = true
    }

    fun clear() {
        cachedToken = null
        verified = false
        prefs.edit().remove(KEY_TOKEN).remove(KEY_EMAIL).apply()
    }
}
