package dev.holdmytrack.android.timeline

import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import java.io.IOException
import java.util.UUID
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject

/**
 * The Google Maps Timeline import in progress — the web's `TimelineImportWindow.tsx` state
 * (root `docs/SPEC.md` FR-3.10), held process-wide so a rotation or a trip to another app
 * neither re-reads a large file nor cuts a send short. `TimelineImportActivity` draws it and
 * listens for changes.
 *
 * The file is read on the phone; the person narrows it to a range of days and a set of modes,
 * and only that is sent, a hundred activities to a request, all under one batch named after the
 * file, so the sync history shows the import as one.
 */
object TimelineImport {

    class Loaded(val file: String, val read: TimelineRead, val first: String, val last: String) {
        val modes: List<ModeSummary> = TimelineReader.summarize(read.segments)
    }

    class Sent(val total: Int) {
        var done = 0
        var enqueued = 0
        var already = 0
        var rejected = 0
    }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    /** The file being read, while it is. */
    var reading: String? = null
        private set
    var loaded: Loaded? = null
        private set
    var from = ""
        private set
    var to = ""
        private set
    var modes: Set<String> = emptySet()
        private set
    var sending = false
        private set
    var sent: Sent? = null
        private set
    var error: String? = null
        private set

    val selected: List<TimelineSegment>
        get() = loaded?.let { TimelineReader.select(it.read.segments, from, to, modes) }.orEmpty()

    val finished: Boolean
        get() = sent?.let { !sending && it.done == it.total } == true

    private val listeners = mutableSetOf<() -> Unit>()

    fun addListener(listener: () -> Unit) {
        listeners += listener
    }

    fun removeListener(listener: () -> Unit) {
        listeners -= listener
    }

    private fun changed() = listeners.toList().forEach { it() }

    /** Back to nothing chosen — once the screen closes, unless a send is still going. */
    fun reset() {
        if (sending || reading != null) return
        loaded = null
        sent = null
        error = null
        changed()
    }

    /** Reads [uri] off the main thread and selects all of it, every mode but [TimelineReader.MODES_OFF_BY_DEFAULT].
     *  Not while sending: a new file would replace what's being sent. */
    fun open(context: Context, uri: Uri) {
        if (sending || reading != null) return
        val app = context.applicationContext
        val res = app.resources
        val name = displayName(app, uri)
        reading = name
        error = null
        loaded = null
        sent = null
        changed()
        scope.launch {
            try {
                val read = withContext(Dispatchers.IO) {
                    val input = app.contentResolver.openInputStream(uri) ?: throw IOException("could not open the file")
                    input.use { TimelineFile.read(it) }
                }
                if (read.segments.isEmpty()) throw IllegalStateException(res.getString(R.string.timeline_empty))
                val first = read.segments.first().day
                val last = read.segments.maxOf { it.day }
                val file = Loaded(name, read, first, last)
                loaded = file
                from = first
                to = last
                modes = file.modes.map { it.mode }.filterNot { it in TimelineReader.MODES_OFF_BY_DEFAULT }.toSet()
            } catch (e: TimelineFormatException) {
                error = res.getString(
                    when (e.format) {
                        TimelineFormat.IOS -> R.string.timeline_unsupported_ios
                        TimelineFormat.TAKEOUT -> R.string.timeline_unsupported_takeout
                        null -> R.string.timeline_not_timeline
                    },
                )
            } catch (e: IllegalStateException) {
                error = e.message
            } catch (e: Exception) {
                // Malformed JSON, or a file that isn't text at all.
                error = res.getString(R.string.timeline_unreadable)
            } finally {
                reading = null
                changed()
            }
        }
    }

    fun setRange(from: String, to: String) {
        if (sending) return
        this.from = from
        this.to = to
        changed()
    }

    fun toggleMode(mode: String) {
        if (sending) return
        modes = if (mode in modes) modes - mode else modes + mode
        changed()
    }

    /** Sends [selected], a hundred to a request. Sending again is safe: what already arrived
     *  comes back as already imported. */
    fun send(context: Context) {
        val file = loaded ?: return
        val selection = selected
        if (selection.isEmpty() || sending) return
        val res = context.applicationContext.resources
        sending = true
        error = null
        val batch = UUID.randomUUID().toString()
        val progress = Sent(selection.size)
        sent = progress
        changed()
        scope.launch {
            try {
                for (chunk in TimelineReader.batches(selection)) {
                    val results = HoldMyTrackApi.syncActivities(
                        chunk.map(::syncJson),
                        HoldMyTrackApi.SOURCE_TIMELINE,
                        batch = batch,
                        batchTitle = file.file,
                    )
                    for (result in results) {
                        when (result.status) {
                            "enqueued" -> progress.enqueued++
                            "already_processed" -> progress.already++
                            else -> progress.rejected++
                        }
                    }
                    progress.done += chunk.size
                    changed()
                }
            } catch (e: Exception) {
                error = e.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.upload_network_error)
            } finally {
                sending = false
                changed()
            }
        }
    }

    /** One segment as `POST /v1/sync/activities` takes it — the web's `syncBatches` entry. */
    private fun syncJson(segment: TimelineSegment): JSONObject = JSONObject()
        .put("external_id", segment.id)
        .put("activity_type", segment.type)
        .put(
            "points",
            JSONArray(segment.points.map { JSONObject().put("lat", it.lat).put("lon", it.lon).put("time", it.time) }),
        )

    private fun displayName(context: Context, uri: Uri): String =
        runCatching {
            context.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
                if (cursor.moveToFirst()) cursor.getString(0) else null
            }
        }.getOrNull() ?: uri.lastPathSegment ?: "Timeline.json"
}
