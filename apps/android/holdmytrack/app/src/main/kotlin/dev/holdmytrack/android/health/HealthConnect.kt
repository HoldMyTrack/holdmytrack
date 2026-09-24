package dev.holdmytrack.android.health

import android.content.Context
import android.content.Intent
import androidx.health.connect.client.HealthConnectClient
import androidx.health.connect.client.permission.HealthPermission
import androidx.health.connect.client.records.ExerciseSessionRecord

/**
 * What the app needs from Health Connect, and the state machine the user has to walk through
 * to grant it.
 *
 * The two permissions are asked for very differently, and that asymmetry is the whole reason
 * this file exists rather than a single "request permissions" call:
 *
 *  - `READ_EXERCISE` is an ordinary runtime permission. The system dialog grants it.
 *  - `READ_EXERCISE_ROUTES` **cannot be requested programmatically**. Phase 1 measured this
 *    directly: asking for it alongside the others grants the others and silently omits it —
 *    it never even acquires a `USER_SET` flag. The user grants it at Health Connect → the app
 *    → *Additional access* → *Access exercise routes* → *Always allow*, a screen two levels
 *    below the app's main permission page and not linked from it. So the onboarding flow has
 *    to send the user there and say what to tap; "grant permissions" is not one flow here.
 */
object HealthConnect {

    /** The session record itself — start, end, exercise type, and the route result. */
    val READ_EXERCISE: String = HealthPermission.getReadPermission(ExerciseSessionRecord::class)

    /**
     * A raw string because the library has no constant for it — it ships
     * `PERMISSION_WRITE_EXERCISE_ROUTE` but no read counterpart, which fits a permission the
     * platform will not let an app request in the first place.
     */
    const val READ_EXERCISE_ROUTES: String = "android.permission.health.READ_EXERCISE_ROUTES"

    /**
     * Not a third kind of data — a time window over the first two. Without it Health Connect
     * serves only the last 30 days, measured directly: a first sync on a real device returned
     * exactly 30 days of sessions and Health Connect's own screen named the cutoff date. An
     * app whose point is the accumulated shape of a history cannot work inside a 30-day
     * window, so it is requested alongside the sessions themselves.
     *
     * Requestable programmatically, unlike the routes permission, so it rides along in the
     * same dialog rather than needing its own trip through settings.
     */
    val READ_HISTORY: String = HealthPermission.PERMISSION_READ_HEALTH_DATA_HISTORY

    /** What the app can do right now, in the order the user has to resolve it. */
    enum class Readiness {
        /** No Health Connect on this device at all — nothing to offer the user. */
        UNAVAILABLE,

        /** Health Connect is present but needs updating before it can serve any request. */
        UPDATE_REQUIRED,

        /** Neither permission granted yet: the system dialog is the next step. */
        NEEDS_EXERCISE_PERMISSION,

        /** Sessions readable, routes not: the manual Health Connect settings step is next. */
        NEEDS_ROUTES_PERMISSION,

        /**
         * Sessions and routes readable, but only the last 30 days of them. Everything works —
         * it just cannot see far enough back — so this is its own state rather than a blocker,
         * and the user can sync anyway if they would rather not grant it.
         */
        NEEDS_HISTORY_PERMISSION,

        /** Everything granted. Syncing can run. */
        READY,
    }

    fun clientOrNull(context: Context): HealthConnectClient? =
        if (HealthConnectClient.getSdkStatus(context) == HealthConnectClient.SDK_AVAILABLE) {
            runCatching { HealthConnectClient.getOrCreate(context) }.getOrNull()
        } else {
            null
        }

    /**
     * Read fresh every time rather than cached: both permissions can change while the app is
     * on screen — the routes one *only* changes that way, since the user grants it in another
     * app's settings and comes back.
     */
    suspend fun readiness(context: Context): Readiness {
        when (HealthConnectClient.getSdkStatus(context)) {
            HealthConnectClient.SDK_UNAVAILABLE -> return Readiness.UNAVAILABLE
            HealthConnectClient.SDK_UNAVAILABLE_PROVIDER_UPDATE_REQUIRED -> return Readiness.UPDATE_REQUIRED
        }
        val client = clientOrNull(context) ?: return Readiness.UNAVAILABLE
        val granted = runCatching { client.permissionController.getGrantedPermissions() }
            .getOrElse { return Readiness.NEEDS_EXERCISE_PERMISSION }
        return when {
            READ_EXERCISE !in granted -> Readiness.NEEDS_EXERCISE_PERMISSION
            READ_EXERCISE_ROUTES !in granted -> Readiness.NEEDS_ROUTES_PERMISSION
            READ_HISTORY !in granted -> Readiness.NEEDS_HISTORY_PERMISSION
            else -> Readiness.READY
        }
    }

    /**
     * Health Connect's home screen — as close as a third-party app can get the user to the
     * exercise-routes toggle.
     *
     * **Not the app's own permission page, and not for want of trying.** The platform does
     * publish `HealthConnectManager.ACTION_MANAGE_HEALTH_PERMISSIONS`, which takes a package
     * name and would land exactly on HoldMyTrack's page — but the activity behind it is guarded by
     * `android.permission.GRANT_RUNTIME_PERMISSIONS`, a signature permission, so launching it
     * from here is a `SecurityException` that kills the process. Confirmed on a device, first
     * by the crash and then by querying the resolver directly.
     *
     * So the user arrives at the Health Connect home screen with four steps still to take, and
     * `SyncActivity` spells all four out. That is the cost of the routes permission not being
     * requestable, and there is no shorter path available to an ordinary app.
     *
     * The action is a raw string because the SDK exposes no constant for it — the one
     * `HealthConnectClient` used to offer is hidden from Kotlin callers now, and the androidx
     * pre-platform action resolves to nothing at all on API 34+, where Health Connect is part
     * of the platform. Both checked against the device's own resolver rather than assumed.
     */
    fun settingsIntent(): Intent = Intent(ACTION_HEALTH_HOME_SETTINGS)

    private const val ACTION_HEALTH_HOME_SETTINGS = "android.health.connect.action.HEALTH_HOME_SETTINGS"
}
