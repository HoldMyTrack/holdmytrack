package dev.holdmytrack.android.panel

import dev.holdmytrack.android.net.Activity
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PanelStateTest {

    private fun activity(id: String, type: String = "walking", meters: Double? = 1000.0, pending: Boolean = false) =
        Activity(
            id = id,
            startedAt = "2026-09-24T08:00:00Z",
            activityType = type,
            name = null,
            distanceMeters = meters,
            durationSeconds = 600,
            description = null,
            bbox = listOf(0.0, 0.0, 1.0, 1.0),
            pending = pending,
            edited = false,
        )

    private val walk = activity("a", "walking", 1200.0)
    private val run = activity("b", "running", 10400.0)
    private val ride = activity("c", "cycling", 21000.0)
    private val noDistance = activity("d", "walking", null)

    private fun state(vararg activities: Activity) = PanelState().apply { setActivities(activities.toList()) }

    @Test
    fun `distance bounds skip activities with no distance`() {
        assertEquals(DistanceRange(1200.0, 21000.0), ActivityFacets.distanceBounds(listOf(walk, run, ride, noDistance)))
        assertNull(ActivityFacets.distanceBounds(listOf(noDistance)))
    }

    @Test
    fun `an active distance band drops activities with no distance`() {
        val band = DistanceRange(1000.0, 5000.0)
        assertTrue(ActivityFacets.passesDistance(walk, band))
        assertFalse(ActivityFacets.passesDistance(run, band))
        assertFalse(ActivityFacets.passesDistance(noDistance, band))
        assertTrue(ActivityFacets.passesDistance(noDistance, null))
    }

    @Test
    fun `type facets are counted after the distance band, busiest first`() {
        val facets = ActivityFacets.typeFacets(listOf(walk, run, ride, noDistance), null)
        assertEquals(listOf(TypeFacet("walking", 2), TypeFacet("running", 1), TypeFacet("cycling", 1)), facets)
        assertEquals(listOf(TypeFacet("walking", 1)), ActivityFacets.typeFacets(listOf(walk, run, noDistance), DistanceRange(0.0, 5000.0)))
    }

    @Test
    fun `the toolbar acts on the checked group, else the selected row`() {
        val s = state(walk, run, ride)
        assertEquals(emptySet<String>(), s.targetIds)
        s.focus("b")
        assertEquals(setOf("b"), s.targetIds)
        s.toggleChecked("a")
        assertEquals(setOf("a"), s.targetIds)
        s.toggleChecked("a")
        assertEquals(setOf("b"), s.targetIds)
    }

    @Test
    fun `selecting a row never touches the checked group`() {
        val s = state(walk, run)
        s.toggleChecked("a")
        s.focus("b")
        s.clearFocus()
        assertEquals(setOf("a"), s.checked)
    }

    @Test
    fun `check all and invert only reach the listed rows`() {
        val s = state(walk, run, ride)
        s.toggleType("cycling")
        s.checkAll()
        assertEquals(setOf("a", "b"), s.checked)
        s.toggleChecked("a")
        s.invertChecked()
        assertEquals(setOf("a"), s.checked)
    }

    @Test
    fun `show and hide makes the whole target one state`() {
        val s = state(walk, run)
        s.toggleChecked("a")
        s.toggleTargetVisibility()
        assertEquals(setOf("a"), s.hidden)
        s.toggleChecked("b")
        s.toggleTargetVisibility()
        assertEquals(emptySet<String>(), s.hidden)
        s.toggleTargetVisibility()
        assertEquals(setOf("a", "b"), s.hidden)
    }

    @Test
    fun `the map hides filtered-out, hidden and pending tracks`() {
        val pending = activity("e", pending = true)
        val s = state(walk, run, ride, pending)
        s.toggleType("cycling")
        s.focus("a")
        s.toggleTargetVisibility()
        assertEquals(setOf("a", "c", "e"), s.mapHidden)
        assertEquals(listOf("a", "b", "e"), s.listed.map { it.id })
    }

    @Test
    fun `targets filtered out of the list disable the toolbar`() {
        val s = state(walk, ride)
        s.focus("c")
        s.toggleType("cycling")
        assertEquals(setOf("c"), s.targetIds)
        assertTrue(s.targets.isEmpty())
    }

    @Test
    fun `a new range clears everything built against the old one`() {
        val s = state(walk, run)
        s.toggleChecked("a")
        s.focus("b")
        s.toggleTargetVisibility()
        s.toggleType("running")
        s.setDistanceFilter(DistanceRange(0.0, 2000.0))
        s.resetForNewRange()
        assertTrue(s.checked.isEmpty() && s.hidden.isEmpty() && s.excludedTypes.isEmpty())
        assertNull(s.focused)
        assertNull(s.distanceFilter)
    }

    @Test
    fun `a refreshed list keeps what still exists`() {
        val s = state(walk, run)
        s.toggleChecked("a")
        s.toggleChecked("b")
        s.focus("b")
        s.setActivities(listOf(walk))
        assertEquals(setOf("a"), s.checked)
        assertNull(s.focused)
    }
}
