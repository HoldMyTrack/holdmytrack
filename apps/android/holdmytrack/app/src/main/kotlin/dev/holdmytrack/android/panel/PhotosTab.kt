package dev.holdmytrack.android.panel

import android.annotation.SuppressLint
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Paint
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.text.Editable
import android.text.TextWatcher
import android.view.LayoutInflater
import android.view.View
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.widget.TooltipCompat
import androidx.core.view.isVisible
import com.google.android.material.button.MaterialButton
import com.google.android.material.slider.Slider
import com.google.android.material.textfield.TextInputEditText
import dev.holdmytrack.android.R
import dev.holdmytrack.android.map.PhotoMarkerItem
import dev.holdmytrack.android.map.PhotoMarkerOverlay
import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Photo
import dev.holdmytrack.android.net.PhotoNeedsPlaceException
import dev.holdmytrack.android.photos.DraftRow
import dev.holdmytrack.android.photos.NewPhoto
import dev.holdmytrack.android.photos.PhotoDraft
import dev.holdmytrack.android.photos.PhotoImages
import dev.holdmytrack.android.photos.PhotoPrep
import dev.holdmytrack.android.photos.PhotoTrack
import dev.holdmytrack.android.photos.PlaceAnchor
import dev.holdmytrack.android.photos.PreparedPhoto
import dev.holdmytrack.android.photos.TimedPoint
import dev.holdmytrack.android.photos.UnreadablePhotoException
import java.time.Instant
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.concurrent.Executors
import kotlin.math.roundToInt

/**
 * The Edit window's Photos tab (`docs/SPEC.md` FR-16.6), the web's `PhotosTab.tsx`: the activity's
 * photos as a list, each with Edit and Delete, and Add photos. Nothing here is written until the
 * window's Save ([save]); Cancel throws it all away. The tab edits [draft] ([PhotoDraft]), and the
 * list and the map ([onOverlay]) show the photos as Save would leave them.
 *
 * Every photo has a place on the track. Add prepares each picked photo ([PhotoPrep]) and asks
 * the server where it goes (`POST /v1/photos/place`, which stores nothing); one it can't place
 * waits at the top of the list with a slider, starting just after the photo picked before it,
 * until the user puts it somewhere (Place here) or removes it. Edit opens the same slider on a
 * photo, with its caption. The slider walks the track as drawn ([PhotoTrack]) and the photo
 * moves along the map with it; a place is kept as the moment at that point.
 */
class PhotosTab(
    private val root: View,
    /** Add photos: the window's owner opens the system photo picker and hands back [add]. */
    private val onPick: () -> Unit,
    /** How the map should show the draft, or null for the saved photos as they are. */
    private val onOverlay: (PhotoMarkerOverlay?) -> Unit,
    /** The draft changed — the window's tab label and dot follow it. */
    private val onChange: () -> Unit,
) {
    private val context = root.context
    private val res = context.resources
    private val main = Handler(Looper.getMainLooper())

    private val add: MaterialButton = root.findViewById(R.id.photos_add)
    private val status: TextView = root.findViewById(R.id.photos_status)
    private val error: TextView = root.findViewById(R.id.photos_error)
    private val failuresView: TextView = root.findViewById(R.id.photos_failures)
    private val note: TextView = root.findViewById(R.id.photos_note)
    private val scroll: MaxHeightScrollView = root.findViewById(R.id.photos_scroll)
    private val list: LinearLayout = root.findViewById(R.id.photos_list)
    private val waitingView: View = root.findViewById(R.id.photos_waiting)
    private val waitingThumb: ImageView = root.findViewById(R.id.photos_waiting_thumb)
    private val waitingName: TextView = root.findViewById(R.id.photos_waiting_name)
    private val waitingMore: TextView = root.findViewById(R.id.photos_waiting_more)
    private val waitingRemove: MaterialButton = root.findViewById(R.id.photos_waiting_remove)
    private val waitingPlace: MaterialButton = root.findViewById(R.id.photos_waiting_place)
    private val waitingSlider = PlaceSlider(root.findViewById(R.id.photos_waiting_slider))

    private var activity: Activity? = null
    private var photos: List<Photo> = emptyList()
    private var photosLoaded = false
    private var photosError: String? = null
    private var track: PhotoTrack? = null
    private var trackError: String? = null
    private var generation = 0

    /** The unsaved changes — what Save writes. */
    var draft = PhotoDraft()
        private set

    /** Any photo write landed — a later failure or a Cancel still reports it. */
    var photosSaved = false
        private set

    /** Whether this tab is the one showing — only then does it draw the draft on the map. */
    var active = false
        set(value) {
            field = value
            render()
        }

    /** The window is saving: nothing here changes meanwhile. */
    var busy = false
        set(value) {
            field = value
            render()
        }

    private var progress: Pair<Int, Int>? = null
    private val failures = ArrayList<String>()
    private var editingId: String? = null
    private var frozenOrder: List<String> = emptyList()

    /** The waiting photo's slider: which photo it's for, and where it is. */
    private var waitingAt: Pair<String, Double>? = null

    /** What each waiting photo was picked after, and which were removed rather than placed —
     *  what a waiting photo's slider starts from ([PhotoTrack.startFraction]). */
    private val anchors = HashMap<String, PlaceAnchor?>()
    private val removed = HashSet<String>()

    /** New photos' thumbnails, decoded once from what will be uploaded. */
    private val localThumbs = HashMap<String, Bitmap>()
    private val holders = HashMap<String, RowHolder>()

    init {
        add.setOnClickListener { if (!busy && progress == null) onPick() }
        waitingRemove.setOnClickListener { draft.waiting.firstOrNull()?.let(::removeNew) }
        waitingPlace.setOnClickListener {
            val photo = draft.waiting.firstOrNull() ?: return@setOnClickListener
            val at = waitingAt?.takeIf { it.first == photo.key } ?: return@setOnClickListener
            val t = track?.pointAt(at.second)?.t ?: return@setOnClickListener
            update(draft.withNew(photo.key) { it.copy(routeAt = t) })
        }
        waitingSlider.onMove = { fraction ->
            draft.waiting.firstOrNull()?.let { waitingAt = it.key to fraction }
            render()
        }
        // The web's min(360px, 45dvh).
        scroll.maxHeight = minOf((LIST_MAX_DP * res.displayMetrics.density).toInt(), (res.displayMetrics.heightPixels * 0.45).toInt())
    }

    /**
     * The tab first opened on [opened]: its track is read for the slider. The session lasts
     * until [close], so switching tabs loses nothing.
     */
    fun open(opened: Activity) {
        if (activity?.id == opened.id) {
            render()
            return
        }
        activity = opened
        track = null
        trackError = null
        val gen = generation
        HoldMyTrackApi.trackMetrics(opened.id) { result ->
            if (gen != generation) return@trackMetrics
            result.onSuccess { metrics ->
                if (metrics.points.isEmpty()) {
                    trackError = res.getString(R.string.photos_no_track)
                } else {
                    track = PhotoTrack(metrics.points.map { TimedPoint(it.lon, it.lat, it.timeS) })
                }
            }.onFailure { trackError = it.message?.takeIf { m -> m.isNotBlank() } ?: res.getString(R.string.edit_save_failed) }
            render()
        }
        render()
    }

    /** The activity's saved photos as the map has them, or why they couldn't be read. */
    fun setPhotos(saved: List<Photo>, failure: String?) {
        photos = saved
        photosLoaded = failure == null
        photosError = failure
        render()
    }

    /** How many photos Save would leave — the tab's count. */
    val count: Int
        get() = photos.size - draft.deleted.size + draft.added.size

    /** Why Save can't go ahead with the photos, or null. */
    fun invalid(): String? = when {
        draft.waiting.isNotEmpty() -> res.getString(R.string.photos_place_first)
        draft.captions.any { it.trim().length > MAX_CAPTION } -> res.getString(R.string.photos_caption_too_long, MAX_CAPTION)
        else -> null
    }

    /** The photos the system picker handed back, prepared and placed one at a time. */
    fun add(uris: List<Uri>) {
        val opened = activity ?: return
        if (uris.isEmpty() || busy || progress != null) return
        failures.clear()
        val gen = generation
        prepareNext(opened.id, uris, 0, previous = null, gen)
    }

    private fun prepareNext(activityId: String, uris: List<Uri>, index: Int, previous: PlaceAnchor?, gen: Int) {
        if (gen != generation) return
        if (index == uris.size) {
            progress = null
            render()
            return
        }
        progress = index to uris.size
        render()
        val uri = uris[index]
        val resolver = context.contentResolver
        PREPARE.execute {
            val name = PhotoPrep.displayName(resolver, uri)
            val prepared = runCatching { PhotoPrep.prepare(resolver, uri) }
            main.post {
                if (gen != generation) return@post
                prepared.onFailure { failure ->
                    fail(name, failure)
                    prepareNext(activityId, uris, index + 1, previous, gen)
                }.onSuccess { photo ->
                    HoldMyTrackApi.checkPhotoPlace(activityId, photo.exif) { placed ->
                        if (gen != generation) return@checkPhotoPlace
                        val key = "new-${System.currentTimeMillis()}-$index"
                        placed.onSuccess { at ->
                            addNew(key, name, photo, at.epochSecond)
                            prepareNext(activityId, uris, index + 1, PlaceAnchor.At(at.epochSecond), gen)
                        }.onFailure { failure ->
                            if (failure is PhotoNeedsPlaceException) {
                                anchors[key] = previous
                                addNew(key, name, photo, null)
                                prepareNext(activityId, uris, index + 1, PlaceAnchor.Waiting(key), gen)
                            } else {
                                fail(name, failure)
                                prepareNext(activityId, uris, index + 1, previous, gen)
                            }
                        }
                    }
                }
            }
        }
    }

    private fun addNew(key: String, name: String, prepared: PreparedPhoto, routeAt: Long?) {
        BitmapFactory.decodeByteArray(prepared.thumb.bytes, 0, prepared.thumb.bytes.size)?.let { localThumbs[key] = it }
        update(draft.withAdded(NewPhoto(key, name, prepared, routeAt)))
    }

    private fun fail(name: String, failure: Throwable) {
        val message = if (failure is UnreadablePhotoException) {
            res.getString(R.string.photos_unreadable)
        } else {
            failure.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.edit_save_failed)
        }
        failures += res.getString(R.string.photos_upload_failed, name, message)
        render()
    }

    /**
     * Writes the draft to [activityId], one request at a time: uploads (each at the place it
     * settled on), then moves and captions, then deletes. Each write that lands leaves the draft,
     * so a Save retried after a failure picks up where this stopped. [onProgress] counts the
     * writes; [onDone] gets the failure, or null once everything is written.
     */
    fun save(activityId: String, onProgress: (done: Int, total: Int) -> Unit, onDone: (Throwable?) -> Unit) {
        val snapshot = draft
        val total = snapshot.size
        var done = 0
        val steps = ArrayList<(next: () -> Unit, fail: (Throwable) -> Unit) -> Unit>()
        for (photo in snapshot.added) {
            steps += { next, fail ->
                val p = photo.prepared
                HoldMyTrackApi.uploadPhoto(activityId, p.file, p.thumb, p.exif, Instant.ofEpochSecond(photo.routeAt ?: 0), photo.caption.trim()) { result ->
                    result.onSuccess {
                        photosSaved = true
                        localThumbs.remove(photo.key)
                        draft = draft.withoutNew(photo.key)
                        next()
                    }.onFailure(fail)
                }
            }
        }
        for ((id, change) in snapshot.changed) {
            if (id in snapshot.deleted) continue
            steps += { next, fail ->
                HoldMyTrackApi.updatePhoto(id, change.caption?.trim(), change.routeAt?.let(Instant::ofEpochSecond)) { result ->
                    result.onSuccess {
                        photosSaved = true
                        draft = draft.withoutChange(id)
                        next()
                    }.onFailure(fail)
                }
            }
        }
        for (id in snapshot.deleted) {
            steps += { next, fail ->
                HoldMyTrackApi.deletePhoto(id) { result ->
                    result.onSuccess {
                        photosSaved = true
                        draft = draft.withoutDelete(id)
                        next()
                    }.onFailure(fail)
                }
            }
        }
        fun run(index: Int) {
            onProgress(done, total)
            if (index == steps.size) {
                render()
                onChange()
                onDone(null)
                return
            }
            steps[index]({
                done += 1
                run(index + 1)
            }) { failure ->
                render()
                onChange()
                onDone(failure)
            }
        }
        run(0)
    }

    /** The window closed: the draft, the track and the overlay go. */
    fun close() {
        generation += 1
        activity = null
        photos = emptyList()
        photosLoaded = false
        photosError = null
        track = null
        trackError = null
        draft = PhotoDraft()
        photosSaved = false
        active = false
        progress = null
        failures.clear()
        editingId = null
        waitingAt = null
        anchors.clear()
        removed.clear()
        localThumbs.clear()
        holders.clear()
        list.removeAllViews()
        onOverlay(null)
    }

    private fun update(next: PhotoDraft) {
        draft = next
        render()
        onChange()
    }

    private fun removeNew(photo: NewPhoto) {
        removed += photo.key
        localThumbs.remove(photo.key)
        if (editingId == photo.key) editingId = null
        update(draft.withoutNew(photo.key))
    }

    private fun setDeleted(id: String, deleted: Boolean) {
        if (deleted && editingId == id) editingId = null
        update(draft.withDeleted(id, deleted))
    }

    private fun render() {
        if (activity == null) return
        val t = track
        val waitingList = draft.waiting
        val waiting = waitingList.firstOrNull()
        // A waiting photo's slider starts just after the photo picked before it — worked out
        // when its turn comes, since the one before may only just have been placed.
        if (waiting != null && t != null && waitingAt?.first != waiting.key) {
            val placed = HashMap<String, Double?>()
            for (p in draft.added) p.routeAt?.let { placed[p.key] = t.fractionAt(it) }
            for (key in removed) placed[key] = null
            waitingAt = waiting.key to t.startFraction(anchors[waiting.key], placed, anchors)
        }
        if (waiting == null) waitingAt = null

        add.isEnabled = !busy && progress == null
        status.text = progress?.let { (done, total) -> res.getString(R.string.photos_preparing, done + 1, total) }
        status.isVisible = progress != null
        val shownError = photosError ?: trackError
        error.text = shownError
        error.isVisible = shownError != null
        failuresView.text = failures.joinToString("\n")
        failuresView.isVisible = failures.isNotEmpty()

        waitingView.isVisible = waiting != null
        if (waiting != null) {
            waitingThumb.setImageBitmap(localThumbs[waiting.key])
            waitingName.text = waiting.name
            waitingMore.text = if (waitingList.size > 1) {
                res.getQuantityString(R.plurals.photos_more_waiting, waitingList.size - 1, waitingList.size - 1)
            } else {
                null
            }
            val at = waitingAt?.takeIf { it.first == waiting.key }
            waitingSlider.bind(t, at?.second, busy)
            waitingRemove.isEnabled = !busy
            waitingPlace.isEnabled = !busy && t != null && at != null
        }

        val sorted = draft.rows(photos)
        // The list keeps its order while a row is open — re-sorting under a slider being
        // dragged would move the row out from under the finger — and takes up its new place
        // when the row closes.
        if (editingId == null) frozenOrder = sorted.map { it.id }
        val rows = if (editingId == null) sorted else {
            val byId = sorted.associateBy { it.id }
            frozenOrder.mapNotNull { byId[it] } + sorted.filter { it.id !in frozenOrder }
        }
        note.text = when {
            !photosLoaded && photosError == null -> res.getString(R.string.photos_loading)
            rows.isEmpty() && waiting == null && shownError == null -> res.getString(R.string.photos_empty)
            else -> null
        }
        note.isVisible = note.text.isNotEmpty()

        val wanted = rows.map { it.id }.toSet()
        holders.keys.filter { it !in wanted }.forEach { holders.remove(it) }
        rows.forEachIndexed { i, row ->
            val holder = holders.getOrPut(row.id) { RowHolder() }
            holder.bind(row, i)
            if (list.getChildAt(i) !== holder.view) {
                (holder.view.parent as? LinearLayout)?.removeView(holder.view)
                list.addView(holder.view, i)
            }
        }
        while (list.childCount > rows.size) list.removeViewAt(list.childCount - 1)

        renderOverlay(rows, waiting)
    }

    /** The map shows the photos as Save would leave them, while this tab shows. */
    private fun renderOverlay(rows: List<DraftRow>, waiting: NewPhoto?) {
        val t = track
        if (!active || t == null) {
            onOverlay(null)
            return
        }
        val upserts = ArrayList<PhotoMarkerItem>()
        for (row in rows) {
            if (row is DraftRow.Saved && (row.deleted || !row.changed)) continue
            val p = t.pointAt(t.fractionAt(row.routeAt))
            upserts += when (row) {
                is DraftRow.New -> PhotoMarkerItem(row.id, p.lon, p.lat, row.caption.ifEmpty { null }, thumb = localThumbs[row.id])
                is DraftRow.Saved -> PhotoMarkerItem(row.id, p.lon, p.lat, row.caption.ifEmpty { null }, thumbPath = row.photo.thumbUrl)
            }
        }
        val at = waitingAt
        if (waiting != null && at != null && at.first == waiting.key) {
            val p = t.pointAt(at.second)
            upserts += PhotoMarkerItem(waiting.key, p.lon, p.lat, null, thumb = localThumbs[waiting.key])
        }
        onOverlay(PhotoMarkerOverlay(upserts, draft.deleted.toSet(), waiting?.key ?: editingId))
    }

    /** A moment's time of day in the zone the activity was recorded in. */
    private fun clock(seconds: Long): String =
        DateTimeFormatter.ofLocalizedTime(FormatStyle.SHORT)
            .withLocale(res.configuration.locales[0])
            .withZone(PanelFormat.zone(activity?.timezone))
            .format(Instant.ofEpochSecond(seconds))

    /** A slider along the track (`include_photo_slider`) and the time at its point. [onMove]
     *  gets the user's moves, as a fraction of the way along. */
    private inner class PlaceSlider(view: View) {
        private val slider: Slider = view.findViewById(R.id.photo_slider)
        private val time: TextView = view.findViewById(R.id.photo_slider_time)
        private var dragging = false
        var onMove: (Double) -> Unit = {}

        init {
            slider.setLabelFormatter { value -> track?.let { clock(it.pointAt(value / STEPS.toDouble()).t) }.orEmpty() }
            slider.addOnChangeListener { _, value, fromUser -> if (fromUser) onMove(value / STEPS.toDouble()) }
            slider.addOnSliderTouchListener(object : Slider.OnSliderTouchListener {
                override fun onStartTrackingTouch(slider: Slider) {
                    dragging = true
                }

                override fun onStopTrackingTouch(slider: Slider) {
                    dragging = false
                }
            })
        }

        /** At [fraction] of [track], unless the finger has it. */
        fun bind(track: PhotoTrack?, fraction: Double?, busy: Boolean) {
            slider.isEnabled = track != null && fraction != null && !busy
            if (fraction == null) return
            if (!dragging) slider.value = (fraction * STEPS).roundToInt().coerceIn(0, STEPS).toFloat()
            time.text = track?.let { clock(it.pointAt(fraction).t) }
        }
    }

    /** One row of the list, kept for as long as its photo is listed, so an open row's slider and
     *  caption survive every render under the finger. */
    private inner class RowHolder {
        val view: View = LayoutInflater.from(context).inflate(R.layout.item_photo_row, list, false)
        private val thumb: ImageView = view.findViewById(R.id.photo_row_thumb)
        private val title: TextView = view.findViewById(R.id.photo_row_title)
        private val time: TextView = view.findViewById(R.id.photo_row_time)
        private val badge: TextView = view.findViewById(R.id.photo_row_badge)
        private val edit: MaterialButton = view.findViewById(R.id.photo_row_edit)
        private val delete: MaterialButton = view.findViewById(R.id.photo_row_delete)
        private val undo: MaterialButton = view.findViewById(R.id.photo_row_undo)
        private val editor: View = view.findViewById(R.id.photo_row_editor)
        private val caption: TextInputEditText = view.findViewById(R.id.photo_row_caption)
        private val done: View = view.findViewById(R.id.photo_row_done)
        private val slider = PlaceSlider(view.findViewById(R.id.photo_row_slider))
        private var row: DraftRow? = null
        private var thumbFor: String? = null

        init {
            edit.setOnClickListener {
                val id = row?.id ?: return@setOnClickListener
                editingId = if (editingId == id) null else id
                render()
                if (editingId == id) view.post { scroll.smoothScrollTo(0, view.top) }
            }
            delete.setOnClickListener {
                when (val r = row) {
                    is DraftRow.New -> removeNew(r.photo)
                    is DraftRow.Saved -> setDeleted(r.id, true)
                    null -> Unit
                }
            }
            undo.setOnClickListener { row?.let { setDeleted(it.id, false) } }
            done.setOnClickListener {
                editingId = null
                render()
            }
            slider.onMove = { fraction ->
                val t = track?.pointAt(fraction)?.t
                if (t != null) {
                    when (val r = row) {
                        is DraftRow.New -> update(draft.withNew(r.id) { it.copy(routeAt = t) })
                        is DraftRow.Saved -> update(draft.withChange(r.photo, routeAt = t))
                        null -> Unit
                    }
                }
            }
            caption.addTextChangedListener(object : TextWatcher {
                override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) = Unit
                override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) = Unit
                override fun afterTextChanged(s: Editable?) {
                    val text = s?.toString().orEmpty()
                    when (val r = row) {
                        is DraftRow.New -> if (text != r.caption) update(draft.withNew(r.id) { it.copy(caption = text) })
                        is DraftRow.Saved -> if (text != r.caption) update(draft.withChange(r.photo, caption = text))
                        null -> Unit
                    }
                }
            })
        }

        @SuppressLint("SetTextI18n")
        fun bind(next: DraftRow, index: Int) {
            row = next
            val deleted = next is DraftRow.Saved && next.deleted
            val label = next.caption.ifEmpty { res.getString(R.string.photos_photo_n, index + 1) }
            title.text = label
            title.paintFlags = if (deleted) title.paintFlags or Paint.STRIKE_THRU_TEXT_FLAG else title.paintFlags and Paint.STRIKE_THRU_TEXT_FLAG.inv()
            title.alpha = if (deleted) DELETED_ALPHA else 1f
            thumb.alpha = if (deleted) DELETED_ALPHA else 1f
            time.text = clock(next.routeAt)
            val badgeText = when {
                next is DraftRow.New -> R.string.photos_new
                deleted -> R.string.photos_deleted
                next is DraftRow.Saved && next.changed -> R.string.photos_changed
                else -> null
            }
            badge.isVisible = badgeText != null
            badgeText?.let(badge::setText)
            badge.setTextColor(context.getColor(if (deleted) R.color.hmt_danger else R.color.hmt_ink_secondary))

            val path = (next as? DraftRow.Saved)?.photo?.thumbUrl
            val thumbKey = path ?: next.id
            if (thumbFor != thumbKey) {
                thumbFor = thumbKey
                if (path == null) {
                    thumb.setImageBitmap(localThumbs[next.id])
                } else {
                    thumb.setImageBitmap(PhotoImages.cached(path))
                    PhotoImages.thumb(path) { loaded -> if (thumbFor == path) thumb.setImageBitmap(loaded) }
                }
            }

            val open = editingId == next.id
            view.setBackgroundResource(if (open) R.drawable.bg_photo_row_editing else 0)
            edit.isVisible = !deleted
            delete.isVisible = !deleted
            undo.isVisible = deleted
            edit.isEnabled = track != null && !busy
            delete.isEnabled = !busy
            undo.isEnabled = !busy
            edit.contentDescription = res.getString(R.string.photos_edit_one, label)
            delete.contentDescription = res.getString(R.string.photos_delete_one, label)
            TooltipCompat.setTooltipText(undo, res.getString(R.string.photos_undo_delete))
            editor.isVisible = open && track != null
            if (open) {
                track?.let { slider.bind(it, it.fractionAt(next.routeAt), busy) }
                if (caption.text?.toString() != next.caption) caption.setText(next.caption)
                caption.isEnabled = !busy
            }
        }
    }

    private companion object {
        /** Mirrors photos.go's maxPhotoCaptionLen. */
        const val MAX_CAPTION = 500
        /** The slider's steps — fine enough that a step on a long route is a few metres. */
        const val STEPS = 1000
        const val LIST_MAX_DP = 360
        const val DELETED_ALPHA = 0.45f

        /** Preparing photos is decoding and re-encoding them: off the main thread, one at a time. */
        val PREPARE = Executors.newSingleThreadExecutor()
    }
}
