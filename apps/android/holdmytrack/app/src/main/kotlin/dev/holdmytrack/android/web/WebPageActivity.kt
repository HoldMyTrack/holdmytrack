package dev.holdmytrack.android.web

import android.annotation.SuppressLint
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.net.Uri
import android.os.Bundle
import android.view.View
import android.webkit.CookieManager
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.browser.customtabs.CustomTabsIntent
import com.google.android.material.color.MaterialColors
import dev.holdmytrack.android.BuildConfig
import dev.holdmytrack.android.MainActivity
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import java.io.File
import java.util.concurrent.Executors

/**
 * One of the web's own pages, shown in the app (root `docs/SPEC.md` FR-10.8): Activity graph &
 * trends, Sync history, Settings (a new account's first run included) and About. The server
 * renders a page for the app when the request carries the `hmt_app` cookie — no site header,
 * footer or Back to map, and none of what the app keeps as screens of its own — and this screen
 * hands the WebView the session as the cookie a browser would hold.
 *
 * Where a link goes ([route]): the map (`/`, Sync history's View on map) opens the native map;
 * the same page again (a pager, Week/Month, Shade by, a form's redirect back to it) stays here;
 * anything else opens in a browser tab. The page follows the app's Theme with no help from
 * here: a WebView answers `prefers-color-scheme` from the screen's own light or dark theme.
 */
class WebPageActivity : AppCompatActivity() {

    /** A page the app shows: its path, its title, and whether to read it again whenever the
     *  screen comes back to the front, as the native screens before it did. */
    enum class Page(val path: String, val title: Int, val refreshOnReturn: Boolean) {
        PROFILE("/profile", R.string.you_graph_link, true),
        SYNC_HISTORY("/sync", R.string.sync_history_title, true),
        SETTINGS("/settings", R.string.settings_title, false),
        ABOUT("/about", R.string.menu_about, false),
    }

    private val page get() = Page.valueOf(intent.getStringExtra(EXTRA_PAGE) ?: Page.ABOUT.name)
    private val onboarding get() = intent.getBooleanExtra(EXTRA_ONBOARDING, false)

    private lateinit var web: WebView
    private lateinit var progress: View
    private lateinit var error: View

    /** Set once [route] has sent the user elsewhere: a navigation can reach it twice. */
    private var leaving = false

    /** Whether the page on screen failed to load: the WebView's own error page is never shown. */
    private var failed = false

    /** The page's file input waiting for the photo picker's answer, which it must get once. */
    private var fileCallback: ValueCallback<Array<Uri>>? = null

    /** Android's photo picker for Settings' avatar: no storage permission, only the image the
     *  user picks, made small first ([AvatarImage]) and handed to the page's file input. */
    private val pickImage = registerForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        val callback = fileCallback ?: return@registerForActivityResult
        fileCallback = null
        if (uri == null) {
            callback.onReceiveValue(null)
            return@registerForActivityResult
        }
        io.execute {
            val file = runCatching {
                AvatarImage.shrink(this, uri)?.let { File(cacheDir, AVATAR_FILE).apply { writeBytes(it) } }
            }.getOrNull()
            runOnUiThread {
                if (file == null) Toast.makeText(this, R.string.settings_avatar_unreadable, Toast.LENGTH_LONG).show()
                callback.onReceiveValue(file?.let { arrayOf(Uri.fromFile(it)) })
            }
        }
    }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (!Session.isSignedIn) {
            finish()
            return
        }
        setContentView(R.layout.activity_web_page)
        setTitle(if (onboarding) R.string.settings_onboarding_title else page.title)
        web = findViewById(R.id.web_page)
        progress = findViewById(R.id.web_page_progress)
        error = findViewById(R.id.web_page_error)
        findViewById<View>(R.id.web_page_retry).setOnClickListener { load() }

        // The theme's background until the page paints, rather than a white flash in dark.
        web.setBackgroundColor(MaterialColors.getColor(web, android.R.attr.colorBackground))
        // The pages' own scripts: Settings' searchable pickers and avatar upload, Profile's taps.
        web.settings.javaScriptEnabled = true
        web.settings.domStorageEnabled = true
        web.webViewClient = Client()
        web.webChromeClient = object : WebChromeClient() {
            override fun onShowFileChooser(view: WebView, callback: ValueCallback<Array<Uri>>, params: FileChooserParams): Boolean {
                fileCallback?.onReceiveValue(null)
                fileCallback = callback
                pickImage.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
                return true
            }
        }
        load()
    }

    override fun onRestart() {
        super.onRestart()
        if (page.refreshOnReturn && !failed) web.reload()
    }

    override fun onDestroy() {
        if (::web.isInitialized) web.destroy()
        super.onDestroy()
    }

    private fun load() {
        setCookies()
        failed = false
        error.visibility = View.GONE
        web.visibility = View.VISIBLE
        web.loadUrl(BuildConfig.API_BASE_URL + page.path)
    }

    /**
     * The session, the in-app marker and the app's language, as cookies on the API's origin, set
     * on every load so they're always the current account's. The WebView's cookie store is the
     * app's own, shared with no browser; `Session.clear` empties it on sign-out. The language
     * cookie is what the page falls back to when the account's Language is Automatic: the app's
     * own language, which a per-app language can make differ from the phone's.
     */
    private fun setCookies() {
        val secure = if (BuildConfig.API_BASE_URL.startsWith("https://")) "; Secure" else ""
        val cookies = CookieManager.getInstance()
        cookies.setCookie(BuildConfig.API_BASE_URL, "$SESSION_COOKIE=${Session.token}; Path=/; HttpOnly; SameSite=Lax$secure")
        cookies.setCookie(BuildConfig.API_BASE_URL, "$APP_COOKIE=android; Path=/; SameSite=Lax$secure")
        val lang = if (resources.configuration.locales[0].language == "ru") "ru" else "en"
        cookies.setCookie(BuildConfig.API_BASE_URL, "$LANG_COOKIE=$lang; Path=/; SameSite=Lax$secure")
        cookies.flush()
    }

    /**
     * Where a navigation to [uri] goes, reporting whether this screen took it away from the
     * WebView. Checked as a link is followed and again as a page starts, since a form's redirect
     * (Settings' first run, saved, goes on to `/`) isn't always offered as a link.
     */
    private fun route(uri: Uri): Boolean {
        val base = Uri.parse(BuildConfig.API_BASE_URL)
        if (uri.scheme != base.scheme || uri.authority != base.authority) {
            openOutside(uri)
            return true
        }
        val path = uri.path.orEmpty().ifEmpty { "/" }
        if (path == page.path) return false
        if (leaving) return true
        // The session has ended, or the account isn't the map's yet: the app's own session
        // check, back on the map, knows what to do.
        if (path == "/signin" || path == "/verify-pending") {
            leaving = true
            finish()
            return true
        }
        if (path == "/") {
            leaving = true
            val activity = uri.getQueryParameter("activity")
            val day = uri.getQueryParameter("day")
            when {
                activity != null && day != null -> MainActivity.viewOnMap(this, activity, day)
                // The first run's save: the map gates on Session.country, which only the
                // server has heard about so far, so it's read before the map opens.
                onboarding -> {
                    HoldMyTrackApi.verifySession { result ->
                        result.onSuccess { Session.update(it) }
                        startActivity(
                            Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK),
                        )
                        finish()
                    }
                    return true
                }
            }
            finish()
            return true
        }
        openOutside(uri)
        return true
    }

    private fun openOutside(uri: Uri) {
        try {
            if (uri.scheme == "http" || uri.scheme == "https") {
                CustomTabsIntent.Builder().build().launchUrl(this, uri)
            } else {
                startActivity(Intent(Intent.ACTION_VIEW, uri))
            }
        } catch (_: ActivityNotFoundException) {
        }
    }

    private inner class Client : WebViewClient() {
        override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean =
            request.isForMainFrame && route(request.url)

        override fun onPageStarted(view: WebView, url: String, favicon: Bitmap?) {
            if (route(Uri.parse(url))) {
                view.stopLoading()
                return
            }
            progress.visibility = View.VISIBLE
        }

        override fun onPageFinished(view: WebView, url: String) {
            progress.visibility = View.GONE
        }

        override fun onReceivedError(view: WebView, request: WebResourceRequest, err: WebResourceError) {
            if (!request.isForMainFrame) return
            failed = true
            progress.visibility = View.GONE
            web.visibility = View.INVISIBLE
            error.visibility = View.VISIBLE
        }
    }

    companion object {
        private const val EXTRA_PAGE = "page"
        private const val EXTRA_ONBOARDING = "onboarding"
        private const val AVATAR_FILE = "avatar.jpg"

        /** The server's session cookie (`sessionCookieName`), its in-app marker
         *  (`appCookieName`) and its language cookie (`i18n.CookieName`). */
        private const val SESSION_COOKIE = "holdmytrack_session"
        private const val APP_COOKIE = "hmt_app"
        private const val LANG_COOKIE = "hmt_lang"

        private val io = Executors.newSingleThreadExecutor()

        fun open(context: Context, page: Page) {
            context.startActivity(Intent(context, WebPageActivity::class.java).putExtra(EXTRA_PAGE, page.name))
        }

        /** A new account's first run: Settings as the root of a new task, with no map behind it
         *  to go back to until it's saved, which goes on to the map. */
        fun openOnboarding(context: Context) {
            context.startActivity(
                Intent(context, WebPageActivity::class.java)
                    .putExtra(EXTRA_PAGE, Page.SETTINGS.name)
                    .putExtra(EXTRA_ONBOARDING, true)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK),
            )
        }
    }
}
