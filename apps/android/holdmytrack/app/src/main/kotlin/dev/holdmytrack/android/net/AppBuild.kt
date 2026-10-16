package dev.holdmytrack.android.net

/**
 * What the app and the server say to each other about which build this is, as plain values:
 * the User-Agent naming it, the server's refusal of one too old (root `docs/IMPLEMENTATION.md`
 * §4.33) and where an update comes from. [AppOutdated] acts on them.
 */
object AppBuild {

    private const val PLAY_INSTALLER = "com.android.vending"

    /** What the User-Agent names this build — `HoldMyTrack-Android/0.14 (62da50b; 812)`: the
     *  release number, the commit and the versionCode, which the server compares against its
     *  minimum. */
    fun userAgent(versionName: String, gitSha: String, versionCode: Int): String =
        "HoldMyTrack-Android/$versionName ($gitSha; $versionCode)"

    /** Whether a response to [url] with [code] is the server turning this build away: only
     *  its own `/v1/`, since nothing else answers 426 for that reason. */
    fun isRefusal(url: String, apiBaseUrl: String, code: Int): Boolean =
        code == 426 && url.startsWith("$apiBaseUrl/v1/")

    /**
     * Where Update leads: the app's Play listing for a Play install, otherwise the sideloaded
     * APK the deployment serves at `/download/` (root `docs/DEPLOY.md` §10), which is the only
     * other way the app is handed out.
     */
    fun updateLink(installer: String?, apiBaseUrl: String, packageName: String): String =
        if (installer == PLAY_INSTALLER) "https://play.google.com/store/apps/details?id=$packageName"
        else "$apiBaseUrl/download/holdmytrack.apk"
}
