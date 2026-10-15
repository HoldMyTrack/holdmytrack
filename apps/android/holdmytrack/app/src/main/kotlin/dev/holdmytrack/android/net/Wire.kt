package dev.holdmytrack.android.net

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

// The API's request and response bodies as they travel, one class each, named after the JSON
// they read or write — the server's snake_case through @SerialName. HoldMyTrackApi maps each
// response to the app's own class. Every class here is checked against the API contract,
// services/server/api (WireContractTest): a field the server renames, drops or starts sending as
// null fails that test rather than a decode on someone's phone.
//
// A response class lists only the fields the app reads. Rules the test holds them to:
// - A field with no default and not nullable must be one the contract requires and never null.
// - A field the contract makes nullable, or doesn't require, is nullable or has a default.
// - Its JSON type is the contract's: a String a string, Int and Long an integer, Double a number.

/** Decodes responses and encodes requests. Unknown keys are skipped, so a field the server adds
 *  breaks no version of the app; a null is left out of a request rather than sent. */
internal val apiJson = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
}

// Errors (ADR-0045)

@Serializable
internal data class ErrorJson(val error: String? = null, val message: String = "")

// Accounts and sessions

@Serializable
internal data class AccountJson(
    val email: String,
    val isDemo: Boolean,
    @SerialName("email_verified") val emailVerified: Boolean,
    @SerialName("display_name") val displayName: String,
    val country: String,
    val timezone: String,
    val locale: String,
    @SerialName("avatar_url") val avatarUrl: String,
)

/** The token half of a session response; the account half is [AccountJson], read from the same
 *  body. */
@Serializable
internal data class SessionTokenJson(@SerialName("session_token") val sessionToken: String)

@Serializable
internal data class MessageJson(val message: String)

@Serializable
internal data class ProvidersJson(
    val google: Boolean,
    val facebook: Boolean,
    @SerialName("google_client_id") val googleClientId: String = "",
)

@Serializable
internal data class SettingsOptionJson(val value: String, val label: String)

@Serializable
internal data class TimezoneGroupJson(val region: String, val label: String, val options: List<SettingsOptionJson>)

@Serializable
internal data class SettingsOptionsJson(
    val countries: List<SettingsOptionJson>,
    val timezones: List<TimezoneGroupJson>,
    val languages: List<SettingsOptionJson>,
)

@Serializable
internal data class ExportPartJson(val n: Int, val size: Long, val name: String, val url: String)

@Serializable
internal data class ExportStatusJson(
    val state: String,
    @SerialName("expires_at") val expiresAt: String? = null,
    @SerialName("total_size") val totalSize: Long = 0,
    val parts: List<ExportPartJson> = emptyList(),
)

@Serializable
internal data class CredentialsJson(val email: String, val password: String, val timezone: String? = null)

@Serializable
internal data class GoogleTokenJson(@SerialName("id_token") val idToken: String, val tz: String)

@Serializable
internal data class HandoffJson(val code: String, val verifier: String)

@Serializable
internal data class SettingsJson(
    @SerialName("display_name") val displayName: String,
    val country: String,
    val timezone: String,
    val locale: String,
)

@Serializable
internal data class EmailJson(val email: String)

// Activities

@Serializable
internal data class StoryRefJson(val id: String, val name: String)

@Serializable
internal data class ActivityRowJson(
    val id: String,
    @SerialName("started_at") val startedAt: String,
    val timezone: String,
    @SerialName("activity_type") val activityType: String,
    val name: String?,
    @SerialName("distance_meters") val distanceMeters: Double?,
    @SerialName("duration_seconds") val durationSeconds: Long?,
    val description: String?,
    val bbox: List<Double>?,
    val pending: Boolean,
    val edited: Boolean,
    val private: Boolean,
    val stories: List<StoryRefJson>,
)

@Serializable
internal data class ActivitiesJson(val activities: List<ActivityRowJson>)

@Serializable
internal data class ActivityPatchJson(
    @SerialName("activity_type") val activityType: String,
    val name: String,
    val description: String,
)

@Serializable
internal data class HistogramBucketJson(
    val date: String,
    val count: Int,
    @SerialName("distance_meters") val distanceMeters: Double,
)

@Serializable
internal data class HistogramJson(val buckets: List<HistogramBucketJson>, val earliest: String? = null)

@Serializable
internal data class CoverageStatusJson(
    val rendering: Boolean,
    val version: Long,
    @SerialName("tile_version") val tileVersion: String,
)

@Serializable
internal data class TrackMetricPointJson(
    val lon: Double,
    val lat: Double,
    @SerialName("speed_mps") val speedMps: Double,
    @SerialName("time_s") val timeS: Long,
)

@Serializable
internal data class TrackMetricsJson(
    @SerialName("activity_id") val activityId: String,
    val points: List<TrackMetricPointJson>,
)

/** A track edit both ways: what the editor opens with and what Apply sends. Times are Unix ms; a
 *  pair is a two-element array, and [move] is keyed by a time, which JSON carries as a string. */
@Serializable
internal data class TrackEditJson(
    val keep: List<Long>? = null,
    val remove: List<List<Long>>? = null,
    val drop: List<Long>? = null,
    val move: Map<String, List<Double>>? = null,
)

/** [points] are `[lon, lat, t]`, t in Unix ms. */
@Serializable
internal data class TrackPointsJson(val points: List<List<Double>>, val edit: TrackEditJson?)

@Serializable
internal data class TrackEditRequestJson(
    val edit: TrackEditJson?,
    @SerialName("split_at") val splitAt: Long? = null,
    @SerialName("allow_hidden") val allowHidden: Boolean = false,
)

@Serializable
internal data class OverlapSpanJson(val key: String, val start: String, val end: String)

@Serializable
internal data class OverlapsRequestJson(val spans: List<OverlapSpanJson>)

@Serializable
internal data class OverlapJson(
    val key: String,
    @SerialName("activity_id") val activityId: String,
    val name: String,
    @SerialName("activity_type") val activityType: String,
    @SerialName("started_at") val startedAt: String,
    val timezone: String?,
)

@Serializable
internal data class OverlapsJson(val overlaps: List<OverlapJson>)

@Serializable
internal data class UploadResultJson(val status: String)

// Sync

/** One point of a synced activity. [elevationM] is left out when unknown: no altitude is not
 *  sea level. */
@Serializable
internal data class SyncPointJson(
    val lat: Double,
    val lon: Double,
    @SerialName("elevation_m") val elevationM: Double? = null,
    val time: String,
)

@Serializable
internal data class SyncActivityJson(
    @SerialName("external_id") val externalId: String,
    @SerialName("activity_type") val activityType: String,
    val name: String? = null,
    val description: String? = null,
    val points: List<SyncPointJson>,
)

@Serializable
internal data class SyncActivitiesRequestJson(val source: String, val activities: List<SyncActivityJson>)

@Serializable
internal data class SyncResultJson(
    @SerialName("external_id") val externalId: String,
    val status: String,
    val error: String = "",
)

@Serializable
internal data class SyncResultsJson(val results: List<SyncResultJson>)

@Serializable
internal data class SyncKnownRequestJson(val source: String, @SerialName("external_ids") val externalIds: List<String>)

@Serializable
internal data class SyncKnownJson(val known: List<String>, val processing: List<String>)

// Private locations

@Serializable
internal data class PrivateLocationJson(
    val id: String,
    val name: String,
    val lon: Double,
    val lat: Double,
    @SerialName("radius_m") val radiusM: Int,
)

@Serializable
internal data class PrivateLocationInputJson(
    val name: String,
    val lon: Double,
    val lat: Double,
    @SerialName("radius_m") val radiusM: Int,
)

// Stories

@Serializable
internal data class StoryTypeStatsJson(
    @SerialName("activity_type") val activityType: String,
    val count: Int,
    @SerialName("distance_meters") val distanceMeters: Double,
    @SerialName("moving_seconds") val movingSeconds: Long,
)

@Serializable
internal data class StoryStatsJson(
    val count: Int,
    @SerialName("distance_meters") val distanceMeters: Double,
    @SerialName("moving_seconds") val movingSeconds: Long,
    @SerialName("by_type") val byType: List<StoryTypeStatsJson>,
)

@Serializable
internal data class StoryJson(
    val id: String,
    val name: String,
    val description: String?,
    val from: String?,
    @SerialName("activity_ids") val activityIds: List<String>,
    val stats: StoryStatsJson,
)

@Serializable
internal data class StoriesJson(val stories: List<StoryJson>)

/** A new Story, with its first [activityIds]. */
@Serializable
internal data class StoryCreateJson(
    val name: String,
    val description: String,
    @SerialName("activity_ids") val activityIds: List<String>,
)

/** A rename: both fields every time, an empty description clearing it. */
@Serializable
internal data class StoryInputJson(val name: String, val description: String)

@Serializable
internal data class StoryActivitiesJson(@SerialName("activity_ids") val activityIds: List<String>)

@Serializable
internal data class StorySendJson(
    val id: String,
    @SerialName("story_name") val storyName: String,
    val from: String,
    @SerialName("activity_count") val activityCount: Int,
)

@Serializable
internal data class StoryCopyingJson(@SerialName("story_name") val storyName: String, val from: String)

@Serializable
internal data class StoryInboxJson(val sends: List<StorySendJson>, val copying: List<StoryCopyingJson>)

// Photos

@Serializable
internal data class PhotoJson(
    val id: String,
    @SerialName("activity_id") val activityId: String,
    @SerialName("taken_at") val takenAt: String?,
    @SerialName("route_at") val routeAt: String,
    val timezone: String,
    val lon: Double?,
    val lat: Double?,
    val caption: String?,
    val width: Int,
    val height: Int,
    val url: String,
    @SerialName("thumb_url") val thumbUrl: String,
)

@Serializable
internal data class PhotosJson(val photos: List<PhotoJson>)

@Serializable
internal data class PhotoPlaceRequestJson(
    @SerialName("activity_id") val activityId: String,
    @SerialName("taken_at") val takenAt: String? = null,
    @SerialName("taken_local") val takenLocal: String? = null,
    val lat: Double? = null,
    val lon: Double? = null,
)

@Serializable
internal data class PhotoPlaceJson(@SerialName("route_at") val routeAt: String)

/** A field left null is left out, and so isn't changed. */
@Serializable
internal data class PhotoPatchJson(val caption: String? = null, @SerialName("route_at") val routeAt: String? = null)

// Spots

@Serializable
internal data class SpotJson(
    val id: Long,
    val category: String,
    val name: String? = null,
    val address: String? = null,
    val description: String? = null,
    val inscription: String? = null,
    val memorial: String? = null,
    @SerialName("start_date") val startDate: String? = null,
    val wikipedia: String? = null,
    val lon: Double,
    val lat: Double,
)

@Serializable
internal data class SpotsInAreaJson(val spots: List<SpotJson>, val total: Int)

/** A GeoJSON MultiPolygon: polygons, each rings, each `[lon, lat]` points. */
@Serializable
internal data class MultiPolygonJson(val coordinates: List<List<List<List<Double>>>>)

/** `GET /v1/spots/{id}`: the place's own fields beside its area, in one object. */
@Serializable
internal data class SpotDetailJson(
    val id: Long,
    val category: String,
    val name: String? = null,
    val address: String? = null,
    val description: String? = null,
    val inscription: String? = null,
    val memorial: String? = null,
    @SerialName("start_date") val startDate: String? = null,
    val wikipedia: String? = null,
    val lon: Double,
    val lat: Double,
    val area: MultiPolygonJson,
    @SerialName("captured_at") val capturedAt: String? = null,
)

@Serializable
internal data class SpotCaptureJson(@SerialName("spot_id") val spotId: Long, @SerialName("captured_at") val capturedAt: String)

@Serializable
internal data class SpotCapturesJson(val captures: List<SpotCaptureJson>)

@Serializable
internal data class SpotCaptureRequestJson(val lat: Double, val lon: Double)