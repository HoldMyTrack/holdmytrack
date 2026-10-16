package dev.holdmytrack.android.net

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.os.Handler
import android.os.Looper
import dev.holdmytrack.android.UpdateAppActivity
import java.util.concurrent.atomic.AtomicBoolean

/**
 * The server's refusal of a build older than its minimum: `426 app_outdated` on any `/v1/`
 * request (root `docs/IMPLEMENTATION.md` §4.33). Seen once, in [HoldMyTrackApi]'s client, for
 * every call the app makes, so no screen has to recognise it; the Update screen covers whatever
 * was showing. The caller still gets its failure as usual, which leaves anything waiting to sync
 * on the phone.
 *
 * It holds the application's own context, which lives as long as the process: nothing to leak.
 */
@SuppressLint("StaticFieldLeak")
object AppOutdated {

    /** Set from the first refusal until the Update screen is gone, so a burst of refused
     *  calls opens it once. */
    private val showing = AtomicBoolean(false)
    private val main = Handler(Looper.getMainLooper())
    private lateinit var app: Context

    fun init(context: Context) {
        app = context.applicationContext
    }

    /**
     * Opens the Update screen over the app's screens, unless it's already up. Over them, not
     * in place of them: this runs before the refused call's own callback, and clearing the task
     * here would hand that callback a screen already destroyed.
     */
    fun refused() {
        if (!showing.compareAndSet(false, true)) return
        main.post {
            app.startActivity(Intent(app, UpdateAppActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }
    }

    /** The Update screen is gone; the next refusal opens it again. */
    fun dismissed() {
        showing.set(false)
    }
}
