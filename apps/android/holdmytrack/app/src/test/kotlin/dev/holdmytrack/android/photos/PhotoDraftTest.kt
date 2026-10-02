package dev.holdmytrack.android.photos

import dev.holdmytrack.android.net.Photo
import dev.holdmytrack.android.net.PhotoExif
import dev.holdmytrack.android.net.PhotoImage
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class PhotoDraftTest {

    private fun saved(id: String, routeAt: Long, caption: String? = null) = Photo(
        id = id,
        activityId = "a1",
        takenAt = null,
        routeAt = Instant.ofEpochSecond(routeAt),
        lon = 0.0,
        lat = 0.0,
        caption = caption,
        width = 2048,
        height = 1365,
        url = "/v1/photos/$id",
        thumbUrl = "/v1/photos/$id/thumb",
    )

    private val image = PhotoImage(ByteArray(0), "image/webp")
    private fun new(key: String, routeAt: Long?) = NewPhoto(key, "$key.jpg", PreparedPhoto(image, image, PhotoExif()), routeAt)

    private val photos = listOf(saved("p1", 100, "Start"), saved("p2", 300))

    @Test
    fun rowsAreInRouteOrderWithNewOnesAmongThemAndWaitingOnesLeftOut() {
        val draft = PhotoDraft().withAdded(new("n1", 200)).withAdded(new("n2", null))
        assertEquals(listOf("p1", "n1", "p2"), draft.rows(photos).map { it.id })
        assertEquals(listOf("n2"), draft.waiting.map { it.key })
    }

    @Test
    fun aMovedPhotoTakesItsNewPlaceInTheOrder() {
        val draft = PhotoDraft().withChange(photos[0], routeAt = 400)
        val rows = draft.rows(photos)
        assertEquals(listOf("p2", "p1"), rows.map { it.id })
        assertTrue((rows[1] as DraftRow.Saved).changed)
    }

    @Test
    fun aChangeBackToWhatsSavedIsNoChange() {
        val moved = PhotoDraft().withChange(photos[0], routeAt = 150)
        assertFalse(moved.isEmpty)
        assertTrue(moved.withChange(photos[0], routeAt = 100).isEmpty)
        val captioned = PhotoDraft().withChange(photos[0], caption = "Start!")
        assertTrue(captioned.withChange(photos[0], caption = "Start").isEmpty)
        // An empty caption on a photo with none is no change either.
        assertTrue(PhotoDraft().withChange(photos[1], caption = "").isEmpty)
    }

    @Test
    fun aMoveAndACaptionAreOneChange() {
        val draft = PhotoDraft().withChange(photos[1], routeAt = 250).withChange(photos[1], caption = "Lake")
        assertEquals(PhotoChange(routeAt = 250, caption = "Lake"), draft.changed["p2"])
        assertEquals(1, draft.size)
    }

    @Test
    fun aDeletedPhotoStaysListedMarkedAndCanBeKept() {
        val draft = PhotoDraft().withDeleted("p2", true)
        assertTrue((draft.rows(photos).single { it.id == "p2" } as DraftRow.Saved).deleted)
        assertTrue(draft.withDeleted("p2", false).isEmpty)
    }

    @Test
    fun saveWritesEachAddChangeAndDeleteOnceAndAChangedThenDeletedPhotoOnlyAsADelete() {
        val draft = PhotoDraft()
            .withAdded(new("n1", 200))
            .withChange(photos[0], caption = "Begin")
            .withChange(photos[1], routeAt = 250)
            .withDeleted("p2", true)
        assertEquals(3, draft.size)
        // Each write that lands leaves the draft, so a retried Save starts from the next.
        val afterUpload = draft.withoutNew("n1")
        val afterChange = afterUpload.withoutChange("p1")
        val afterDelete = afterChange.withoutDelete("p2")
        assertEquals(listOf(2, 1, 0), listOf(afterUpload.size, afterChange.size, afterDelete.size))
        assertTrue(afterDelete.isEmpty)
    }

    @Test
    fun captionsAreEveryOneSaveWouldWrite() {
        val draft = PhotoDraft()
            .withAdded(new("n1", 200))
            .withNew("n1") { it.copy(caption = "New one") }
            .withChange(photos[1], caption = "Lake")
            .withChange(photos[0], routeAt = 150)
        assertEquals(listOf("New one", "Lake"), draft.captions)
    }
}
