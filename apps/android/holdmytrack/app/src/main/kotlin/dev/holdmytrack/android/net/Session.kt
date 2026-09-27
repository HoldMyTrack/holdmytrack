package dev.holdmytrack.android.net

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
 * cookie jar (`apps/android/docs/ARCHITECTURE.md` §1.1): one
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

    private const val PREFS_NAME = "holdmytrack.session"
    private const val KEY_TOKEN = "session_token"
    private const val KEY_EMAIL = "email"
    private const val KEY_EMAIL_VERIFIED = "email_verified"
    private const val KEY_COUNTRY = "country"
    private const val KEY_LOCALE = "locale"
    private const val KEY_TIMEZONE = "timezone"

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

    /**
     * Whether the account's email address is confirmed — the server's `requireVerified` gate,
     * which answers every tile and sync request of an unconfirmed account with `403
     * email_not_verified`. Always true for a demo account (the server says so, since the gate
     * never applies to one). Kept on disk so a cold start goes straight to the right screen,
     * then refreshed by every `GET /v1/auth/me`; a session stored before this was recorded
     * reads as confirmed until that first check says otherwise.
     */
    val emailVerified: Boolean get() = prefs.getBoolean(KEY_EMAIL_VERIFIED, true)

    /**
     * The account's Country (ISO code), empty until Settings is first saved — which is what the
     * first-run gate keys on (`MainActivity`), and what decides the app's units
     * (`recording/Units`). Kept on disk, like [emailVerified], so a cold start routes and
     * formats correctly before the first `GET /v1/auth/me` answers; that answer refreshes it.
     */
    val country: String get() = prefs.getString(KEY_COUNTRY, "").orEmpty()

    /** The account's Timezone (an IANA name), which decides which calendar day an activity
     *  falls on — the server's, and so the map's date range's. Empty until the first profile
     *  that carries it. */
    val timezone: String get() = prefs.getString(KEY_TIMEZONE, "").orEmpty()

    /** The account's Language, empty for automatic — see `AppLanguage`. */
    val locale: String get() = prefs.getString(KEY_LOCALE, "").orEmpty()

    fun start(token: String, profile: Profile) {
        cachedToken = token
        verified = true
        prefs.edit()
            .putString(KEY_TOKEN, token)
            .putString(KEY_EMAIL, profile.email)
            .also { store(it, profile) }
            .apply()
    }

    /** What `GET /v1/auth/me` just confirmed: the token is live, and the account as it is now. */
    fun markVerified(profile: Profile) {
        verified = true
        update(profile)
    }

    /** Takes in a profile the server just returned — a Settings save, an avatar change. */
    fun update(profile: Profile) {
        prefs.edit().also { store(it, profile) }.apply()
    }

    private fun store(editor: SharedPreferences.Editor, profile: Profile) {
        editor
            .putBoolean(KEY_EMAIL_VERIFIED, profile.emailVerified)
            .putString(KEY_COUNTRY, profile.country)
            .putString(KEY_LOCALE, profile.locale)
            .putString(KEY_TIMEZONE, profile.timezone)
    }

    fun clear() {
        cachedToken = null
        verified = false
        prefs.edit()
            .remove(KEY_TOKEN)
            .remove(KEY_EMAIL)
            .remove(KEY_EMAIL_VERIFIED)
            .remove(KEY_COUNTRY)
            .remove(KEY_LOCALE)
            .remove(KEY_TIMEZONE)
            .apply()
    }
}
