package dev.holdmytrack.android.sync

import android.content.Context
import android.util.Log
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * What the Sync tab sent and the server hasn't finished yet (`docs/SPEC.md` FR-3.6): the server
 * queues each activity and answers at once, and until the worker has done it the activity is
 * neither on the phone's list nor on the map. Each stays here, and on the Sync tab as "Adding to
 * your map…", until `POST /v1/sync/known` says its job has ended: known and not processing, it
 * has landed — [addListener]'s map reads its list and tiles again; in neither, its job failed —
 * it goes back on the list, a recording back in the phone's store, since the phone held its only
 * copy.
 *
 * Process-wide rather than the tab's, like `imports/FileImports`: the tab's reads and sends stop
 * when it leaves the screen (Health Connect's routes can't be read in the background), but this
 * reads nothing from Health Connect, so it carries on while the map is shown instead. It asks
 * every [POLL_MS] while anything is here and anyone is listening, and stops when either isn't.
 */
object SyncProgress {

    private const val TAG = "SyncProgress"
    private const val POLL_MS = 2_000L

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    /** The account these were sent from: another one signed in doesn't see them. */
    private var account: String? = null
    private val inFlight = LinkedHashMap<String, Candidate>()
    private val failed = mutableListOf<Candidate>()
    private var store: RecordedActivityStore? = null
    private var poll: Job? = null

    /** Sent, not on the map yet. */
    val candidates: List<Candidate>
        get() = if (forThisAccount()) inFlight.values.toList() else emptyList()

    fun isProcessing(key: String) = forThisAccount() && key in inFlight

    /** Listeners: [onChange] when the set changes, [onLanded] when any of it reached the map. */
    class Listener(val onChange: () -> Unit, val onLanded: () -> Unit)

    private val listeners = mutableSetOf<Listener>()

    fun addListener(listener: Listener) {
        listeners += listener
        startPolling()
    }

    fun removeListener(listener: Listener) {
        listeners -= listener
        if (listeners.isEmpty()) {
            poll?.cancel()
            poll = null
        }
    }

    /** [sent] has been taken by the server — or the server says it's still at work on it. */
    fun add(context: Context, sent: Collection<Candidate>) {
        if (sent.isEmpty()) return
        if (!forThisAccount()) clear()
        account = Session.email
        if (store == null) store = RecordedActivityStore(context.applicationContext)
        var added = false
        for (candidate in sent) if (inFlight.put(candidate.key, candidate) == null) added = true
        if (added) {
            changed()
            startPolling()
        }
    }

    /** What failed since this was last asked — once each. */
    fun takeFailed(): List<Candidate> {
        if (!forThisAccount()) return emptyList()
        return failed.toList().also { failed.clear() }
    }

    private fun forThisAccount() = account != null && account == Session.email

    private fun clear() {
        inFlight.clear()
        failed.clear()
    }

    private fun changed() = listeners.toList().forEach { it.onChange() }

    private fun startPolling() {
        if (poll?.isActive == true || listeners.isEmpty() || inFlight.isEmpty()) return
        poll = scope.launch {
            while (inFlight.isNotEmpty() && listeners.isNotEmpty()) {
                delay(POLL_MS)
                check()
            }
            poll = null
        }
    }

    private suspend fun check() {
        if (!forThisAccount()) {
            clear()
            changed()
            return
        }
        var landed = false
        var changed = false
        for ((source, sent) in inFlight.values.groupBy { it.source }) {
            val answer = try {
                HoldMyTrackApi.syncKnown(source, sent.map { it.externalId })
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                // A dropped read decides nothing: the next one asks again.
                Log.w(TAG, "could not ask what's still processing", e)
                continue
            }
            for (candidate in sent) {
                if (candidate.externalId in answer.processing) continue
                inFlight.remove(candidate.key)
                changed = true
                if (candidate.externalId in answer.known) {
                    landed = true
                } else {
                    failed += candidate
                    (candidate.origin as? Recording)?.let { restore(it) }
                }
            }
        }
        if (landed) listeners.toList().forEach { it.onLanded() }
        if (changed) changed()
    }

    /** A recording the server couldn't take is the phone's again: it was deleted once sent. */
    private suspend fun restore(recording: Recording) {
        try {
            store?.insert(recording.record)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Log.w(TAG, "could not put a failed recording back", e)
        }
    }
}
