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
import org.json.JSONArray
import org.json.JSONObject

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
) {
    companion object {
        fun parse(json: JSONObject): ExportStatus {
            val parts = json.optJSONArray("parts")
            return ExportStatus(
                state = json.optString("state", "none"),
                expiresAt = json.optString("expires_at").takeIf { it.isNotEmpty() }?.let(java.time.Instant::parse),
                totalSize = json.optLong("total_size"),
                parts = List(parts?.length() ?: 0) { i ->
                    val p = parts!!.getJSONObject(i)
                    ExportPart(p.optInt("n"), p.optLong("size"), p.optString("name"), p.optString("url"))
                },
            )
        }
    }
}

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
) {
    companion object {
        fun parse(json: JSONObject) = Profile(
            email = json.optString("email"),
            isDemo = json.optBoolean("isDemo"),
            emailVerified = json.optBoolean("email_verified", true),
            displayName = json.optString("display_name"),
            country = json.optString("country"),
            timezone = json.optString("timezone"),
            locale = json.optString("locale"),
            avatarUrl = json.optString("avatar_url"),
        )
    }
}

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
 * What `POST /v1/activities/upload` said about one file: "enqueued" or "already_processed" for a
 * single `.gpx`/`.fit`/`.tcx`, "zip_accepted" for a `.zip` or a Google Takeout export, which the
 * server unpacks in the background; [batch] is the archive's, for finding its [UnpackedArchive]
 * once it has been.
 */
data class UploadOutcome(val status: String, val batch: String)

/**
 * An archive the server finished unpacking (`GET /v1/uploads/active`'s `unpacked`): how many of
 * its files were already imported before or skipped, and whether it had more files than one
 * upload reads ([truncated]); [error], when it couldn't be unpacked, is why, in the app's
 * language. Neither already-imported nor skipped files become jobs, so the sync history never
 * shows them: this is the only place they're counted.
 */
data class UnpackedArchive(val batch: String, val already: Int, val skipped: Int, val truncated: Boolean, val error: String)

/**
 * One row of the sync history — an `ingest` job and, once it has produced one, the activity it
 * became; the web's `UploadHistoryRow`. [filename] is the job's `source_detail`: a real name for
 * an uploaded file or a Takeout entry, a raw external id for everything else. [startedAt],
 * [distanceMeters] and [activityId] are null while the job is still processing, or forever if
 * it failed: there is no activity behind it to describe. [finishedAt] is when the import finished
 * — when it was synced — and [keptSource], for an activity cross-source deduplication set aside
 * as a duplicate (`docs/IMPLEMENTATION.md` §4.6), is the source of the copy kept in its place;
 * such a row's [status] is still "done", the import itself having succeeded.
 */
data class SyncHistoryEntry(
    val filename: String,
    val source: String,
    val status: String,
    val error: String,
    val finishedAt: String?,
    val startedAt: String?,
    val distanceMeters: Double?,
    val activityId: String?,
    val keptSource: String?,
    /** The zone [startedAt]'s activity was recorded in (`docs/IMPLEMENTATION.md` §4.30). */
    val timezone: String? = null,
)

/** A page of the sync history, plus the counts that describe the whole of it. */
data class SyncHistory(
    val total: Long,
    val processing: Long,
    val limit: Int,
    val offset: Int,
    val entries: List<SyncHistoryEntry>,
)

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

/** One week's or month's totals, as `GET /v1/activities/trends` sums them; [periodStart] is the
 *  bucket's first day, `YYYY-MM-DD`. Periods with nothing are never returned. */
data class TrendPeriod(
    val periodStart: String,
    val count: Int,
    val distanceMeters: Double,
    val movingSeconds: Long,
    val elevationGainM: Double,
)

/**
 * A request the server answered with a non-2xx status. The API writes its errors as plain
 * text (`http.Error`), so `message` is the server's own wording, shown to the user as-is
 * rather than replaced with something vaguer — "invalid email or password" and "an account
 * with this email already exists" are exactly what someone needs to read.
 */
class ApiException(val code: Int, message: String, val errorCode: String? = null) :
    IOException(message.ifBlank { "request failed ($code)" }) {
    companion object {
        /**
         * The refusal in a non-2xx [body]: plain text as it is, or — for the refusals a client
         * branches on (`demo_read_only`, `photo_needs_place`) — `{error, message}`, its message
         * shown and its code kept.
         */
        fun from(code: Int, body: String): ApiException {
            val text = body.trim()
            if (text.startsWith("{")) {
                val json = runCatching { JSONObject(text) }.getOrNull()
                val message = json?.optString("message").orEmpty()
                if (json != null && message.isNotEmpty()) {
                    return ApiException(code, message, json.optString("error").takeIf { it.isNotEmpty() })
                }
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
     *  `docs/adr/0007-in-app-gps-recording-submits-directly.md`) and a Google Maps Timeline
     *  export read on the phone (`timeline/TimelineImport.kt`, root `docs/IMPLEMENTATION.md`
     *  §4.0.5). iOS's own is `"healthkit"`; an uploaded file has its own endpoint
     *  ([uploadActivityFile]). */
    const val SOURCE_HEALTH_CONNECT = "healthconnect"
    const val SOURCE_RECORDED = "recorded"
    const val SOURCE_TIMELINE = "timeline"
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
    /** This build: the release number and the commit, "0.9 (62da50b)" — what the You tab shows
     *  and the User-Agent carries. */
    val appVersion: String = "${BuildConfig.VERSION_NAME} (${BuildConfig.GIT_SHA})"

    val client: OkHttpClient = OkHttpClient.Builder()
        .dispatcher(Dispatcher().apply { maxRequestsPerHost = 20 })
        .addInterceptor(BearerInterceptor)
        .addInterceptor(UserAgentInterceptor)
        .addInterceptor(LanguageInterceptor)
        .addInterceptor(OriginInterceptor)
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
     * Names the app and its build — `HoldMyTrack-Android/0.9 (62da50b)` — on HoldMyTrack's own
     * requests, so the server's logs say which build made a call. Other origins keep what the
     * request already had: MapLibre's own agent on the basemap's assets.
     */
    private object UserAgentInterceptor : Interceptor {
        override fun intercept(chain: Interceptor.Chain): Response {
            val request = chain.request()
            if (!request.url.toString().startsWith(BuildConfig.API_BASE_URL)) return chain.proceed(request)
            return chain.proceed(request.newBuilder().header("User-Agent", "HoldMyTrack-Android/$appVersion").build())
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
     * be sent, and the watermark must not move, until this one is answered.
     *
     * Returns the server's per-activity verdicts rather than a single pass/fail — a batch is
     * not all-or-nothing there, and the caller needs to know which ones landed before it can
     * decide how far the watermark may move. A non-2xx status is the whole request failing and
     * throws instead; nothing in the batch was decided.
     *
     * [batch] and [batchTitle] name one import the caller splits over several requests (a
     * Timeline export, a hundred activities to a request), so the history counts it as one.
     */
    suspend fun syncActivities(
        activities: List<JSONObject>,
        source: String,
        batch: String? = null,
        batchTitle: String? = null,
    ): List<SyncResult> =
        withContext(Dispatchers.IO) {
            val body = JSONObject()
                .put("source", source)
                .put("activities", JSONArray(activities))
            if (batch != null) body.put("batch", batch)
            if (batchTitle != null) body.put("batch_title", batchTitle)
            val request = Request.Builder()
                .url(BuildConfig.API_BASE_URL + API_V1 + "/sync/activities")
                .post(body.toString().toRequestBody(JSON))
                .build()
            val response = client.newCall(request).execute()
            val text = response.use { it.body?.string().orEmpty() }
            if (!response.isSuccessful) throw ApiException.from(response.code, text)
            val results = JSONObject(text).getJSONArray("results")
            List(results.length()) { i ->
                val result = results.getJSONObject(i)
                SyncResult(
                    externalId = result.optString("external_id"),
                    status = result.optString("status"),
                    error = result.optString("error"),
                )
            }
        }

    /**
     * Uploads take as long as sending the file does, up to 512 MiB, and the server answers once it
     * has stored the file, which for a large archive takes longer than the default ten seconds.
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
     * FR-3.1–FR-3.3): one `.gpx`, `.fit`, `.tcx` or `.zip`, which the server tells apart from a
     * Google Takeout export itself. The file is streamed from [uri] rather than read into memory
     * first, since a `.zip` can be 512 MiB; [onProgress] gets the bytes sent so far, on the
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
        val json = runCatching { JSONObject(text) }.getOrDefault(JSONObject())
        UploadOutcome(status = json.optString("status"), batch = json.optString("batch"))
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

    /**
     * `GET /v1/uploads` — every ingest job this account has, newest first, whatever path it
     * arrived by. The sync screen and an uploaded file share one history because they are the
     * same jobs table; there is no separate notion of "a sync" to list.
     */
    fun syncHistory(limit: Int, offset: Int, onResult: (Result<SyncHistory>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/uploads?limit=" + limit + "&offset=" + offset)
            .build()
        call(request, { body ->
            val json = JSONObject(body)
            val rows = json.getJSONArray("uploads")
            SyncHistory(
                total = json.optLong("total"),
                processing = json.optLong("processing"),
                limit = json.optInt("limit", limit),
                offset = json.optInt("offset", offset),
                entries = List(rows.length()) { i ->
                    val row = rows.getJSONObject(i)
                    SyncHistoryEntry(
                        filename = row.optString("filename").ifBlank { row.optString("external_id") },
                        source = row.optString("source"),
                        status = row.optString("status"),
                        error = row.optString("error"),
                        finishedAt = row.optString("finished_at").ifBlank { null },
                        startedAt = row.optString("started_at").ifBlank { null },
                        distanceMeters = if (row.isNull("distance_meters")) null else row.optDouble("distance_meters"),
                        activityId = row.optString("activity_id").ifBlank { null },
                        keptSource = row.optJSONObject("superseded_by")?.optString("source")?.ifBlank { null },
                        timezone = row.optString("timezone").ifBlank { null },
                    )
                },
            )
        }, onResult)
    }

    /** `GET /v1/uploads/active`'s archives unpacked in the last hour — see [UnpackedArchive]. */
    fun unpackedArchives(onResult: (Result<List<UnpackedArchive>>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/uploads/active")
            .build()
        call(request, { body ->
            val rows = JSONObject(body).optJSONArray("unpacked") ?: JSONArray()
            List(rows.length()) { i ->
                val row = rows.getJSONObject(i)
                UnpackedArchive(
                    batch = row.optString("batch"),
                    already = row.optInt("already"),
                    skipped = row.optInt("skipped"),
                    truncated = row.optBoolean("truncated"),
                    error = row.optString("error"),
                )
            }
        }, onResult)
    }

    /** `GET /v1/auth/providers` — which of the Google and Facebook buttons to show. */
    fun providers(onResult: (Result<Providers>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/auth/providers").build()
        call(request, { body ->
            val json = JSONObject(body)
            Providers(
                google = json.optBoolean("google"),
                facebook = json.optBoolean("facebook"),
                googleClientId = json.optString("google_client_id"),
            )
        }, onResult)
    }

    /**
     * `POST /v1/auth/google/token` — trades the ID token Credential Manager returned for a
     * session. The server checks the token's signature against Google's keys before trusting
     * anything in it (`docs/adr/0016-native-sign-in.md`).
     */
    fun signInWithGoogle(idToken: String, timezone: String, onResult: (Result<Account>) -> Unit) {
        val body = JSONObject().put("id_token", idToken).put("tz", timezone)
        authenticate("/auth/google/token", body.toString().toRequestBody(JSON), onResult)
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
        val body = JSONObject().put("code", code).put("verifier", verifier)
        authenticate("/auth/handoff", body.toString().toRequestBody(JSON), onResult)
    }

    fun signIn(email: String, password: String, onResult: (Result<Account>) -> Unit) {
        authenticate("/auth/login", credentials(email, password), onResult)
    }

    /** Sends the phone's IANA zone as `timezone`, as the web's sign-up sends the browser's, so
     *  a new account starts on it rather than on UTC (Google and Facebook already send it). */
    fun signUp(email: String, password: String, onResult: (Result<Account>) -> Unit) {
        val body = JSONObject()
            .put("email", email)
            .put("password", password)
            .put("timezone", ZoneId.systemDefault().id)
        authenticate("/auth/signup", body.toString().toRequestBody(JSON), onResult)
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
        call(request, { body -> Profile.parse(JSONObject(body)) }, onResult)
    }

    /** `GET /v1/account/settings/options` — see [SettingsOptions]. */
    fun settingsOptions(onResult: (Result<SettingsOptions>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/account/settings/options").build()
        call(request, { body ->
            val json = JSONObject(body)
            fun options(array: JSONArray?) = List(array?.length() ?: 0) { i ->
                val o = array!!.getJSONObject(i)
                SettingsOption(o.optString("value"), o.optString("label"))
            }
            val groups = json.optJSONArray("timezones")
            SettingsOptions(
                countries = options(json.optJSONArray("countries")),
                timezones = List(groups?.length() ?: 0) { i ->
                    val g = groups!!.getJSONObject(i)
                    TimezoneGroup(g.optString("label").ifBlank { g.optString("region") }, options(g.optJSONArray("options")))
                },
                languages = options(json.optJSONArray("languages")),
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
        val body = JSONObject()
            .put("display_name", displayName)
            .put("country", country)
            .put("timezone", timezone)
            .put("locale", locale)
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/account/settings")
            .patch(body.toString().toRequestBody(JSON))
            .build()
        call(request, { text -> Profile.parse(JSONObject(text)) }, onResult)
    }

    /** `POST /v1/account/avatar` — the image as picked; the server checks its type (PNG,
     *  JPEG, WebP) and size (5 MB) itself. */
    fun uploadAvatar(bytes: ByteArray, contentType: String, onResult: (Result<Profile>) -> Unit) {
        val body = MultipartBody.Builder()
            .setType(MultipartBody.FORM)
            .addFormDataPart("file", "avatar", bytes.toRequestBody(contentType.toMediaType()))
            .build()
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/account/avatar").post(body).build()
        call(request, { text -> Profile.parse(JSONObject(text)) }, onResult)
    }

    /** `DELETE /v1/account/avatar`. */
    fun deleteAvatar(onResult: (Result<Profile>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/account/avatar").delete().build()
        call(request, { text -> Profile.parse(JSONObject(text)) }, onResult)
    }

    /**
     * `DELETE /v1/account` (root `docs/SPEC.md` FR-1.11) — [email] typed as confirmation. The
     * server closes the account and ends every session of it; a mismatch is its own wording.
     * The caller clears what this device holds for the account ([Session.clear] and the rest).
     */
    fun deleteAccount(email: String, onResult: (Result<Unit>) -> Unit) {
        val body = JSONObject().put("email", email)
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/account")
            .delete(body.toString().toRequestBody(JSON))
            .build()
        call(request, { }, onResult)
    }

    /** `GET /v1/account/export` — where the account's latest copy of its data stands. */
    fun exportStatus(onResult: (Result<ExportStatus>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/account/export").build()
        call(request, { body -> ExportStatus.parse(JSONObject(body)) }, onResult)
    }

    /** `POST /v1/account/export` — asks for a copy; one already being prepared is returned
     *  instead. The server emails the account when it's ready, in the app's language. */
    fun requestExport(onResult: (Result<ExportStatus>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/account/export")
            .post(EMPTY_BODY)
            .build()
        call(request, { body -> ExportStatus.parse(JSONObject(body)) }, onResult)
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
        call(request, { body -> JSONObject(body).optString("message") }, onResult)
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
                val startedAt = runCatching { OffsetDateTime.parse(row.optString("started_at")).toInstant() }.getOrNull()
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
        call(Request.Builder().url(url).build(), { body ->
            val rows = JSONObject(body).getJSONArray("activities")
            List(rows.length()) { i -> parseActivity(rows.getJSONObject(i)) }
        }, onResult)
    }

    /**
     * `PATCH /v1/activities/{id}` — a full replace of the three fields a person can set
     * (`docs/IMPLEMENTATION.md` §4.7.4): an empty [name] or [description] clears it. Answers
     * with the row as it now is. A refusal (too long, a demo account) is the server's own
     * wording, in the app's language.
     */
    fun updateActivity(id: String, activityType: String, name: String, description: String, onResult: (Result<Activity>) -> Unit) {
        val body = JSONObject()
            .put("activity_type", activityType)
            .put("name", name)
            .put("description", description)
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/activities/" + id)
            .patch(body.toString().toRequestBody(JSON))
            .build()
        call(request, { text -> parseActivity(JSONObject(text)) }, onResult)
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
            val json = JSONObject(text)
            CoverageStatus(json.optBoolean("rendering"), json.optLong("version"), json.optString("tile_version"))
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
            val json = JSONObject(text)
            val rows = json.optJSONArray("spots") ?: JSONArray()
            SpotsInArea(List(rows.length()) { parseSpot(rows.getJSONObject(it)) }, json.optInt("total"))
        }, onResult)
    }

    /** `GET /v1/spots/{id}` (`docs/IMPLEMENTATION.md` §4.25) — see [SpotDetail]. */
    fun spotDetail(id: Long, onResult: (Result<SpotDetail>) -> Unit) {
        call(Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/spots/" + id).build(), { text ->
            val json = JSONObject(text)
            SpotDetail(
                spot = parseSpot(json),
                area = parseMultiPolygon(json.getJSONObject("area")),
                capturedAt = json.optString("captured_at").takeIf { it.isNotEmpty() }?.let(::parseInstant),
            )
        }, onResult)
    }

    /** `GET /v1/spots/captures` — every place the account has captured, newest first. */
    fun spotCaptures(onResult: (Result<List<SpotCapture>>) -> Unit) {
        call(Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/spots/captures").build(), { text ->
            val rows = JSONObject(text).optJSONArray("captures") ?: JSONArray()
            List(rows.length()) { parseSpotCapture(rows.getJSONObject(it)) }
        }, onResult)
    }

    /** `POST /v1/spots/{id}/captures` — captures the place at the position last measured inside
     *  it; the server checks it against the area. A place already captured answers the first
     *  capture. Outside the area is an [ApiException] with code 422. */
    fun captureSpot(id: Long, lat: Double, lon: Double, onResult: (Result<SpotCapture>) -> Unit) {
        val body = JSONObject().put("lat", lat).put("lon", lon)
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/spots/" + id + "/captures")
            .post(body.toString().toRequestBody(JSON))
            .build()
        call(request, { text -> parseSpotCapture(JSONObject(text)) }, onResult)
    }

    private fun parseSpotCapture(json: JSONObject) =
        SpotCapture(json.getLong("spot_id"), parseInstant(json.getString("captured_at")))

    private fun parseInstant(text: String): Instant = OffsetDateTime.parse(text).toInstant()

    /** A GeoJSON MultiPolygon's coordinates, `[[[[lon, lat], …], …], …]`, as polygons of rings. */
    private fun parseMultiPolygon(json: JSONObject): SpotArea {
        val polygons = json.getJSONArray("coordinates")
        return List(polygons.length()) { p ->
            val rings = polygons.getJSONArray(p)
            List(rings.length()) { r ->
                val points = rings.getJSONArray(r)
                List(points.length()) { i ->
                    val point = points.getJSONArray(i)
                    GeoPoint(lat = point.getDouble(1), lon = point.getDouble(0))
                }
            }
        }
    }

    private fun parseSpot(json: JSONObject): Spot {
        fun text(key: String) = json.optString(key).takeIf { it.isNotEmpty() }
        return Spot(
            id = json.optLong("id"),
            category = json.optString("category"),
            name = text("name"),
            address = text("address"),
            description = text("description"),
            inscription = text("inscription"),
            memorial = text("memorial"),
            startDate = text("start_date"),
            wikipedia = text("wikipedia"),
            lon = json.optDouble("lon"),
            lat = json.optDouble("lat"),
        )
    }

    /** `GET /v1/activities/track-metrics/{id}` (`docs/IMPLEMENTATION.md` §4.3.1) — see
     *  [TrackMetrics]. */
    fun trackMetrics(id: String, onResult: (Result<TrackMetrics>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/activities/track-metrics/" + id).build()
        call(request, { text ->
            val json = JSONObject(text)
            val rows = json.getJSONArray("points")
            TrackMetrics(
                activityId = json.optString("activity_id", id),
                points = List(rows.length()) { i ->
                    val p = rows.getJSONObject(i)
                    TrackMetricPoint(
                        lon = p.getDouble("lon"),
                        lat = p.getDouble("lat"),
                        speedMps = p.optDouble("speed_mps", 0.0),
                        timeS = p.optLong("time_s"),
                    )
                },
            )
        }, onResult)
    }

    /** `GET /v1/activities/track-points/{id}` — the recorded points, unedited and with any
     *  Private location already clipped off, and the saved edit. A refusal (`409`: no
     *  recording kept, points without times) is the server's own wording. */
    fun trackPoints(id: String, onResult: (Result<TrackPoints>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/activities/track-points/" + id).build()
        call(request, { text ->
            val json = JSONObject(text)
            val rows = json.getJSONArray("points")
            TrackPoints(
                points = List(rows.length()) { i ->
                    val p = rows.getJSONArray(i)
                    TrackPoint(p.getDouble(0), p.getDouble(1), p.getLong(2))
                },
                edit = if (json.isNull("edit")) null else parseTrackEdit(json.getJSONObject("edit")),
            )
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
        val body = JSONObject().put("edit", edit?.let(::trackEditJson) ?: JSONObject.NULL)
        if (splitAt != null) {
            body.put("split_at", splitAt)
            if (allowHidden) body.put("allow_hidden", true)
        }
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/activities/track-edit/" + id)
            .post(body.toString().toRequestBody(JSON))
            .build()
        call(request, { }, onResult)
    }

    private fun parseTrackEdit(json: JSONObject): TrackEdit {
        fun pair(a: JSONArray) = a.getLong(0) to a.getLong(1)
        val remove = json.optJSONArray("remove")
        val drop = json.optJSONArray("drop")
        // Keyed by time, which JSON can only carry as an object key.
        val move = json.optJSONObject("move")
        return TrackEdit(
            keep = json.optJSONArray("keep")?.let(::pair),
            remove = List(remove?.length() ?: 0) { pair(remove!!.getJSONArray(it)) },
            drop = List(drop?.length() ?: 0) { drop!!.getLong(it) },
            move = move?.keys()?.asSequence()?.associate { t ->
                val at = move.getJSONArray(t)
                t.toLong() to (at.getDouble(0) to at.getDouble(1))
            }.orEmpty(),
        )
    }

    private fun trackEditJson(edit: TrackEdit): JSONObject = JSONObject().apply {
        edit.keep?.let { put("keep", JSONArray().put(it.first).put(it.second)) }
        if (edit.remove.isNotEmpty()) put("remove", JSONArray(edit.remove.map { JSONArray().put(it.first).put(it.second) }))
        if (edit.drop.isNotEmpty()) put("drop", JSONArray(edit.drop))
        if (edit.move.isNotEmpty()) {
            put("move", JSONObject().apply { edit.move.forEach { (t, at) -> put(t.toString(), JSONArray().put(at.first).put(at.second)) } })
        }
    }

    /**
     * `GET /v1/photos?activity=` or `?story=` (`docs/SPEC.md` FR-16.3) — one activity's photos,
     * or every photo of a Story's activities, in route order. Exactly one of the two is given.
     */
    fun photos(activity: String?, story: String?, onResult: (Result<List<Photo>>) -> Unit) {
        val url = (BuildConfig.API_BASE_URL + API_V1 + "/photos").toHttpUrl().newBuilder()
            .apply { if (activity != null) addQueryParameter("activity", activity) }
            .apply { if (story != null) addQueryParameter("story", story) }
            .build()
        call(Request.Builder().url(url).build(), { text ->
            val rows = JSONObject(text).optJSONArray("photos") ?: JSONArray()
            List(rows.length()) { parsePhoto(rows.getJSONObject(it)) }
        }, onResult)
    }

    /**
     * `POST /v1/photos/place` (FR-16.1) — where an upload of a photo with this EXIF would be
     * placed on [activityId]'s track, storing nothing: its `route_at`. A
     * [PhotoNeedsPlaceException] when the server can't tell, for the user to choose.
     */
    fun checkPhotoPlace(activityId: String, exif: PhotoExif, onResult: (Result<Instant>) -> Unit) {
        val body = JSONObject().put("activity_id", activityId)
        exif.takenAt?.let { body.put("taken_at", it.toString()) }
        if (exif.takenAt == null) exif.takenLocal?.let { body.put("taken_local", it) }
        if (exif.lat != null && exif.lon != null) body.put("lat", exif.lat).put("lon", exif.lon)
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/photos/place")
            .post(body.toString().toRequestBody(JSON))
            .build()
        call(request, { text -> parseInstant(JSONObject(text).getString("route_at")) }) { result ->
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
        call(request, { text -> parsePhoto(JSONObject(text)) }) { result ->
            onResult(result.recoverCatching { throw needsPlace(it) })
        }
    }

    /** `PATCH /v1/photos/{id}` (FR-16.4) — a field given changes: [caption] (empty clears it),
     *  [routeAt] the photo's new moment on its track, which the server clamps to the track. */
    fun updatePhoto(id: String, caption: String?, routeAt: Instant?, onResult: (Result<Photo>) -> Unit) {
        val body = JSONObject()
        caption?.let { body.put("caption", it) }
        routeAt?.let { body.put("route_at", it.toString()) }
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/photos/" + id)
            .patch(body.toString().toRequestBody(JSON))
            .build()
        call(request, { text -> parsePhoto(JSONObject(text)) }, onResult)
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

    private fun parsePhoto(json: JSONObject) = Photo(
        id = json.getString("id"),
        activityId = json.optString("activity_id"),
        takenAt = json.optNullableString("taken_at")?.let(::parseInstant),
        routeAt = parseInstant(json.getString("route_at")),
        lon = json.optNullableDouble("lon"),
        lat = json.optNullableDouble("lat"),
        caption = json.optNullableString("caption")?.takeIf { it.isNotEmpty() },
        width = json.optInt("width"),
        height = json.optInt("height"),
        url = json.optString("url"),
        thumbUrl = json.optString("thumb_url"),
        timezone = json.optString("timezone"),
    )

    /** `GET /v1/private-locations` — every one, oldest first. */
    fun privateLocations(onResult: (Result<List<PrivateLocation>>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/private-locations").build()
        call(request, { text ->
            val rows = JSONArray(text)
            List(rows.length()) { parsePrivateLocation(rows.getJSONObject(it)) }
        }, onResult)
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
        val body = JSONObject().put("name", name).put("lon", lon).put("lat", lat).put("radius_m", radiusM)
            .toString().toRequestBody(JSON)
        val url = BuildConfig.API_BASE_URL + API_V1 + "/private-locations" + (id?.let { "/$it" } ?: "")
        val request = Request.Builder().url(url).apply { if (id == null) post(body) else patch(body) }.build()
        call(request, { text -> parsePrivateLocation(JSONObject(text)) }, onResult)
    }

    /** `DELETE /v1/private-locations/{id}`. */
    fun deletePrivateLocation(id: String, onResult: (Result<Unit>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/private-locations/" + id).delete().build()
        call(request, { }, onResult)
    }

    private fun parsePrivateLocation(json: JSONObject) = PrivateLocation(
        id = json.getString("id"),
        name = json.optString("name"),
        lon = json.getDouble("lon"),
        lat = json.getDouble("lat"),
        radiusM = json.getInt("radius_m"),
    )

    /** `GET /v1/stories` — every Story of the account, newest first (`docs/SPEC.md` FR-14). */
    fun stories(onResult: (Result<List<Story>>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/stories").build()
        call(request, { text ->
            val rows = JSONObject(text).optJSONArray("stories") ?: JSONArray()
            List(rows.length()) { parseStory(rows.getJSONObject(it)) }
        }, onResult)
    }

    /** `GET /v1/stories/{id}` — one Story. A `404` ([ApiException]) is one that doesn't exist
     *  or isn't this account's, which the server doesn't tell apart (FR-14.5). */
    fun story(id: String, onResult: (Result<Story>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/stories/" + id).build()
        call(request, { text -> parseStory(JSONObject(text)) }, onResult)
    }

    /** `POST /v1/stories` — a new Story with [activityIds] in it, in one step (FR-14.2). A
     *  refusal (a blank or long name, a demo account) is the server's own wording. */
    fun createStory(name: String, description: String, activityIds: Collection<String>, onResult: (Result<Story>) -> Unit) {
        val body = JSONObject()
            .put("name", name)
            .put("description", description)
            .put("activity_ids", JSONArray(activityIds))
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/stories")
            .post(body.toString().toRequestBody(JSON))
            .build()
        call(request, { text -> parseStory(JSONObject(text)) }, onResult)
    }

    /** `PATCH /v1/stories/{id}` — both fields every time: an empty [description] clears it. */
    fun updateStory(id: String, name: String, description: String, onResult: (Result<Story>) -> Unit) {
        val body = JSONObject().put("name", name).put("description", description)
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/stories/" + id)
            .patch(body.toString().toRequestBody(JSON))
            .build()
        call(request, { text -> parseStory(JSONObject(text)) }, onResult)
    }

    /** `DELETE /v1/stories/{id}` — the Story alone; its activities stay. */
    fun deleteStory(id: String, onResult: (Result<Unit>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/stories/" + id).delete().build()
        call(request, { }, onResult)
    }

    /** `POST` ([add]) or `DELETE /v1/stories/{id}/activities` with [activityIds] (FR-14.3): puts
     *  them in the Story, or takes them out. Answers with the Story as it now is. */
    fun changeStoryActivities(id: String, activityIds: Collection<String>, add: Boolean, onResult: (Result<Story>) -> Unit) {
        val body = JSONObject().put("activity_ids", JSONArray(activityIds)).toString().toRequestBody(JSON)
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/stories/" + id + "/activities")
            .apply { if (add) post(body) else delete(body) }
            .build()
        call(request, { text -> parseStory(JSONObject(text)) }, onResult)
    }

    /** `POST /v1/stories/{id}/send` — a copy of the Story to [email] (FR-14.7). Succeeds
     *  whether or not the address has an account: the server never says. */
    fun sendStory(id: String, email: String, onResult: (Result<Unit>) -> Unit) {
        val request = Request.Builder()
            .url(BuildConfig.API_BASE_URL + API_V1 + "/stories/" + id + "/send")
            .post(JSONObject().put("email", email).toString().toRequestBody(JSON))
            .build()
        call(request, { }, onResult)
    }

    /** `GET /v1/story-sends` — the Stories tab's inbox. */
    fun storySends(onResult: (Result<StoryInbox>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + "/story-sends").build()
        call(request, { text ->
            val json = JSONObject(text)
            val sends = json.optJSONArray("sends") ?: JSONArray()
            val copying = json.optJSONArray("copying") ?: JSONArray()
            StoryInbox(
                sends = List(sends.length()) { i ->
                    sends.getJSONObject(i).let {
                        StorySend(it.getString("id"), it.optString("story_name"), it.optString("from"), it.optInt("activity_count"))
                    }
                },
                copying = List(copying.length()) { i ->
                    copying.getJSONObject(i).let { StoryCopying(it.optString("story_name"), it.optString("from")) }
                },
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

    private fun parseStory(json: JSONObject): Story {
        fun totals(t: JSONObject) = StoryTotals(
            activityType = t.optString("activity_type"),
            count = t.optInt("count"),
            distanceMeters = t.optDouble("distance_meters", 0.0),
            movingSeconds = t.optLong("moving_seconds"),
        )
        val stats = json.optJSONObject("stats") ?: JSONObject()
        val byType = stats.optJSONArray("by_type") ?: JSONArray()
        val ids = json.optJSONArray("activity_ids") ?: JSONArray()
        return Story(
            id = json.getString("id"),
            name = json.optString("name"),
            description = json.optNullableString("description").orEmpty(),
            from = json.optNullableString("from"),
            activityIds = List(ids.length()) { ids.getString(it) },
            stats = totals(stats),
            byType = List(byType.length()) { totals(byType.getJSONObject(it)) },
        )
    }

    private fun parseActivity(row: JSONObject): Activity {
        val box = row.optJSONArray("bbox")?.takeIf { it.length() == 4 }
        val stories = row.optJSONArray("stories") ?: JSONArray()
        return Activity(
            id = row.getString("id"),
            startedAt = row.optString("started_at"),
            activityType = row.optString("activity_type"),
            timezone = row.optString("timezone"),
            name = row.optNullableString("name"),
            distanceMeters = row.optNullableDouble("distance_meters"),
            durationSeconds = row.optNullableDouble("duration_seconds")?.toLong(),
            description = row.optNullableString("description"),
            bbox = box?.let { b -> List(4) { b.getDouble(it) } },
            pending = row.optBoolean("pending"),
            edited = row.optBoolean("edited"),
            isPrivate = row.optBoolean("private"),
            stories = List(stories.length()) { i ->
                stories.getJSONObject(i).let { StoryRef(it.getString("id"), it.optString("name")) }
            },
        )
    }

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
        val json = JSONObject(body)
        val buckets = json.getJSONArray("buckets")
        return ActivityDayPage(
            days = List(buckets.length()) { i ->
                val bucket = buckets.getJSONObject(i)
                ActivityDay(bucket.getString("date"), bucket.optInt("count"), bucket.optDouble("distance_meters", 0.0))
            },
            earliest = json.optString("earliest").ifEmpty { null },
        )
    }

    /** `GET /v1/activities/trends?bucket=week|month` over its default window, the trailing 12
     *  months — what the web's Profile page charts. */
    fun activityTrends(bucket: String, onResult: (Result<List<TrendPeriod>>) -> Unit) {
        val url = (BuildConfig.API_BASE_URL + API_V1 + "/activities/trends").toHttpUrl().newBuilder()
            .addQueryParameter("bucket", bucket)
            .build()
        call(Request.Builder().url(url).build(), { body ->
            val periods = JSONObject(body).optJSONArray("periods") ?: JSONArray()
            List(periods.length()) { i ->
                val p = periods.getJSONObject(i)
                TrendPeriod(
                    periodStart = p.getString("period_start"),
                    count = p.optInt("count"),
                    distanceMeters = p.optDouble("distance_meters", 0.0),
                    movingSeconds = p.optLong("moving_seconds"),
                    elevationGainM = p.optDouble("elevation_gain_m", 0.0),
                )
            }
        }, onResult)
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
            val activities = JSONObject(body).getJSONArray("activities")
            buildMap {
                for (i in 0 until activities.length()) {
                    val type = activities.getJSONObject(i).optString("activity_type")
                    if (type.isNotEmpty()) merge(type, 1, Int::plus)
                }
            }
        }, onResult)
    }

    /** Each row of a `GET /v1/activities` body the tracks tile draws — one with a bbox and
     *  not Pending — with that bbox as `[minLon, minLat, maxLon, maxLat]`. */
    private fun forEachDrawn(body: String, action: (JSONObject, DoubleArray) -> Unit) {
        val activities = JSONObject(body).getJSONArray("activities")
        for (i in 0 until activities.length()) {
            val row = activities.getJSONObject(i)
            if (row.optBoolean("pending")) continue
            val box = row.optJSONArray("bbox") ?: continue
            if (box.length() != 4) continue
            action(row, DoubleArray(4) { box.getDouble(it) })
        }
    }

    private fun JSONObject.optNullableString(key: String): String? =
        if (isNull(key)) null else optString(key)

    private fun JSONObject.optNullableDouble(key: String): Double? =
        if (isNull(key)) null else optDouble(key).takeUnless { it.isNaN() }

    private fun credentials(email: String, password: String): RequestBody =
        JSONObject().put("email", email).put("password", password).toString().toRequestBody(JSON)

    private val EMPTY_BODY: RequestBody = ByteArray(0).toRequestBody(null, 0, 0)

    /**
     * Every endpoint that mints a session answers with the same body, and all of them carry
     * `session_token` — the field a browser ignores (it already has the credential as a
     * `Set-Cookie`) and a native client exists to read.
     */
    private fun authenticate(path: String, body: RequestBody, onResult: (Result<Account>) -> Unit) {
        val request = Request.Builder().url(BuildConfig.API_BASE_URL + API_V1 + path).post(body).build()
        call(request, { text ->
            val json = JSONObject(text)
            val token = json.optString("session_token")
            if (token.isEmpty()) throw IOException("the server returned no session token")
            Account(token = token, profile = Profile.parse(json))
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
                    else Result.failure(ApiException.from(response.code, body)),
                )
            }

            private fun deliver(result: Result<T>) {
                main.post { onResult(result) }
            }
        })
    }
}
