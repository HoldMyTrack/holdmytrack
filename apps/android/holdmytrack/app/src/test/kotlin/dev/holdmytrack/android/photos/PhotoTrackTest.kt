package dev.holdmytrack.android.photos

import org.junit.Assert.assertEquals
import org.junit.Test

class PhotoTrackTest {

    // Due east along the equator: 0.001° of longitude is about 111 m. The first leg takes 100 s,
    // then a stop of 500 s where nothing moves, then a second leg of 100 s.
    private val track = PhotoTrack(
        listOf(
            TimedPoint(0.000, 0.0, 1000),
            TimedPoint(0.001, 0.0, 1100),
            TimedPoint(0.001, 0.0, 1600),
            TimedPoint(0.002, 0.0, 1700),
        ),
    )

    @Test
    fun theSliderWalksByDistanceAndAStopTakesNoneOfIt() {
        assertEquals(1000L, track.pointAt(0.0).t)
        assertEquals(1050L, track.pointAt(0.25).t)
        // Halfway along by distance is where the stop is: its first moment.
        assertEquals(0.001, track.pointAt(0.5).lon, 1e-9)
        assertEquals(1650L, track.pointAt(0.75).t)
        assertEquals(1700L, track.pointAt(1.0).t)
    }

    @Test
    fun aMomentIsFoundBackAsAPlace() {
        assertEquals(0.25, track.fractionAt(1050), 1e-9)
        assertEquals(0.75, track.fractionAt(1650), 1e-9)
        // Anywhere in the stop is the same place.
        assertEquals(0.5, track.fractionAt(1300), 1e-9)
        assertEquals(0.0, track.fractionAt(900), 0.0)
        assertEquals(1.0, track.fractionAt(2000), 0.0)
    }

    @Test
    fun aTrackWithNoLengthGoesByTime() {
        val still = PhotoTrack(listOf(TimedPoint(1.0, 1.0, 0), TimedPoint(1.0, 1.0, 100)))
        assertEquals(25L, still.pointAt(0.25).t)
        assertEquals(0.25, still.fractionAt(25), 1e-9)
    }

    @Test
    fun aWaitingPhotoStartsJustAfterTheOneBefore() {
        val after = track.startFraction(PlaceAnchor.At(1050), emptyMap(), emptyMap())
        assertEquals(0.25 + PhotoTrack.NEXT_PHOTO_STEP, after, 1e-9)
    }

    @Test
    fun afterAnotherWaitingPhotoItStartsWhereThatOneWasPut() {
        val anchors = mapOf<String, PlaceAnchor?>("a" to PlaceAnchor.At(1050), "b" to PlaceAnchor.Waiting("a"))
        val start = track.startFraction(PlaceAnchor.Waiting("b"), mapOf("b" to 0.6), anchors)
        assertEquals(0.6 + PhotoTrack.NEXT_PHOTO_STEP, start, 1e-9)
    }

    @Test
    fun aRemovedOneIsSkippedForTheOneBeforeIt() {
        val anchors = mapOf<String, PlaceAnchor?>("b" to PlaceAnchor.At(1050))
        val start = track.startFraction(PlaceAnchor.Waiting("b"), mapOf("b" to null), anchors)
        assertEquals(0.25 + PhotoTrack.NEXT_PHOTO_STEP, start, 1e-9)
    }

    @Test
    fun theFirstOfABatchStartsAtTheBeginningAndNoneGoesPastTheEnd() {
        assertEquals(0.0, track.startFraction(null, emptyMap(), emptyMap()), 0.0)
        assertEquals(1.0, track.startFraction(PlaceAnchor.At(1700), emptyMap(), emptyMap()), 0.0)
    }
}
