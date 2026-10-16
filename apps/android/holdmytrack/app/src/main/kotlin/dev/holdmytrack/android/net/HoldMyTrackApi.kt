package dev.holdmytrack.android.net

import android.content.ContentResolver
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import android.os.Handler
import android.os.LocaleList
import android.os.Looper
import dev.holdmytrack.android.BuildConfig
import dev.holdmytrack.android.map.GeoPoint
import dev.holdmytrack.android.map.SpotArea
import java.io.IOException
import java.time.ZoneId
import java.time.Instant
import java.time.OffsetDateTime
import java.util.concurrent.TimeUnit
import okhttp3.Call
import okhttp3.Callback
import okhttp3.Dispatcher
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okio.BufferedSink
import okio.source

/**
 * The account's latest copy of its data (root `docs/SPEC.md` FR-1.12), as
 * `GET /v1/account/export` describes it: [state] is "none", "preparing", "ready" or "failed";
 * [expiresAt] and [parts] only when ready.
 */
data class ExportStatus(
    val state: String,
    val expiresAt: java.time.Instant?,
    val totalSize: Long,
    val parts: List<ExportPart>,
)

/** One zip archive of an export: [url] is relative to the site, [name] what it saves as. */
data class ExportPart(val n: Int, val size: Long, val name: String, val url: String)

/**
 * The account as the server describes it — `GET /v1/auth/me`, and the same shape every
 * sign-in answers with and `PATCH /v1/account/settings` returns (`authResponse`,
 * `services/server/internal/httpapi/auth.go`). [email] is empty for a demo account, which the
 * server never names. [emailVerified] is false for a new email-and-password account (when the
 * server sends verification emails) and for a new Sign in with Facebook account, until the
 * emailed link is clicked; true for a demo account. [country] is empty until Settings is first
 * saved (the first-run gate); [locale] is empty for "automatic"; [avatarUrl] is an API path, or
 * empty with no avatar.
 */
data class Profile(
    val email: String,
    val isDemo: Boolean,
    val emailVerified: Boolean,
    val displayName: String,
    val country: String,
    val timezone: String,
    val locale: String,
    val avatarUrl: String,
)

/** An account with a live session — what a successful sign-in, sign-up or demo start yields. */
data class Account(val token: String, val profile: Profile)

/** One choice in a Settings list: what the server stores, and what to show for it. */
data class SettingsOption(val value: String, val label: String)

/** A region of the Timezone list ("Europe", named in the request's language). */
data class TimezoneGroup(val label: String, val options: List<SettingsOption>)

/** `GET /v1/account/settings/options` — the lists Settings chooses from, the server's own, so
 *  a choice is always one the save accepts. */
data class SettingsOptions(
    val countries: List<SettingsOption>,
    val timezones: List<TimezoneGroup>,
    val languages: List<SettingsOption>,
)

/**
 * The optional sign-in methods this deployment has configured (`GET /v1/auth/providers`).
 * [googleClientId] is the server's *web* client ID, which Credential Manager needs as its
 * `serverClientId` for the ID token it returns to be one the server accepts; empty when Google
 * is off.
 */
data class Providers(val google: Boolean, val facebook: Boolean, val googleClientId: String)

/**
 * What the server did with one activity in a sync batch. [status] is `"enqueued"`,
 * `"already_processed"` or `"rejected"`, and [error] carries the reason for the last of those —
 * an activity with no usable geometry, most often, which is a permanent verdict about that
 * record rather than a transient failure worth retrying.
 */
data class SyncResult(val externalId: String, val status: String, val error: String)

/**
 * What `POST /v1/sync/known` said: the ids the account has from that source, and, of those, the
 * ones still [processing] — sent, their ingest not finished, so not on the map yet.
 */
data class SyncKnown(val known: Set<String>, val processing: Set<String>)

/** What `POST /v1/activities/upload` said about one file: "enqueued" or "already_processed". */
data class UploadOutcome(val status: String)

/**
 * One row of `GET /v1/activities` (`docs/IMPLEMENTATION.md` §4.7) — what the map's Activities
 * panel lists, the web's `Activity`. [startedAt] is the server's RFC 3339 instant, shown in
 * [timezone], the zone it was recorded in (§4.30; empty from an older server). [name],
 * [description], [distanceMeters] and [durationSeconds] are null when the source never set
 * them; [bbox] (`[minLon, minLat, maxLon, maxLat]`) is null for an activity with no drawn
 * track. [pending] is a reprocess still running (a track edit, a Private location change):
 * the tracks tile leaves such a row out until it lands (`docs/SPEC.md` FR-5.15). [isPrivate] is an
 * activity entirely inside the account's Private locations (FR-5.1): kept, with no track, so its
 * zero distance and duration mean nothing.
 */
data class Activity(
    val id: String,
    val startedAt: String,
    val activityType: String,
    val timezone: String = "",
    val name: String?,
    val distanceMeters: Double?,
    val durationSeconds: Long?,
    val description: String?,
    val bbox: List<Double>?,
    val pending: Boolean,
    val edited: Boolean,
    val isPrivate: Boolean = false,
    /** The Stories it's in, newest first — what its row's Story badge names. */
    val stories: List<StoryRef> = emptyList(),
)

/** One of an activity's Stories, as a row of `GET /v1/activities` names it (`docs/SPEC.md`
 *  FR-5.1). */
data class StoryRef(val id: String, val name: String)

/** A copy of a Story someone sent the account, waiting to be accepted or declined
 *  (`docs/SPEC.md` FR-14.7). [from] is the sender's name, or their email address. */
data class StorySend(val id: String, val storyName: String, val from: String, val activityCount: Int)

/** A copy the account accepted that hasn't finished arriving. */
data class StoryCopying(val storyName: String, val from: String)

/** `GET /v1/story-sends`: the copies waiting, newest first, and the accepted ones arriving. */
data class StoryInbox(val sends: List<StorySend>, val copying: List<StoryCopying>)

/**
 * A Story's numbers, or one activity type's share of them (`docs/SPEC.md` FR-14.1): [count]
 * activities, their distance and moving time. [activityType] is empty for the whole Story.
 */
data class StoryTotals(
    val activityType: String,
    val count: Int,
    val distanceMeters: Double,
    val movingSeconds: Long,
)

/**
 * A Story — a hand-picked set of the account's activities (`docs/SPEC.md` FR-14.1), the web's
 * `Story`. [description] is empty when none is set; [from] names who sent the copy it came from
 * (FR-14.8), null for the account's own; [activityIds] is every member, earliest activity
 * first; [stats] covers the whole Story, whatever the date range, and [byType] the same per
 * activity type, the most frequent first.
 */
data class Story(
    val id: String,
    val name: String,
    val description: String,
    val from: String? = null,
    val activityIds: List<String>,
    val stats: StoryTotals,
    val byType: List<StoryTotals>,
)

/**
 * One vertex of an activity's display track, as `GET /v1/activities/track-metrics/{id}`
 * measures it: its speed from the vertex before, and its moment ([timeS], epoch seconds) —
 * what the Photos tab's slider turns a place on the track into (`docs/SPEC.md` FR-16.4).
 */
data class TrackMetricPoint(
    val lon: Double,
    val lat: Double,
    val speedMps: Double,
    val timeS: Long = 0,
)

/** What the selected activity's pace bands are drawn from. */
data class TrackMetrics(
    val activityId: String,
    val points: List<TrackMetricPoint>,
)

/** One recorded point of an activity, as `GET /v1/activities/track-points` returns it — its
 *  time ([t], Unix ms) is what a track edit names it by. */
data class TrackPoint(val lon: Double, val lat: Double, val t: Long)

/**
 * A track edit, as the server stores and replays it (`docs/IMPLEMENTATION.md` §4.7.7): a point
 * survives when it's inside [keep] (inclusive, null for no limit), outside every [remove]
 * range (inclusive), and not in [drop]; a survivor whose time is a key of [move] is drawn at
 * that lon/lat instead. All times are Unix ms.
 */
data class TrackEdit(
    val keep: Pair<Long, Long>? = null,
    val remove: List<Pair<Long, Long>> = emptyList(),
    val drop: List<Long> = emptyList(),
    val move: Map<Long, Pair<Double, Double>> = emptyMap(),
) {
    val isEmpty: Boolean
        get() = keep == null && remove.isEmpty() && drop.isEmpty() && move.isEmpty()
}

/** An activity's recorded points and the edit already applied to them, if any. */
data class TrackPoints(val points: List<TrackPoint>, val edit: TrackEdit?)

/**
 * One photo on an activity (`docs/SPEC.md` FR-16.3), always with a place on its track:
 * [routeAt], a moment on it. [lon]/[lat] are that moment's point on the track as drawn, null
 * only for an activity with no track left at all. [url] and [thumbUrl] are API paths
 * (`/v1/photos/…`); [width]/[height] are the stored copy's. [timezone] is its activity's, which
 * its times are shown in.
 */
data class Photo(
    val id: String,
    val activityId: String,
    val takenAt: Instant?,
    val routeAt: Instant,
    val lon: Double?,
    val lat: Double?,
    val caption: String?,
    val width: Int,
    val height: Int,
    val url: String,
    val thumbUrl: String,
    val timezone: String = "",
)

/**
 * What a photo's EXIF said about when and where it was taken, as `POST /v1/photos` and its
 * placement check take it (FR-16.1): [takenAt] an instant, or [takenLocal] a camera clock with
 * no zone (`YYYY-MM-DDTHH:MM:SS`) — at most one is set — and a position.
 */
data class PhotoExif(
    val takenAt: Instant? = null,
    val takenLocal: String? = null,
    val lat: Double? = null,
    val lon: Double? = null,
)

/** A photo's image, resized for upload: its bytes and their type (`image/webp`, `image/jpeg`). */
class PhotoImage(val bytes: ByteArray, val contentType: String)

/** The server couldn't tell where on the track a photo was taken (`photo_needs_place`,
 *  FR-16.1): nothing was stored, and it goes again with the user's `route_at`. */
class PhotoNeedsPlaceException(message: String) : IOException(message)

/**
 * One of the account's Private locations (`docs/SPEC.md` FR-8.1): a circle whose contents are
 * clipped off the ends of every track. [name] is empty when unset; [radiusM] is between
 * [PrivateLocation.MIN_RADIUS_M] and [PrivateLocation.MAX_RADIUS_M].
 */
data class PrivateLocation(val id: String, val name: String, val lon: Double, val lat: Double, val radiusM: Int) {
    companion object {
        /** The server's own bounds (`private_locations.go`). */
        const val MIN_RADIUS_M = 50
        const val MAX_RADIUS_M = 2000
    }
}

/** `GET /v1/coverage/status`: a Fog/Heatmap re-render still to come, the account's map version
 *  (it moves whenever any of its tiles may have), and that version as the `cv` tile URLs carry
 *  ([Session.tileVersion]). */
data class CoverageStatus(val rendering: Boolean, val version: Long, val tileVersion: String)

/**
 * One Spots place (`docs/SPEC.md` FR-15), as a tile's `spots` feature or `GET /v1/spots` carries
 * it: [category] is the wire name (`playground`, `dog_park`, `monument`, `viewpoint`,
 * `history`), [lon]/[lat] its anchor, and each text null when OSM has none. [wikipedia] is OSM's
 * tag in its "lang:Article title" form, the only one the server keeps.
 */
data class Spot(
    val id: Long,
    val category: String,
    val name: String?,
    val address: String?,
    val description: String?,
    val inscription: String?,
    val memorial: String?,
    val startDate: String?,
    val wikipedia: String?,
    val lon: Double,
    val lat: Double,
)

/** `GET /v1/spots`: the places in a box, up to the server's cap, and how many there are in all. */
data class SpotsInArea(val spots: List<Spot>, val total: Int)

/** `GET /v1/spots/{id}` (`docs/SPEC.md` FR-15.6): the place, its whole [area] — what capture
 *  mode measures against — and when the account captured it, or null. */
data class SpotDetail(val spot: Spot, val area: SpotArea, val capturedAt: Instant?)

/** One place the account has captured (`GET /v1/spots/captures`, `POST /v1/spots/{id}/captures`). */
data class SpotCapture(val spotId: Long, val capturedAt: Instant)

/** One day that has activity, as `GET /v1/activities/histogram` counts it; [date] is `YYYY-MM-DD`
 *  in the account's timezone. Days with none are never returned. */
data class ActivityDay(val date: String, val count: Int, val distanceMeters: Double)

/**
 * A page of [ActivityDay]s, ascending, and the account's first activity day — how far back
 * paging can go, independent of this page's own bounds; null for an account with no activity.
 */
data class ActivityDayPage(val days: List<ActivityDay>, val earliest: String?)

/**
 * A request the server answered with a non-2xx status. Every API error is
 * `{"error": code, "message": text}` (`docs/adr/0045-one-json-error-shape.md`): `message` is
 * the server's own wording, shown to the user as-is rather than replaced with something vaguer —
 * "invalid email or password" and "an account with this email already exists" are exactly what
 * someone needs to read — and [errorCode] is what a caller branches on, never the message.
 */
class ApiException(val code: Int, message: String, val errorCode: String? = null) :
    IOException(message.ifBlank { "request failed ($code)" }) {
    companion object {
        /**
         * The refusal in a non-2xx [body]: the API's `{error, message}`, its message shown and
         * its code kept, or — from something in front of the server, a proxy's error page — the
         * text as it is, with no code.
         */
        fun from(code: Int, body: String): ApiException {
            val text = body.trim()
            val json = if (text.startsWith("{")) runCatching { apiJson.decodeFromString<ErrorJson>(text) }.getOrNull() else null
            if (json != null && (json.error != null || json.message.isNotEmpty())) {
                return ApiException(code, json.message, json.error?.takeIf { it.isNotEmpty() })
            }
            return ApiException(code, text)
        }
    }
}

/**
 * The app's whole HTTP surface: the auth endpoints, sync and its history, and the activity
 * reads the map and the recording screen use.
 *
 * Deliberately callback-based over OkHttp's own `enqueue` rather than coroutine-based. There
 * are five calls in the entire app and OkHttp is already a dependency (MapLibre Native pulls
 * it in), so a coroutines runtime plus the lifecycle-scope artifacts would be two new
 * dependencies bought for five call sites. Every callback is posted back to the main thread,
 * so callers touch views directly without re-dispatching.
 */
object HoldMyTrackApi {

    /** [ApiException.errorCode] of a split that would leave a part entirely inside a Private
     *  location (`docs/SPEC.md` FR-5.17 behavior 7) — asked about, then sent with allowHidden. */
    const val SPLIT_HIDES_PART = "split_hides_part"

    private const val API_V1 = "/v1"

    /** The `source` values this app posts to `POST /v1/sync/activities` — Health Connect
     *  sync (`sync/SyncRunner.kt`), in-app GPS recording (`recording/RecordingActivity.kt`,
     *  `docs/adr/0007-in-app-gps-recording-submits-directly.md`). iOS's own is `"healthkit"`; an
     *  uploaded file has its own endpoint ([uploadActivityFile]). */
    const val SOURCE_HEALTH_CONNECT = "healthconnect"
    const val SOURCE_RECORDED = "recorded"
    private val JSON = "application/json; charset=utf-8".toMediaType()
    private val main = Handler(Looper.getMainLooper())

    /**
     * One client for the app's own calls *and* for MapLibre Native's tile fetching — handed to
     * the map SDK by `HoldMyTrackApplication` via `HttpRequestUtil.setOkHttpClient`. Sharing one
     * instance is what makes the interceptor below a single place: a second client for the map
     * would be a second place the token could go missing from.
     *
     * The dispatcher mirrors what MapLibre's own default client configures (20 requests per
     * host, up from OkHttp's default 5) — a map fetches tiles in bursts from one origin, and
     * inheriting OkHttp's default here would throttle the map relative to the SDK's own
     * behaviour for no reason.
     */
    /** This build: the release number and the commit, "0.14 (62da50b)" — what the You tab shows
     *  and the User-Agent carries. */
    val appVersion: String = "${BuildConfig.VERSION_NAME} (${BuildConfig.GIT_SHA})"

    val client: OkHttpClient = OkHttpClient.Builder()
        .dispatcher(Dispatcher().apply { maxRequestsPerHost = 20 })
        .addInterceptor(BearerInterceptor)
        .addInterceptor(UserAgentInterceptor)
        .addInterceptor(LanguageInterceptor)
        .addInterceptor(OriginInterceptor)
        .addInterceptor(OutdatedInterceptor)
        .build()

    /**
     * Sends `Origin: <the API's origin>` on every request that leaves it, as the web client's
     * browser does on its cross-origin fetches. The satellite imagery's key (root
     * `docs/DEPLOY.md` §5) is restricted to the deployment's origin, and without an Origin
     * MapTiler refuses the app's tiles with a `403`. It gives nothing away: the key is already
     * public in the web bundle and the style document, and the restriction is there to stop other
     * websites spending the quota, which a native client setting a header doesn't. A request that
     * already carries an Origin is left as it is.
     */
    private object OriginInterceptor : Interceptor {
        private val origin = BuildConfig.API_BASE_URL.toHttpUrl().let { url ->
            val port = if (url.port == HttpUrl.defaultPort(url.scheme)) "" else ":${url.port}"
            "${url.scheme}://${url.host}$port"
        }

        override fun intercept(chain: Interceptor.Chain): Response {
            val request = chain.request()
            if (request.url.toString().startsWith(BuildConfig.API_BASE_URL) || request.header("Origin") != null) {
                return chain.proceed(request)
            }
            return chain.proceed(request.newBuilder().header("Origin", origin).build())
        }
    }

    /**
     * Names the app and its build — `HoldMyTrack-Android/0.14 (62da50b; 812)` — on HoldMyTrack's
     * own requests, so the server's logs say which build made a call and the server can turn
     * away one older than its minimum. Other origins keep what the request already had:
     * MapLibre's own agent on the basemap's assets.
     */
    private object UserAgentInterceptor : Interceptor {
        private val agent = AppBuild.userAgent(BuildConfig.VERSION_NAME, BuildConfig.GIT_SHA, BuildConfig.VERSION_CODE)

        override fun intercept(chain: Interceptor.Chain): Response {
            val request = chain.request()
            if (!request.url.toString().startsWith(BuildConfig.API_BASE_URL)) return chain.proceed(request)
            return chain.proceed(request.newBuilder().header("User-Agent", agent).build())
        }
    }

    /**
     * Whether [response] is the server refusing this build, which [OutdatedInterceptor] has
     * already handed to the Update screen. A callback caller isn't told: the screen that made
     * the call may be gone by the time it would run, and nothing it could show is still seen.
     * A suspending caller still gets its exception, and its lifecycle's scope is cancelled with
     * the screen.
     */
    private fun refusedBuild(response: Response): Boolean =
        AppBuild.isRefusal(response.request.url.toString(), BuildConfig.API_BASE_URL, response.code)

    /** Hands the server's refusal of this build to [AppOutdated], whichever call met it. */
    private object OutdatedInterceptor : Interceptor {
        override fun intercept(chain: Interceptor.Chain): Response {
            val response = chain.proceed(chain.request())
            if (AppBuild.isRefusal(response.request.url.toString(), BuildConfig.API_BASE_URL, response.code)) {
                AppOutdated.refused()
            }
            return response
        }
    }

    /**
     * Sends the app's language as Accept-Language on HoldMyTrack's own requests, so the
     * server's messages (a failed sign-in, a rejected activity) come back in it — the same
     * language the rest of the screen is in. Read per request: the per-app language can change
     * while the app runs. OkHttp sends no Accept-Language of its own, so without this the
     * server would always answer in English.
     */
    private object LanguageInterceptor : Interceptor {
        override fun intercept(chain: Interceptor.Chain): Response {
            val request = chain.request()
            if (!request.url.toString().startsWith(BuildConfig.API_BASE_URL)) return chain.proceed(request)
            val tags = LocaleList.getDefault().toLanguageTags()
            if (tags.isEmpty()) return chain.proceed(request)
            return chain.proceed(request.newBuilder().header("Accept-Language", tags).build())
        }
    }

    /**
     * Attaches the session token to every request bound for HoldMyTrack's own API, and to nothing
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

    /**
     * `POST /v1/sync/activities` — Path 2's batched sync (`docs/IMPLEMENTATION.md` §4.0.3).
     *
     * Suspending rather than callback-based like everything above it, because its only caller
     * is the sync run, which is a coroutine from end to end (Health Connect's read API leaves
     * no choice). The blocking `execute()` is confined to `Dispatchers.IO` here rather than
     * wrapped in `enqueue`, since the caller genuinely wants to wait: the next batch must not
     * be sent until this one is answered.
     *
     * Returns the server's per-activity verdicts rather than a single pass/fail — a batch is
     * not all-or-nothing there, and the caller needs to know which ones landed before it takes
     * them off its list. A non-2xx status is the whole request failing and throws instead;
     * nothing in the batch was decided.
     */
    internal suspend fun syncActivities(activities: List<SyncActivityJson>, source: String): List<SyncResult> =
        withContext(Dispatchers.IO) {
            val request = Request.Builder()
                .url(BuildConfig.API_BASE_URL + API_V1 + "/sync/activities")
                .post(jsonBody(SyncActivitiesRequestJson(source, activities)))
                .build()
            val response = client.newCall(request).execute()
            val text = response.use { it.body?.string().orEmpty() }
            if (!response.isSuccessful) throw ApiException.from(response.code, text)
            decode<SyncResultsJson>(text).results.map { SyncResult(it.externalId, it.status, it.error) }
        }

    /**
     * `POST /v1/sync/known` (`docs/IMPLEMENTATION.md` §4.0.3): which of [externalIds] the
     * account already has from [source] — the Sync tab's list is what's on the phone minus these,
     * so a reinstall or a second phone agrees with the server — and, of those, which are still
     * processing: sent, not on the map yet. Suspending, like [syncActivities], for the same
     * caller; a failure throws.
     */
    suspend fun syncKnown(source: String, externalIds: List<String>): SyncKnown =
        withContext(Dispatchers.IO) {
            val known = mutableSetOf<String>()
            val processing = mutableSetOf<String>()
            for (chunk in externalIds.chunked(MAX_KNOWN_IDS)) {
                val request = Request.Builder()
                    .url(BuildConfig.API_BASE_URL + API_V1 + "/sync/known")
                    .post(jsonBody(SyncKnownRequestJson(source, chunk)))
                    .build()
                val response = client.newCall(request).execute()
                val text = response.use { it.body?.string().orEmpty() }
                if (!response.isSuccessful) throw ApiException.from(response.code, text)
                val json = decode<SyncKnownJson>(text)
                known += json.known
                processing += json.processing
            }
            SyncKnown(known, processing)
        }

    /**
     * `POST /v1/activities/overlaps` (root `docs/IMPLEMENTATION.md` §4.6): for each of [spans],
     * keyed by the caller, the live activity it overlaps, if any — the Already on your map half
     * of the Sync tab's similar tracks (`docs/SPEC.md` FR-3.7). Keys with no overlap aren't in
     * the map.
     */
    suspend fun activityOverlaps(spans: List<OverlapSpan>): Map<String, ActivityOverlap> =
        withContext(Dispatchers.IO) {
            val found = mutableMapOf<String, ActivityOverlap>()
            for (chunk in spans.chunked(MAX_KNOWN_IDS)) {
                val spansJson = chunk.map { OverlapSpanJson(it.key, it.start.toString(), it.end.toString()) }
                val request = Request.Builder()
                    .url(BuildConfig.API_BASE_URL + API_V1 + "/activities/overlaps")
                    .post(jsonBody(OverlapsRequestJson(spansJson)))
                    .build()
                val response = client.newCall(request).execute()
                val text = response.use { it.body?.string().orEmpty() }
                if (!response.isSuccessful) throw ApiException.from(response.code, text)
                for (row in decode<OverlapsJson>(text).overlaps) {
                    found[row.key] = ActivityOverlap(row.activityId, row.name, row.activityType, row.startedAt, row.timezone)
                }
            }
            found
        }

    /** The server's `maxKnownIDs`: one `POST /v1/sync/known` asks about at most this many. */
    private const val MAX_KNOWN_IDS = 5000

    /**
     * Uploads take as long as sending the file does, up to 16 MiB, which over a slow connection
     * takes longer than the default ten seconds.
     * The same client otherwise, so the same interceptors.
     */
    private val uploadClient: OkHttpClient by lazy {
        client.newBuilder()
            .writeTimeout(2, TimeUnit.MINUTES)
            .readTimeout(10, TimeUnit.MINUTES)
            .build()
    }

    /**
     * `POST /v1/activities/upload` — the web's Upload menu's request (root `docs/SPEC.md`
     * FR-3.1): one `.gpx`, `.fit` or `.tcx`. The file is streamed from [uri] rather than read into
     * memory first, since it can be 16 MiB; [onProgress] gets the bytes sent so far, on the
     * I/O thread sending them. Suspending, like [syncActivities]: its caller (`imports/FileImports.kt`)
     * sends one file after another and waits for each.
     */
    suspend fun uploadActivityFile(
        resolver: ContentResolver,
        uri: Uri,
        name: String,
        size: Long,
        onProgress: (Long) -> Unit,
    ): UploadOutcome = withContext(Dispatchers.IO) {
        val body = MultipartBody.Builder()
            .setType(MultipartBody.FORM)
            .addFormDataPart("file", name, UriRequestBody(resolver, uri, size, onProgress))
            .build()
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/activities/upload")
            .post(body)
            .build()
        val response = uploadClient.newCall(request).execute()
        val text = response.use { it.body?.string().orEmpty() }
        if (!response.isSuccessful) throw ApiException.from(response.code, text)
        UploadOutcome(status = decode<UploadResultJson>(text).status)
    }

    /** A picked file as a request body, read as it's sent. Opened afresh on every [writeTo], so
     *  OkHttp's retry of a dropped connection sends the whole file again. */
    private class UriRequestBody(
        private val resolver: ContentResolver,
        private val uri: Uri,
        private val size: Long,
        private val onProgress: (Long) -> Unit,
    ) : RequestBody() {
        override fun contentType() = "application/octet-stream".toMediaType()

        override fun contentLength() = size

        override fun writeTo(sink: BufferedSink) {
            val input = resolver.openInputStream(uri) ?: throw IOException("could not open the file")
            input.source().use { source ->
                var sent = 0L
                while (true) {
                    val read = source.read(sink.buffer, 64 * 1024L)
                    if (read == -1L) break
                    sink.emitCompleteSegments()
                    sent += read
                    onProgress(sent)
                }
            }
        }
    }

    /** `GET /v1/auth/providers` — which of the Google and Facebook buttons to show. */
    fun providers(onResult: (Result<Providers>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/auth/providers").build()
        call(request, { body ->
            val json = decode<ProvidersJson>(body)
            Providers(google = json.google, facebook = json.facebook, googleClientId = json.googleClientId)
        }, onResult)
    }

    /**
     * `POST /v1/auth/google/token` — trades the ID token Credential Manager returned for a
     * session. The server checks the token's signature against Google's keys before trusting
     * anything in it (`docs/IMPLEMENTATION.md` §4.15).
     */
    fun signInWithGoogle(idToken: String, timezone: String, onResult: (Result<Account>) -> Unit) {
        authenticate("/auth/google/token", jsonBody(GoogleTokenJson(idToken, timezone)), onResult)
    }

    /**
     * Where Sign in with Facebook starts: the server's own web flow, opened in a browser tab,
     * with the S256 [challenge] of a verifier only this app holds. The round trip ends at
     * `holdmytrack://oauth?code=…`, redeemed with [exchangeHandoff].
     */
    fun facebookStartUri(timezone: String, challenge: String): Uri =
        Uri.parse(BuildConfig.API_BASE_URL + API_V1 + "/auth/facebook/start").buildUpon()
            .appendQueryParameter("tz", timezone)
            .appendQueryParameter("app_challenge", challenge)
            .build()

    /** One of the web's own pages — "Forgot password?", About, Help — served by the API's own
     *  origin, outside `/v1`. */
    fun webPageUri(path: String): Uri = Uri.parse(BuildConfig.API_BASE_URL + path)

    /** `POST /v1/auth/handoff` — redeems a browser-tab round trip's one-time code, once. */
    fun exchangeHandoff(code: String, verifier: String, onResult: (Result<Account>) -> Unit) {
        authenticate("/auth/handoff", jsonBody(HandoffJson(code, verifier)), onResult)
    }

    fun signIn(email: String, password: String, onResult: (Result<Account>) -> Unit) {
        authenticate("/auth/login", jsonBody(CredentialsJson(email, password)), onResult)
    }

    /** Sends the phone's IANA zone as `timezone`, as the web's sign-up sends the browser's, so
     *  a new account starts on it rather than on UTC (Google and Facebook already send it). */
    fun signUp(email: String, password: String, onResult: (Result<Account>) -> Unit) {
        authenticate("/auth/signup", jsonBody(CredentialsJson(email, password, ZoneId.systemDefault().id)), onResult)
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
     *
     * Answers with the account's `email_verified`, since a live token is not by itself enough
     * for the map: an unconfirmed email gets `403` on every tile (`VerifyEmailActivity`).
     */
    fun verifySession(onResult: (Result<Profile>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/auth/me").build()
        call(request, { body -> decode<AccountJson>(body).toProfile() }, onResult)
    }

    /** `GET /v1/account/settings/options` — see [SettingsOptions]. */
    fun settingsOptions(onResult: (Result<SettingsOptions>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/account/settings/options").build()
        call(request, { body ->
            val json = decode<SettingsOptionsJson>(body)
            fun options(list: List<SettingsOptionJson>) = list.map { SettingsOption(it.value, it.label) }
            SettingsOptions(
                countries = options(json.countries),
                timezones = json.timezones.map { TimezoneGroup(it.label.ifBlank { it.region }, options(it.options)) },
                languages = options(json.languages),
            )
        }, onResult)
    }

    /**
     * `PATCH /v1/account/settings` — Name, Country, Timezone and Language as one save, as the
     * web page saves them. [locale] is empty for "automatic". Answers with the saved profile;
     * a refusal (a missing country, a demo account) is the server's own wording.
     */
    fun updateSettings(
        displayName: String,
        country: String,
        timezone: String,
        locale: String,
        onResult: (Result<Profile>) -> Unit,
    ) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/account/settings")
            .patch(jsonBody(SettingsJson(displayName, country, timezone, locale)))
            .build()
        call(request, { text -> decode<AccountJson>(text).toProfile() }, onResult)
    }

    /**
     * `DELETE /v1/account` (root `docs/SPEC.md` FR-1.11) — [email] typed as confirmation. The
     * server closes the account and ends every session of it; a mismatch is its own wording.
     * The caller clears what this device holds for the account ([Session.clear] and the rest).
     */
    fun deleteAccount(email: String, onResult: (Result<Unit>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/account")
            .delete(jsonBody(EmailJson(email)))
            .build()
        call(request, { }, onResult)
    }

    /** `GET /v1/account/export` — where the account's latest copy of its data stands. */
    fun exportStatus(onResult: (Result<ExportStatus>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/account/export").build()
        call(request, { body -> decode<ExportStatusJson>(body).toExportStatus() }, onResult)
    }

    /** `POST /v1/account/export` — asks for a copy; one already being prepared is returned
     *  instead. The server emails the account when it's ready, in the app's language. */
    fun requestExport(onResult: (Result<ExportStatus>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/account/export")
            .post(EMPTY_BODY)
            .build()
        call(request, { body -> decode<ExportStatusJson>(body).toExportStatus() }, onResult)
    }

    /** An export part's download URL, absolute — for Android's DownloadManager. */
    fun exportPartUri(part: ExportPart): Uri = Uri.parse(BuildConfig.API_BASE_URL + part.url)

    /** The avatar at [path] (a [Profile.avatarUrl], relative to the API) — see [image]. */
    fun avatar(path: String, onResult: (Result<Bitmap>) -> Unit) = image(path, maxSide = null, onResult)

    /**
     * The image at [path] (relative to the API: an avatar, a [Photo.url] or [Photo.thumbUrl]),
     * fetched with the session's bearer token like any other API request — a browser tab or an
     * image library would have no token to send — and decoded, off the main thread. With
     * [maxSide], decoded at the largest power-of-two reduction still at least that many pixels on
     * its long side, so a 2048 px photo shown at a popup's width isn't held at full size.
     */
    fun image(path: String, maxSide: Int?, onResult: (Result<Bitmap>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + path).build()
        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                main.post { onResult(Result.failure(e)) }
            }

            override fun onResponse(call: Call, response: Response) {
                if (refusedBuild(response)) return response.close()
                val result = response.use {
                    if (!it.isSuccessful) return@use Result.failure(ApiException(it.code, ""))
                    val bytes = it.body?.bytes() ?: ByteArray(0)
                    val options = BitmapFactory.Options()
                    if (maxSide != null) {
                        options.inJustDecodeBounds = true
                        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, options)
                        val longSide = maxOf(options.outWidth, options.outHeight)
                        var sample = 1
                        while (longSide / (sample * 2) >= maxSide) sample *= 2
                        options.inJustDecodeBounds = false
                        options.inSampleSize = sample
                    }
                    BitmapFactory.decodeByteArray(bytes, 0, bytes.size, options)?.let { bitmap -> Result.success(bitmap) }
                        ?: Result.failure(IOException("not an image"))
                }
                main.post { onResult(result) }
            }
        })
    }

    /**
     * `POST /v1/auth/resend-verification` — mails a fresh confirmation link. Answers with the
     * server's own confirmation text ("Verification email sent."), in the app's language.
     */
    fun resendVerification(onResult: (Result<String>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/auth/resend-verification")
            .post(EMPTY_BODY)
            .build()
        call(request, { body -> decode<MessageJson>(body).message }, onResult)
    }

    /**
     * The bounding box of the account's most recent drawn activity, or null when it has none
     * with geometry yet — what the map opens on, as the web's does (`docs/SPEC.md` FR-4.5): an
     * account with scattered recent history would otherwise open on a near-world view.
     *
     * From `GET /v1/activities`'s per-row `bbox` rather than from anything the map itself
     * knows: a track outside the current viewport is in no loaded tile, so asking the renderer
     * where the user's history is would only ever answer for history already on screen. Rows
     * with a null bbox — an activity whose trajectory never made it in — are skipped rather
     * than treated as a point at (0, 0), and so are Pending ones, which the tracks tile doesn't
     * draw.
     */
    fun latestActivityBounds(onResult: (Result<DoubleArray?>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/activities").build()
        call(request, { body ->
            var latest: Instant? = null
            var box: DoubleArray? = null
            forEachDrawn(body) { row, bbox ->
                val startedAt = runCatching { OffsetDateTime.parse(row.startedAt).toInstant() }.getOrNull()
                if (startedAt != null && (latest == null || startedAt > latest)) {
                    latest = startedAt
                    box = bbox
                }
            }
            box
        }, onResult)
    }

    /**
     * `GET /v1/activities?from=&to=` — every activity from [from] to [to] (`YYYY-MM-DD`,
     * inclusive, read by the server as days in the account's timezone), newest first, in one
     * response: the list isn't paged (`docs/IMPLEMENTATION.md` §4.7). What the map's
     * Activities panel lists. With [story] instead, that whole Story's (`docs/SPEC.md`
     * FR-14.4, FR-14.6: an open Story ignores the date range).
     */
    fun activities(from: String?, to: String?, story: String?, onResult: (Result<List<Activity>>) -> Unit) {
        val url = (BuildConfig.API_BASE_URL + API_V1 + "/activities").toHttpUrl().newBuilder()
            .apply { if (from != null) addQueryParameter("from", from) }
            .apply { if (to != null) addQueryParameter("to", to) }
            .apply { if (story != null) addQueryParameter("story", story) }
            .build()
        call(Request.Builder().url(url).build(), { body -> decode<ActivitiesJson>(body).activities.map { it.toActivity() } }, onResult)
    }

    /**
     * `PATCH /v1/activities/{id}` — a full replace of the three fields a person can set
     * (`docs/IMPLEMENTATION.md` §4.7.4): an empty [name] or [description] clears it. Answers
     * with the row as it now is. A refusal (too long, a demo account) is the server's own
     * wording, in the app's language.
     */
    fun updateActivity(id: String, activityType: String, name: String, description: String, onResult: (Result<Activity>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/activities/" + id)
            .patch(jsonBody(ActivityPatchJson(activityType, name, description)))
            .build()
        call(request, { text -> decode<ActivityRowJson>(text).toActivity() }, onResult)
    }

    /** `DELETE /v1/activities/{id}` (`docs/IMPLEMENTATION.md` §4.7.5) — the activity, its
     *  track and its share of fog and heatmap coverage, for good. */
    fun deleteActivity(id: String, onResult: (Result<Unit>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/activities/" + id).delete().build()
        call(request, { }, onResult)
    }

    /**
     * `GET /v1/coverage/status` — whether the account still has a job that changes its Fog and
     * Heatmap tiles, and its map version. What the map polls after a delete or a reprocess, to
     * know when to fetch those tiles again (`map/CoverageWatch`).
     */
    fun coverageStatus(onResult: (Result<CoverageStatus>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/coverage/status").build()
        call(request, { text ->
            val json = decode<CoverageStatusJson>(text)
            CoverageStatus(json.rendering, json.version, json.tileVersion)
        }, onResult)
    }

    /** `GET /v1/spots` (`docs/IMPLEMENTATION.md` §4.25) — [categories]' places whose anchor is
     *  inside the box, named first, up to the server's 2,000. */
    fun spotsInArea(
        west: Double, south: Double, east: Double, north: Double,
        categories: List<String>,
        onResult: (Result<SpotsInArea>) -> Unit,
    ) {
        val url = (BuildConfig.API_BASE_URL + API_V1 + "/spots").toHttpUrl().newBuilder()
            .addQueryParameter("bbox", listOf(west, south, east, north).joinToString(",") { "%.5f".format(java.util.Locale.ROOT, it) })
            .addQueryParameter("categories", categories.joinToString(","))
            .build()
        call(Request.Builder().url(url).build(), { text ->
            val json = decode<SpotsInAreaJson>(text)
            SpotsInArea(json.spots.map { it.toSpot() }, json.total)
        }, onResult)
    }

    /** `GET /v1/spots/{id}` (`docs/IMPLEMENTATION.md` §4.25) — see [SpotDetail]. */
    fun spotDetail(id: Long, onResult: (Result<SpotDetail>) -> Unit) {
        call(Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/spots/" + id).build(), { text ->
            val json = decode<SpotDetailJson>(text)
            SpotDetail(
                spot = Spot(json.id, json.category, json.name, json.address, json.description, json.inscription, json.memorial, json.startDate, json.wikipedia, json.lon, json.lat),
                area = json.area.coordinates.map { polygon -> polygon.map { ring -> ring.map { GeoPoint(lat = it[1], lon = it[0]) } } },
                capturedAt = json.capturedAt?.let(::parseInstant),
            )
        }, onResult)
    }

    /** `GET /v1/spots/captures` — every place the account has captured, newest first; with
     *  [since], only those captured at or after it. */
    fun spotCaptures(since: Instant?, onResult: (Result<List<SpotCapture>>) -> Unit) {
        val url = (BuildConfig.API_BASE_URL + API_V1 + "/spots/captures").toHttpUrl().newBuilder()
        if (since != null) url.addQueryParameter("since", since.toString())
        call(Request.Builder().url(url.build()).build(), { text -> decode<SpotCapturesJson>(text).captures.map { it.toSpotCapture() } }, onResult)
    }

    /** `POST /v1/spots/{id}/captures` — captures the place at the position last measured inside
     *  it; the server checks it against the area. A place already captured answers the first
     *  capture. Outside the area is an [ApiException] with code 422. */
    fun captureSpot(id: Long, lat: Double, lon: Double, onResult: (Result<SpotCapture>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/spots/" + id + "/captures")
            .post(jsonBody(SpotCaptureRequestJson(lat, lon)))
            .build()
        call(request, { text -> decode<SpotCaptureJson>(text).toSpotCapture() }, onResult)
    }

    private fun SpotCaptureJson.toSpotCapture() = SpotCapture(spotId, parseInstant(capturedAt))

    private fun parseInstant(text: String): Instant = OffsetDateTime.parse(text).toInstant()

    private fun SpotJson.toSpot() = Spot(id, category, name, address, description, inscription, memorial, startDate, wikipedia, lon, lat)

    /** `GET /v1/activities/track-metrics/{id}` (`docs/IMPLEMENTATION.md` §4.3.1) — see
     *  [TrackMetrics]. */
    fun trackMetrics(id: String, onResult: (Result<TrackMetrics>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/activities/track-metrics/" + id).build()
        call(request, { text ->
            val json = decode<TrackMetricsJson>(text)
            TrackMetrics(json.activityId, json.points.map { TrackMetricPoint(it.lon, it.lat, it.speedMps, it.timeS) })
        }, onResult)
    }

    /** `GET /v1/activities/track-points/{id}` — the recorded points, unedited and with any
     *  Private location already clipped off, and the saved edit. A refusal (`409`: no
     *  recording kept, points without times) is the server's own wording. */
    fun trackPoints(id: String, onResult: (Result<TrackPoints>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/activities/track-points/" + id).build()
        call(request, { text ->
            val json = decode<TrackPointsJson>(text)
            TrackPoints(json.points.map { TrackPoint(it[0], it[1], it[2].toLong()) }, json.edit?.toTrackEdit())
        }, onResult)
    }

    /**
     * `POST /v1/activities/track-edit/{id}` — the whole new edit, or null (or an empty one) to
     * go back to the track as recorded, and [splitAt], a point's time, to also split the activity
     * there (`docs/SPEC.md` FR-5.17). The server answers `202` and reprocesses it in the
     * background, the activity Pending meanwhile; `409` when a reprocess is already running; a
     * `422` [SPLIT_HIDES_PART] for a split that would leave a part inside a Private location,
     * which goes ahead when sent again with [allowHidden].
     */
    fun trackEdit(id: String, edit: TrackEdit?, splitAt: Long? = null, allowHidden: Boolean = false, onResult: (Result<Unit>) -> Unit) {
        val body = TrackEditRequestJson(edit?.toJson(), splitAt, allowHidden = splitAt != null && allowHidden)
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/activities/track-edit/" + id)
            .post(jsonBody(body))
            .build()
        call(request, { }, onResult)
    }

    private fun TrackEditJson.toTrackEdit() = TrackEdit(
        keep = keep?.let { it[0] to it[1] },
        remove = remove.orEmpty().map { it[0] to it[1] },
        drop = drop.orEmpty(),
        // Keyed by time, which JSON can only carry as an object key.
        move = move.orEmpty().entries.associate { (t, at) -> t.toLong() to (at[0] to at[1]) },
    )

    private fun TrackEdit.toJson() = TrackEditJson(
        keep = keep?.let { listOf(it.first, it.second) },
        remove = remove.map { listOf(it.first, it.second) }.ifEmpty { null },
        drop = drop.ifEmpty { null },
        move = move.entries.associate { (t, at) -> t.toString() to listOf(at.first, at.second) }.ifEmpty { null },
    )

    /**
     * `GET /v1/photos?activity=` or `?story=` (`docs/SPEC.md` FR-16.3) — one activity's photos,
     * or every photo of a Story's activities, in route order. Exactly one of the two is given.
     */
    fun photos(activity: String?, story: String?, onResult: (Result<List<Photo>>) -> Unit) {
        val url = (BuildConfig.API_BASE_URL + API_V1 + "/photos").toHttpUrl().newBuilder()
            .apply { if (activity != null) addQueryParameter("activity", activity) }
            .apply { if (story != null) addQueryParameter("story", story) }
            .build()
        call(Request.Builder().url(url).build(), { text -> decode<PhotosJson>(text).photos.map { it.toPhoto() } }, onResult)
    }

    /**
     * `POST /v1/photos/place` (FR-16.1) — where an upload of a photo with this EXIF would be
     * placed on [activityId]'s track, storing nothing: its `route_at`. A
     * [PhotoNeedsPlaceException] when the server can't tell, for the user to choose.
     */
    fun checkPhotoPlace(activityId: String, exif: PhotoExif, onResult: (Result<Instant>) -> Unit) {
        val placed = exif.lat != null && exif.lon != null
        val body = PhotoPlaceRequestJson(
            activityId = activityId,
            takenAt = exif.takenAt?.toString(),
            takenLocal = exif.takenLocal.takeIf { exif.takenAt == null },
            lat = exif.lat.takeIf { placed },
            lon = exif.lon.takeIf { placed },
        )
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/photos/place")
            .post(jsonBody(body))
            .build()
        call(request, { text -> parseInstant(decode<PhotoPlaceJson>(text).routeAt) }) { result ->
            onResult(result.recoverCatching { throw needsPlace(it) })
        }
    }

    /**
     * `POST /v1/photos` (FR-16.1) — a photo already resized ([file] and its [thumb]), what its
     * EXIF said, where on the track it goes ([routeAt]: the placement check's answer or the
     * user's, which wins at upload) and its [caption]. Answers with the photo as stored.
     */
    fun uploadPhoto(
        activityId: String,
        file: PhotoImage,
        thumb: PhotoImage,
        exif: PhotoExif,
        routeAt: Instant,
        caption: String,
        onResult: (Result<Photo>) -> Unit,
    ) {
        val body = MultipartBody.Builder()
            .setType(MultipartBody.FORM)
            .addFormDataPart("activity_id", activityId)
            .addFormDataPart("file", "photo", file.bytes.toRequestBody(file.contentType.toMediaType()))
            .addFormDataPart("thumb", "thumb", thumb.bytes.toRequestBody(thumb.contentType.toMediaType()))
            .apply {
                if (exif.takenAt != null) addFormDataPart("taken_at", exif.takenAt.toString())
                else exif.takenLocal?.let { addFormDataPart("taken_local", it) }
                if (exif.lat != null && exif.lon != null) {
                    addFormDataPart("lat", exif.lat.toString())
                    addFormDataPart("lon", exif.lon.toString())
                }
                addFormDataPart("route_at", routeAt.toString())
                if (caption.isNotEmpty()) addFormDataPart("caption", caption)
            }
            .build()
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/photos").post(body).build()
        call(request, { text -> decode<PhotoJson>(text).toPhoto() }) { result ->
            onResult(result.recoverCatching { throw needsPlace(it) })
        }
    }

    /** `PATCH /v1/photos/{id}` (FR-16.4) — a field given changes: [caption] (empty clears it),
     *  [routeAt] the photo's new moment on its track, which the server clamps to the track. */
    fun updatePhoto(id: String, caption: String?, routeAt: Instant?, onResult: (Result<Photo>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/photos/" + id)
            .patch(jsonBody(PhotoPatchJson(caption, routeAt?.toString())))
            .build()
        call(request, { text -> decode<PhotoJson>(text).toPhoto() }, onResult)
    }

    /** `DELETE /v1/photos/{id}` (FR-16.5) — the photo and both its images. */
    fun deletePhoto(id: String, onResult: (Result<Unit>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/photos/" + id).delete().build()
        call(request, { }, onResult)
    }

    /** `photo_needs_place` as its own exception, so the Photos tab can tell it from a failure. */
    private fun needsPlace(failure: Throwable): Throwable =
        if (failure is ApiException && failure.code == 422 && failure.errorCode == "photo_needs_place") {
            PhotoNeedsPlaceException(failure.message.orEmpty())
        } else {
            failure
        }

    private fun PhotoJson.toPhoto() = Photo(
        id = id,
        activityId = activityId,
        takenAt = takenAt?.let(::parseInstant),
        routeAt = parseInstant(routeAt),
        lon = lon,
        lat = lat,
        caption = caption?.takeIf { it.isNotEmpty() },
        width = width,
        height = height,
        url = url,
        thumbUrl = thumbUrl,
        timezone = timezone,
    )

    /** `GET /v1/private-locations` — every one, oldest first. */
    fun privateLocations(onResult: (Result<List<PrivateLocation>>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/private-locations").build()
        call(request, { text -> decode<List<PrivateLocationJson>>(text).map { it.toPrivateLocation() } }, onResult)
    }

    /**
     * `POST /v1/private-locations`, or with [id] `PATCH /v1/private-locations/{id}` — the
     * whole circle either way. Answers with it as saved (the name trimmed). A refusal — a
     * radius or place out of range, a 21st location, the demo account — is the server's own
     * wording, in the app's language.
     */
    fun savePrivateLocation(
        id: String?,
        name: String,
        lon: Double,
        lat: Double,
        radiusM: Int,
        onResult: (Result<PrivateLocation>) -> Unit,
    ) {
        val body = jsonBody(PrivateLocationInputJson(name, lon, lat, radiusM))
        val url = BuildConfig.API_BASE_URL + API_V1 + "/private-locations" + (id?.let { "/$it" } ?: "")
        val request = Request.Builder().url(url).apply { if (id == null) post(body) else patch(body) }.build()
        call(request, { text -> decode<PrivateLocationJson>(text).toPrivateLocation() }, onResult)
    }

    /** `DELETE /v1/private-locations/{id}`. */
    fun deletePrivateLocation(id: String, onResult: (Result<Unit>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/private-locations/" + id).delete().build()
        call(request, { }, onResult)
    }

    private fun PrivateLocationJson.toPrivateLocation() = PrivateLocation(id, name, lon, lat, radiusM)

    /** `GET /v1/stories` — every Story of the account, newest first (`docs/SPEC.md` FR-14). */
    fun stories(onResult: (Result<List<Story>>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/stories").build()
        call(request, { text -> decode<StoriesJson>(text).stories.map { it.toStory() } }, onResult)
    }

    /** `GET /v1/stories/{id}` — one Story. A `404` ([ApiException]) is one that doesn't exist
     *  or isn't this account's, which the server doesn't tell apart (FR-14.5). */
    fun story(id: String, onResult: (Result<Story>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/stories/" + id).build()
        call(request, { text -> decode<StoryJson>(text).toStory() }, onResult)
    }

    /** `POST /v1/stories` — a new Story with [activityIds] in it, in one step (FR-14.2). A
     *  refusal (a blank or long name, a demo account) is the server's own wording. */
    fun createStory(name: String, description: String, activityIds: Collection<String>, onResult: (Result<Story>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/stories")
            .post(jsonBody(StoryCreateJson(name, description, activityIds.toList())))
            .build()
        call(request, { text -> decode<StoryJson>(text).toStory() }, onResult)
    }

    /** `PATCH /v1/stories/{id}` — both fields every time: an empty [description] clears it. */
    fun updateStory(id: String, name: String, description: String, onResult: (Result<Story>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/stories/" + id)
            .patch(jsonBody(StoryInputJson(name, description)))
            .build()
        call(request, { text -> decode<StoryJson>(text).toStory() }, onResult)
    }

    /** `DELETE /v1/stories/{id}` — the Story alone; its activities stay. */
    fun deleteStory(id: String, onResult: (Result<Unit>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/stories/" + id).delete().build()
        call(request, { }, onResult)
    }

    /** `POST` ([add]) or `DELETE /v1/stories/{id}/activities` with [activityIds] (FR-14.3): puts
     *  them in the Story, or takes them out. Answers with the Story as it now is. */
    fun changeStoryActivities(id: String, activityIds: Collection<String>, add: Boolean, onResult: (Result<Story>) -> Unit) {
        val body = jsonBody(StoryActivitiesJson(activityIds.toList()))
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/stories/" + id + "/activities")
            .apply { if (add) post(body) else delete(body) }
            .build()
        call(request, { text -> decode<StoryJson>(text).toStory() }, onResult)
    }

    /** `POST /v1/stories/{id}/send` — a copy of the Story to [email] (FR-14.7). Succeeds
     *  whether or not the address has an account: the server never says. */
    fun sendStory(id: String, email: String, onResult: (Result<Unit>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/stories/" + id + "/send")
            .post(jsonBody(EmailJson(email)))
            .build()
        call(request, { }, onResult)
    }

    /** `GET /v1/story-sends` — the Stories tab's inbox. */
    fun storySends(onResult: (Result<StoryInbox>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/story-sends").build()
        call(request, { text ->
            val json = decode<StoryInboxJson>(text)
            StoryInbox(
                sends = json.sends.map { StorySend(it.id, it.storyName, it.from, it.activityCount) },
                copying = json.copying.map { StoryCopying(it.storyName, it.from) },
            )
        }, onResult)
    }

    /** `POST /v1/story-sends/{id}/accept` ([accept]) or `DELETE /v1/story-sends/{id}` (decline). */
    fun answerStorySend(id: String, accept: Boolean, onResult: (Result<Unit>) -> Unit) {
        val url = BuildConfig.API_BASE_URL + API_V1 + "/story-sends/" + id + if (accept) "/accept" else ""
        val request = Request.Builder().url(url)
            .apply { if (accept) post(EMPTY_BODY) else delete() }
            .build()
        call(request, { }, onResult)
    }

    private fun StoryJson.toStory() = Story(
        id = id,
        name = name,
        description = description.orEmpty(),
        from = from,
        activityIds = activityIds,
        stats = StoryTotals("", stats.count, stats.distanceMeters, stats.movingSeconds),
        byType = stats.byType.map { StoryTotals(it.activityType, it.count, it.distanceMeters, it.movingSeconds) },
    )

    private fun ActivityRowJson.toActivity() = Activity(
        id = id,
        startedAt = startedAt,
        activityType = activityType,
        timezone = timezone,
        name = name,
        distanceMeters = distanceMeters,
        durationSeconds = durationSeconds,
        description = description,
        bbox = bbox?.takeIf { it.size == 4 },
        pending = pending,
        edited = edited,
        isPrivate = private,
        stories = stories.map { StoryRef(it.id, it.name) },
    )

    private fun AccountJson.toProfile() = Profile(email, isDemo, emailVerified, displayName, country, timezone, locale, avatarUrl)

    private fun ExportStatusJson.toExportStatus() = ExportStatus(
        state = state,
        expiresAt = expiresAt?.let(::parseInstant),
        totalSize = totalSize,
        parts = parts.map { ExportPart(it.n, it.size, it.name, it.url) },
    )

    /**
     * `GET /v1/activities/histogram?days=&before=` — one page of the days that have activity
     * (`docs/IMPLEMENTATION.md` §4.7's activity-day pagination mode): the [limit] most recent,
     * or the [limit] most recent strictly before [before]. What the map's date-range slider
     * (`map/ActivityDays.kt`) pages through, as the web's does.
     */
    fun activityDayPage(limit: Int, before: String?, onResult: (Result<ActivityDayPage>) -> Unit) {
        val url = (BuildConfig.API_BASE_URL + API_V1 + "/activities/histogram").toHttpUrl().newBuilder()
            .addQueryParameter("days", limit.toString())
            .apply { if (before != null) addQueryParameter("before", before) }
            .build()
        call(Request.Builder().url(url).build(), ::parseActivityDays, onResult)
    }

    /**
     * `GET /v1/activities/histogram?from=&to=` — every day with activity in a calendar window,
     * both ends inclusive (§4.7's calendar-window mode). Profile asks for everything up to the
     * end of this year in one call, the same single query the web's page runs for its grids.
     */
    fun activityDays(from: String, to: String, onResult: (Result<ActivityDayPage>) -> Unit) {
        val url = (BuildConfig.API_BASE_URL + API_V1 + "/activities/histogram").toHttpUrl().newBuilder()
            .addQueryParameter("from", from)
            .addQueryParameter("to", to)
            .build()
        call(Request.Builder().url(url).build(), ::parseActivityDays, onResult)
    }

    private fun parseActivityDays(body: String): ActivityDayPage {
        val json = decode<HistogramJson>(body)
        return ActivityDayPage(json.buckets.map { ActivityDay(it.date, it.count, it.distanceMeters) }, json.earliest?.ifEmpty { null })
    }

    /**
     * How many of the account's activities use each `activity_type` — what the recording Edit screen's
     * Type picker lists, the same per-type counts the web client's Type facet is built from.
     * Read from the same unpaginated `GET /v1/activities` as [activities], since there is
     * no dedicated facets endpoint and this list already carries every live row.
     */
    fun activityTypeCounts(onResult: (Result<Map<String, Int>>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/activities").build()
        call(request, { body ->
            buildMap {
                for (row in decode<ActivitiesJson>(body).activities) {
                    if (row.activityType.isNotEmpty()) merge(row.activityType, 1, Int::plus)
                }
            }
        }, onResult)
    }

    /** Each row of a `GET /v1/activities` body the tracks tile draws — one with a bbox and
     *  not Pending — with that bbox as `[minLon, minLat, maxLon, maxLat]`. */
    private fun forEachDrawn(body: String, action: (ActivityRowJson, DoubleArray) -> Unit) {
        for (row in decode<ActivitiesJson>(body).activities) {
            if (row.pending) continue
            val box = row.bbox ?: continue
            if (box.size != 4) continue
            action(row, box.toDoubleArray())
        }
    }

    /** A response body as its wire class (Wire.kt). */
    private inline fun <reified T> decode(text: String): T = apiJson.decodeFromString(text)

    /** A request body from its wire class (Wire.kt). */
    private inline fun <reified T> jsonBody(value: T): RequestBody = apiJson.encodeToString(value).toRequestBody(JSON)

    private val EMPTY_BODY: RequestBody = ByteArray(0).toRequestBody(null, 0, 0)

    /**
     * Every endpoint that mints a session answers with the same body, and all of them carry
     * `session_token` — the field a browser ignores (it already has the credential as a
     * `Set-Cookie`) and a native client exists to read.
     */
    private fun authenticate(path: String, body: RequestBody, onResult: (Result<Account>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + path).post(body).build()
        call(request, { text ->
            val token = decode<SessionTokenJson>(text).sessionToken
            if (token.isEmpty()) throw IOException("the server returned no session token")
            Account(token = token, profile = decode<AccountJson>(text).toProfile())
        }, onResult)
    }

    private fun <T> call(request: Request, parse: (String) -> T, onResult: (Result<T>) -> Unit) {
        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                deliver(Result.failure(e))
            }

            override fun onResponse(call: Call, response: Response) {
                val body = response.use { it.body?.string().orEmpty() }
                if (refusedBuild(response)) return
                deliver(
                    if (response.isSuccessful) runCatching { parse(body) }
                    else Result.failure(ApiException.from(response.code, body)),
                )
            }

            private fun deliver(result: Result<T>) {
                main.post { onResult(result) }
            }
        })
    }
}

/** One time span asked about in [HoldMyTrackApi.activityOverlaps], under the caller's [key]. */
data class OverlapSpan(val key: String, val start: Instant, val end: Instant)

/** The live activity a span overlaps: its [id], to open it on the map, its [name] (empty when it
 *  has none) and type, and its start in the zone it was recorded in, which titles it when it has
 *  no name and gives the day the map opens on. */
data class ActivityOverlap(val id: String, val name: String, val activityType: String, val startedAt: String, val timezone: String?)
