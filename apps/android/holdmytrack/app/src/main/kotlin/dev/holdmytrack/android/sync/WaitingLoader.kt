package dev.holdmytrack.android.sync

import android.content.Context
import dev.holdmytrack.android.R
import dev.holdmytrack.android.health.HealthConnect
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.sync.HealthConnectCard.Companion.isReadable
import java.time.Instant
import kotlinx.coroutines.CancellationException

/**
 * Load (`apps/android/docs/SPEC.md` FR-3.2): what Health Connect has waiting, read now
 * and kept ([WaitingStore]) until the next one. Health Connect is read only here, and only when
 * the user asks — the Sync tab's Load — or once by itself after it's switched on
 * ([SyncSources.firstLoadPending]), on Settings › Sync or the Sync tab, whichever is in front.
 *
 * The sessions with a route from the last three months ([SyncCandidates.readHealthConnect]), less
 * those the account already has (`POST /v1/sync/known`) — so a reinstall or a second phone
 * agrees, and an activity deleted on the web is offered again. Those the server is still adding
 * are listed as on their way ([SyncProgress]). A failure keeps what was kept before.
 *
 * **Foreground only** (`docs/IMPLEMENTATION.md` §4.0): its caller cancels it when its screen
 * leaves, since Health Connect won't hand over routes in the background.
 */
class WaitingLoader(private val context: Context) {

    sealed interface Result {
        /** A whole read, kept: the sessions waiting, when, and how many routes were refused. */
        class Loaded(val sessions: List<Candidate>, val checkedAt: Instant, val refused: Int) : Result

        /** Health Connect or the server couldn't be read; [reason] says why. Nothing changed. */
        class Failed(val reason: String) : Result

        /** Nothing to read: the demo account, Health Connect switched off or not readable yet. */
        data object Unreadable : Result
    }

    suspend fun load(): Result {
        if (Session.isDemo) return Result.Unreadable
        val sources = SyncSources(context, Session.email)
        if (!sources.healthConnect || !HealthConnect.readiness(context).isReadable()) return Result.Unreadable
        val client = HealthConnect.clientOrNull(context) ?: return Result.Unreadable
        return try {
            val read = SyncCandidates.readHealthConnect(client)
            val ids = read.sessions.map { it.externalId }
            val answer = if (ids.isEmpty()) null else HoldMyTrackApi.syncKnown(HoldMyTrackApi.SOURCE_HEALTH_CONNECT, ids)
            val known = answer?.known.orEmpty()
            // Sent before, from here or another phone, and not on the map yet: on its way.
            answer?.processing?.takeIf { it.isNotEmpty() }?.let { processing ->
                SyncProgress.add(context, read.sessions.filter { it.externalId in processing })
            }
            val waiting = read.sessions.filter { it.externalId !in known }
            // A hidden session that's gone — synced from another phone, aged out of the window —
            // needn't be remembered; only after a whole read, since one missing from a partial
            // read may still be there. Hidden recordings are the Sync tab's to prune.
            if (read.refused == 0) {
                val hidden = HiddenCandidates(context, Session.email)
                val recordedPrefix = HoldMyTrackApi.SOURCE_RECORDED + ":"
                hidden.retainOnly(waiting.map { it.key }.toSet() + hidden.all().filter { it.startsWith(recordedPrefix) })
            }
            val now = Instant.now()
            WaitingStore(context).replace(waiting, now, read.refused)
            sources.firstLoadPending = false
            Result.Loaded(waiting, now, read.refused)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Result.Failed(context.getString(R.string.sync_read_failed, e.message.orEmpty()))
        }
    }
}