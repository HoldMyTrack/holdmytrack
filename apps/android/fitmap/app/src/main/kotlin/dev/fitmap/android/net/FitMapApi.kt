package dev.fitmap.android.net

import android.os.Handler
import android.os.Looper
import dev.fitmap.android.BuildConfig
import java.io.IOException
import okhttp3.Call
import okhttp3.Callback
import okhttp3.Dispatcher
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import org.json.JSONObject

/** An account with a live session — what a successful sign-in, sign-up or demo start yields. */
data class Account(val token: String, val email: String)

/**
 * A request the server answered with a non-2xx status. The API writes its errors as plain
 * text (`http.Error`), so `message` is the server's own wording, shown to the user as-is
 * rather than replaced with something vaguer — "invalid email or password" and "an account
 * with this email already exists" are exactly what someone needs to read.
 */
class ApiException(val code: Int, message: String) :
    IOException(message.ifBlank { "request failed ($code)" })

/**
 * The app's whole HTTP surface: the four auth endpoints, plus the one activity read the map
 * uses to frame its opening camera.
 *
 * Deliberately callback-based over OkHttp's own `enqueue` rather than coroutine-based. There
 * are five calls in the entire app and OkHttp is already a dependency (MapLibre Native pulls
 * it in), so a coroutines runtime plus the lifecycle-scope artifacts would be two new
 * dependencies bought for five call sites. Every callback is posted back to the main thread,
 * so callers touch views directly without re-dispatching.
 */
object FitMapApi {

    private const val API_V1 = "/v1"
    private val JSON = "application/json; charset=utf-8".toMediaType()
    private val main = Handler(Looper.getMainLooper())

    /**
     * One client for the app's own calls *and* for MapLibre Native's tile fetching — handed to
     * the map SDK by `FitMapApplication` via `HttpRequestUtil.setOkHttpClient`. Sharing one
     * instance is what makes the interceptor below a single place: a second client for the map
     * would be a second place the token could go missing from.
     *
     * The dispatcher mirrors what MapLibre's own default client configures (20 requests per
     * host, up from OkHttp's default 5) — a map fetches tiles in bursts from one origin, and
     * inheriting OkHttp's default here would throttle the map relative to the SDK's own
     * behaviour for no reason.
     */
    val client: OkHttpClient = OkHttpClient.Builder()
        .dispatcher(Dispatcher().apply { maxRequestsPerHost = 20 })
        .addInterceptor(BearerInterceptor)
        .build()

    /**
     * Attaches the session token to every request bound for FitMap's own API, and to nothing
     * else. Runs per request rather than being configured once, so a sign-in, sign-out or
     * expiry takes effect on the very next tile without anything being re-registered — the
     * property the roadmap picked this hook for.
     *
     * The origin check is the part worth keeping: the style document's basemap assets (the
     * pmtiles archive, glyphs, sprites) are unauthenticated and, in a CDN deployment, live on
     * a third party's origin. A bearer token is a live credential, and sending it to an origin
     * that has no use for it only puts it in someone else's access logs.
     */
    private object BearerInterceptor : Interceptor {
        override fun intercept(chain: Interceptor.Chain): Response {
            val request = chain.request()
            val token = Session.token
            if (token == null || !request.url.toString().startsWith(BuildConfig.API_BASE_URL)) {
                return chain.proceed(request)
            }
            return chain.proceed(request.newBuilder().header("Authorization", "Bearer $token").build())
        }
    }

    fun signIn(email: String, password: String, onResult: (Result<Account>) -> Unit) {
        authenticate("/auth/login", credentials(email, password), onResult)
    }

    fun signUp(email: String, password: String, onResult: (Result<Account>) -> Unit) {
        authenticate("/auth/signup", credentials(email, password), onResult)
    }

    /**
     * `docs/VISION.md` §8.2's no-signup account. A real user row with a real session behind the
     * scenes, so nothing downstream — tiles, sync, anything — needs a demo-specific branch.
     */
    fun startDemo(onResult: (Result<Account>) -> Unit) {
        authenticate("/auth/demo", EMPTY_BODY, onResult)
    }

    /**
     * Revokes the session server-side, not just locally: `POST /v1/auth/logout` deletes the
     * row, so a token copied off the device stops working immediately rather than staying
     * valid for the rest of its 30-day TTL.
     *
     * The local clear happens either way. A failed request here means the network was down or
     * the token was already dead — in neither case is keeping it on the device the right
     * answer, and a sign-out that visibly doesn't sign out is worse than one that can't reach
     * the server.
     */
    fun signOut(onDone: () -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/auth/logout")
            .post(EMPTY_BODY)
            .build()
        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) = finish()
            override fun onResponse(call: Call, response: Response) = response.use { finish() }
            private fun finish() {
                Session.clear()
                main.post(onDone)
            }
        })
    }

    /**
     * Checks a token restored from disk against the server — `GET /v1/auth/me`, the same
     * question the web client asks on first load. A 401 means the session is gone (revoked,
     * or expired past its TTL while the app was closed) and the token should be dropped; any
     * other failure is the network's fault and says nothing about the token.
     */
    fun verifySession(onResult: (Result<Unit>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/auth/me").build()
        call(request, { }, onResult)
    }

    /**
     * The bounding box containing every activity the account has, or null when it has none
     * with geometry yet.
     *
     * From `GET /v1/activities`'s per-row `bbox` rather than from anything the map itself
     * knows: a track outside the current viewport is in no loaded tile, so asking the renderer
     * where the user's history is would only ever answer for history already on screen. Rows
     * with a null bbox — an activity whose trajectory never made it in — are skipped rather
     * than treated as a point at (0, 0).
     */
    fun activityBounds(onResult: (Result<DoubleArray?>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/activities").build()
        call(request, ::parseBounds, onResult)
    }

    private fun parseBounds(body: String): DoubleArray? {
        val activities = JSONObject(body).getJSONArray("activities")
        var union: DoubleArray? = null
        for (i in 0 until activities.length()) {
            val box = activities.getJSONObject(i).optJSONArray("bbox") ?: continue
            if (box.length() != 4) continue
            val next = DoubleArray(4) { box.getDouble(it) }
            val current = union
            union = if (current == null) {
                next
            } else {
                doubleArrayOf(
                    minOf(current[0], next[0]),
                    minOf(current[1], next[1]),
                    maxOf(current[2], next[2]),
                    maxOf(current[3], next[3]),
                )
            }
        }
        return union
    }

    private fun credentials(email: String, password: String): RequestBody =
        JSONObject().put("email", email).put("password", password).toString().toRequestBody(JSON)

    private val EMPTY_BODY: RequestBody = ByteArray(0).toRequestBody(null, 0, 0)

    /**
     * The four endpoints that mint a session all answer with the same body, and all of them
     * carry `session_token` — the field a browser ignores (it already has the credential as a
     * `Set-Cookie`) and a native client exists to read.
     */
    private fun authenticate(path: String, body: RequestBody, onResult: (Result<Account>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + path).post(body).build()
        call(request, { text ->
            val json = JSONObject(text)
            val token = json.optString("session_token")
            if (token.isEmpty()) throw IOException("the server returned no session token")
            Account(token = token, email = json.optString("email"))
        }, onResult)
    }

    private fun <T> call(request: Request, parse: (String) -> T, onResult: (Result<T>) -> Unit) {
        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                deliver(Result.failure(e))
            }

            override fun onResponse(call: Call, response: Response) {
                val body = response.use { it.body?.string().orEmpty() }
                deliver(
                    if (response.isSuccessful) runCatching { parse(body) }
                    else Result.failure(ApiException(response.code, body.trim())),
                )
            }

            private fun deliver(result: Result<T>) {
                main.post { onResult(result) }
            }
        })
    }
}
