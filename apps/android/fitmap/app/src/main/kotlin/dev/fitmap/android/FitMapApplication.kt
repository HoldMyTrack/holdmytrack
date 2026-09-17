package dev.fitmap.android

import android.app.Application
import dev.fitmap.android.net.FitMapApi
import dev.fitmap.android.net.Session
import org.maplibre.android.MapLibre
import org.maplibre.android.module.http.HttpRequestUtil

/**
 * Process-level setup, all of which has to happen before anything else runs.
 *
 * The load-bearing line is the last one. MapLibre Native fetches the style document, the
 * basemap archive and every tile through its *own* HTTP stack, which the app's API client
 * never sees — so a credential held only by the app client would reach none of the user
 * layers. `HttpRequestUtil.setOkHttpClient` replaces that stack's client with the app's own,
 * whose interceptor attaches the session token per request (`net/FitMapApi.kt`). This is the
 * Android half of the roadmap's mobile auth decision, and it is confirmed core-SDK API rather
 * than a React-Native shim.
 *
 * It belongs here, not in an Activity: the map SDK may issue a request as soon as a `MapView`
 * exists, and a client installed after that has already missed requests.
 */
class FitMapApplication : Application() {

    override fun onCreate() {
        super.onCreate()
        Session.init(this)
        // Loads the native library and installs the module provider a MapView's constructor
        // asks for immediately, so no Activity has to remember to do it first.
        MapLibre.getInstance(this)
        HttpRequestUtil.setOkHttpClient(FitMapApi.client)
    }
}
