package dev.holdmytrack.android.imports

import android.content.Context
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.provider.OpenableColumns
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.UnpackedArchive
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

/**
 * The files the Upload screen sends to `POST /v1/activities/upload` — the web's Upload menu
 * (`services/server/internal/web/static/upload.js`, root `docs/SPEC.md` FR-3.1–FR-3.4) on the
 * phone: `.gpx`, `.fit`, `.tcx` and `.zip`, a Google Takeout export being a `.zip` the server
 * recognizes. One file at a time, in the order picked, each with its progress.
 *
 * Process-wide rather than the screen's, so an upload carries on through a rotation or a trip to
 * another app; `UploadActivity` only draws [transfers] and [notes] and listens for changes. The
 * server takes it from there: once a file has gone, its jobs are in Sync's history, which polls
 * while they process. An archive is unpacked in the background (root `docs/IMPLEMENTATION.md`
 * §4.0.1), so what its files came to arrives later: this polls `GET /v1/uploads/active` while
 * one it sent is unpacking, and adds the same notes then.
 */
object FileImports {

    /** One picked file waiting or going. [sent] is the bytes sent so far, or -1 while queued. */
    class Transfer(val name: String, val size: Long) {
        var sent: Long = -1
            internal set
    }

    /** What a finished upload had to say that the history won't — already imported before,
     *  skipped inside an archive, refused — or why a picked file wasn't sent. */
    data class Note(val text: String, val error: Boolean)

    /** The web's cap on files picked one by one; a `.zip` bypasses it (root
     *  `docs/IMPLEMENTATION.md` §4.0.1). */
    const val MAX_PLAIN_FILES = 20

    /** The server's limits (`services/server/internal/httpapi/server.go`): checked here too, so
     *  a file it would refuse isn't sent first. */
    private const val MAX_FILE_BYTES = 64L shl 20
    private const val MAX_ZIP_BYTES = 512L shl 20

    private val ACCEPTED = Regex("""\.(gpx|fit|tcx|zip)$""", RegexOption.IGNORE_CASE)
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

    /** Archives sent and still being unpacked, by batch: their file names, for the notes. */
    private val unpacking = mutableMapOf<String, String>()
    private var polling = false

    /** How often [unpacking] is checked on: the web's Upload menu's pace while something is in
     *  flight. */
    private const val UNPACK_POLL_MS = 2_000L

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
     * Queues [uris] the way the web's `enqueue` does. More than [MAX_PLAIN_FILES] files picked
     * one by one are refused in favour of a `.zip`; a file of any other kind, or past the
     * server's size limit, is named in a note rather than sent.
     */
    fun enqueue(context: Context, uris: List<Uri>) {
        val res = context.resources
        _notes.clear()
        val picked = uris.map { uri -> Picked(uri, nameOf(context, uri), sizeOf(context, uri)) }

        for (other in picked.filterNot { ACCEPTED.containsMatchIn(it.name) }) {
            _notes += Note(res.getString(R.string.upload_wrong_type, other.name), error = true)
        }
        var files = picked.filter { ACCEPTED.containsMatchIn(it.name) }
        val plain = files.filterNot { ZIP.containsMatchIn(it.name) }
        if (plain.size > MAX_PLAIN_FILES) {
            _notes += Note(res.getString(R.string.upload_too_many, plain.size), error = true)
            files = files.filter { ZIP.containsMatchIn(it.name) }
        }
        for (file in files) {
            val limit = if (ZIP.containsMatchIn(file.name)) MAX_ZIP_BYTES else MAX_FILE_BYTES
            if (file.size > limit) {
                _notes += Note(res.getString(R.string.upload_too_large, file.name, limit shr 20), error = true)
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
                // Redrawn once a percent, not once a chunk: a 512 MiB archive is 8,000 chunks.
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
                if (outcome.status == "zip_accepted" && outcome.batch.isNotEmpty()) {
                    unpacking[outcome.batch] = transfer.name
                    pollUnpacking(context)
                }
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

    /** Checks every [UNPACK_POLL_MS] for the archives in [unpacking] until none is left, noting
     *  what each one's files came to. A failed check just tries again. */
    private fun pollUnpacking(context: Context) {
        if (polling) return
        polling = true
        main.postDelayed({
            HoldMyTrackApi.unpackedArchives { result ->
                polling = false
                result.getOrNull()?.forEach { unpacked(context, it) }
                if (unpacking.isNotEmpty()) pollUnpacking(context)
            }
        }, UNPACK_POLL_MS)
    }

    private fun unpacked(context: Context, archive: UnpackedArchive) {
        val name = unpacking.remove(archive.batch) ?: return
        val res = context.resources
        if (archive.error.isNotEmpty()) _notes += Note(res.getString(R.string.upload_failed_transfer, name, archive.error), error = true)
        if (archive.already > 0) _notes += Note(res.getString(R.string.upload_already_in, name, archive.already), error = false)
        if (archive.skipped > 0) _notes += Note(res.getString(R.string.upload_skipped, name, archive.skipped), error = false)
        if (archive.truncated) _notes += Note(res.getString(R.string.upload_truncated, name), error = false)
        changed()
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
