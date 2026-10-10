package dev.holdmytrack.android.imports

import android.content.Context
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.provider.OpenableColumns
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

/**
 * The files the Upload screen sends to `POST /v1/activities/upload` — the web's Upload menu
 * (`services/server/internal/web/static/upload.js`, root `docs/SPEC.md` FR-3.1–FR-3.4) on the
 * phone: `.gpx`, `.fit` and `.tcx`, up to 20 at a time. A `.zip` is refused with a note:
 * archives aren't imported (ADR-0039). One file at a time, in the order picked, each with its
 * progress.
 *
 * Process-wide rather than the screen's, so an upload carries on through a rotation or a trip to
 * another app; `UploadActivity` only draws [transfers] and [notes] and listens for changes. The
 * server takes it from there: once a file has gone, its jobs are in Sync's history, which polls
 * while they process.
 */
object FileImports {

    /** One picked file waiting or going. [sent] is the bytes sent so far, or -1 while queued. */
    class Transfer(val name: String, val size: Long) {
        var sent: Long = -1
            internal set
    }

    /** What a finished upload had to say that the history won't — already imported before,
     *  refused — or why a picked file wasn't sent. */
    data class Note(val text: String, val error: Boolean)

    /** The web's cap on files picked at once (root `docs/IMPLEMENTATION.md` §4.0.1). */
    const val MAX_FILES = 20

    /** The server's limits (`services/server/internal/httpapi/server.go`): checked here too, so
     *  a file it would refuse isn't sent first. */
    private const val MAX_FILE_BYTES = 16L shl 20

    private val ACCEPTED = Regex("""\.(gpx|fit|tcx)$""", RegexOption.IGNORE_CASE)
    private val ZIP = Regex("""\.zip$""", RegexOption.IGNORE_CASE)

    private class Pending(val uri: Uri, val transfer: Transfer)

    private val main = Handler(Looper.getMainLooper())
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val queue = ArrayDeque<Pending>()
    private var running = false

    private val _transfers = mutableListOf<Transfer>()
    val transfers: List<Transfer> get() = _transfers

    private val _notes = mutableListOf<Note>()
    val notes: List<Note> get() = _notes

    /** Bumped each time a file has gone, so the Upload screen knows to point to Sync's history. */
    var sentCount = 0
        private set

    private val listeners = mutableSetOf<() -> Unit>()

    fun addListener(listener: () -> Unit) {
        listeners += listener
    }

    fun removeListener(listener: () -> Unit) {
        listeners -= listener
    }

    private fun changed() = listeners.toList().forEach { it() }

    /**
     * Queues [uris] the way the web's `enqueue` does. More than [MAX_FILES] files are refused
     * together; a `.zip`, a file of any other kind, or one past the server's size limit is
     * named in a note rather than sent.
     */
    fun enqueue(context: Context, uris: List<Uri>) {
        val res = context.resources
        _notes.clear()
        val picked = uris.map { uri -> Picked(uri, nameOf(context, uri), sizeOf(context, uri)) }

        for (other in picked.filterNot { ACCEPTED.containsMatchIn(it.name) }) {
            val refusal = if (ZIP.containsMatchIn(other.name)) R.string.upload_zip_refused else R.string.upload_wrong_type
            _notes += Note(res.getString(refusal, other.name), error = true)
        }
        var files = picked.filter { ACCEPTED.containsMatchIn(it.name) }
        if (files.size > MAX_FILES) {
            _notes += Note(res.getString(R.string.upload_too_many, files.size), error = true)
            files = emptyList()
        }
        for (file in files) {
            if (file.size > MAX_FILE_BYTES) {
                _notes += Note(res.getString(R.string.upload_too_large, file.name, MAX_FILE_BYTES shr 20), error = true)
                continue
            }
            val transfer = Transfer(file.name, file.size)
            _transfers += transfer
            queue += Pending(file.uri, transfer)
        }
        changed()
        if (!running) sendNext(context.applicationContext)
    }

    private class Picked(val uri: Uri, val name: String, val size: Long)

    private fun sendNext(context: Context) {
        val next = queue.removeFirstOrNull()
        if (next == null) {
            running = false
            return
        }
        running = true
        val res = context.resources
        val transfer = next.transfer
        transfer.sent = 0
        changed()
        scope.launch {
            try {
                // Redrawn once a percent, not once a chunk: a 16 MiB file is 256 chunks.
                var shown = -1L
                val outcome = HoldMyTrackApi.uploadActivityFile(context.contentResolver, next.uri, transfer.name, transfer.size) { sent ->
                    val percent = if (transfer.size > 0) sent * 100 / transfer.size else sent shr 20
                    if (percent == shown) return@uploadActivityFile
                    shown = percent
                    main.post {
                        transfer.sent = sent
                        changed()
                    }
                }
                if (outcome.status == "already_processed") _notes += Note(res.getString(R.string.upload_already, transfer.name), error = false)
            } catch (e: Exception) {
                val reason = e.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.upload_network_error)
                _notes += Note(res.getString(R.string.upload_failed_transfer, transfer.name, reason), error = true)
            } finally {
                _transfers -= transfer
                sentCount++
                changed()
                sendNext(context)
            }
        }
    }

    /** The file's own name, as the picker or the app that shared it gives it. */
    private fun nameOf(context: Context, uri: Uri): String =
        query(context, uri, OpenableColumns.DISPLAY_NAME) { it.getString(0) } ?: uri.lastPathSegment.orEmpty()

    /** Its size, or -1 when the provider doesn't say (the upload is then sent without a length). */
    private fun sizeOf(context: Context, uri: Uri): Long =
        query(context, uri, OpenableColumns.SIZE) { if (it.isNull(0)) null else it.getLong(0) } ?: -1

    private fun <T> query(context: Context, uri: Uri, column: String, read: (android.database.Cursor) -> T?): T? =
        runCatching {
            context.contentResolver.query(uri, arrayOf(column), null, null, null)?.use { cursor ->
                if (cursor.moveToFirst()) read(cursor) else null
            }
        }.getOrNull()
}
