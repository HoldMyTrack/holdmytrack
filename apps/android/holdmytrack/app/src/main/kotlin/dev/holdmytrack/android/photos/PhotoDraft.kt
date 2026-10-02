package dev.holdmytrack.android.photos

import dev.holdmytrack.android.net.Photo
import dev.holdmytrack.android.net.PhotoExif
import dev.holdmytrack.android.net.PhotoImage

/** A picked photo ready for `POST /v1/photos`: the resized copy, its thumbnail, and its EXIF. */
class PreparedPhoto(val file: PhotoImage, val thumb: PhotoImage, val exif: PhotoExif)

/** A picked photo, not yet uploaded. [routeAt] (epoch seconds) is null while it waits for the
 *  user to place it. */
data class NewPhoto(
    val key: String,
    val name: String,
    val prepared: PreparedPhoto,
    val routeAt: Long?,
    val caption: String = "",
)

/** A saved photo's pending changes; a field set is a change. */
data class PhotoChange(val routeAt: Long? = null, val caption: String? = null) {
    val isEmpty: Boolean
        get() = routeAt == null && caption == null
}

/** One row of the Photos tab's list: a saved photo as the draft would leave it, or a new one. */
sealed interface DraftRow {
    val id: String
    val routeAt: Long
    val caption: String

    data class Saved(
        override val id: String,
        val photo: Photo,
        override val routeAt: Long,
        override val caption: String,
        val deleted: Boolean,
        val changed: Boolean,
    ) : DraftRow

    data class New(override val id: String, val photo: NewPhoto, override val routeAt: Long, override val caption: String) : DraftRow
}

/**
 * The Photos tab's unsaved changes (`docs/SPEC.md` FR-16.6), the web's
 * `apps/web/src/ui/photoDraft.ts`: what the Edit window's one Save writes, after the activity's
 * fields and before its track edit, and what its Cancel throws away. Nothing here has reached
 * the server: a new photo is prepared on the phone and its place settled by the placement check
 * (`POST /v1/photos/place`) or by the user, and goes up only on Save.
 */
data class PhotoDraft(
    val added: List<NewPhoto> = emptyList(),
    val changed: Map<String, PhotoChange> = emptyMap(),
    val deleted: List<String> = emptyList(),
) {
    val isEmpty: Boolean
        get() = added.isEmpty() && changed.isEmpty() && deleted.isEmpty()

    /** How many writes Save will make. */
    val size: Int
        get() = added.size + changed.keys.count { it !in deleted } + deleted.size

    /** New photos still waiting for the user to place them — Save can't go ahead while there
     *  are any. */
    val waiting: List<NewPhoto>
        get() = added.filter { it.routeAt == null }

    /** Every caption Save would write, for its length check. */
    val captions: List<String>
        get() = added.map { it.caption } + changed.values.mapNotNull { it.caption }

    /** The photos as Save would leave them, in route order — saved ones (a deleted one kept,
     *  marked, so it can be brought back) and new ones with a place. Waiting ones aren't rows. */
    fun rows(photos: List<Photo>): List<DraftRow> {
        val rows = ArrayList<DraftRow>()
        for (photo in photos) {
            val change = changed[photo.id]
            rows += DraftRow.Saved(
                id = photo.id,
                photo = photo,
                routeAt = change?.routeAt ?: photo.routeAt.epochSecond,
                caption = change?.caption ?: photo.caption.orEmpty(),
                deleted = photo.id in deleted,
                changed = change != null,
            )
        }
        for (photo in added) {
            val at = photo.routeAt ?: continue
            rows += DraftRow.New(photo.key, photo, at, photo.caption)
        }
        // Stable, so photos at one moment keep the server's order, new ones after them.
        return rows.sortedBy { it.routeAt }
    }

    fun withAdded(photo: NewPhoto) = copy(added = added + photo)

    fun withNew(key: String, change: (NewPhoto) -> NewPhoto) =
        copy(added = added.map { if (it.key == key) change(it) else it })

    fun withoutNew(key: String) = copy(added = added.filter { it.key != key })

    /** A saved photo moved and/or captioned; a field changed back to what's saved is no
     *  change, and a photo with none left drops out of [changed]. */
    fun withChange(photo: Photo, routeAt: Long? = null, caption: String? = null): PhotoDraft {
        val current = changed[photo.id] ?: PhotoChange()
        var next = current.copy(routeAt = routeAt ?: current.routeAt, caption = caption ?: current.caption)
        if (next.routeAt == photo.routeAt.epochSecond) next = next.copy(routeAt = null)
        if (next.caption == photo.caption.orEmpty()) next = next.copy(caption = null)
        return copy(changed = if (next.isEmpty) changed - photo.id else changed + (photo.id to next))
    }

    fun withDeleted(id: String, deleted: Boolean) =
        copy(deleted = if (deleted) (this.deleted - id) + id else this.deleted - id)

    /** A saved photo's change written: it leaves the draft. */
    fun withoutChange(id: String) = copy(changed = changed - id)

    /** A saved photo's delete written: it leaves the draft, with whatever change it had. */
    fun withoutDelete(id: String) = copy(changed = changed - id, deleted = deleted - id)
}
