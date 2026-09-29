package dev.holdmytrack.android.panel

import dev.holdmytrack.android.net.Story
import dev.holdmytrack.android.net.StoryTotals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class StoryChangesTest {

    private fun story(id: String, vararg activityIds: String) =
        Story(id, id, "", activityIds.toList(), StoryTotals("", activityIds.size, 0.0, 0), emptyList())

    private val holdsBoth = story("both", "a", "b", "z")
    private val holdsOne = story("one", "a")
    private val holdsNone = story("none", "z")

    @Test
    fun `a box shows how much of the window's activities its Story holds`() {
        val changes = StoryChanges(listOf("a", "b"))
        assertEquals(StoryMembership.ALL, changes.shown(holdsBoth))
        assertEquals(StoryMembership.SOME, changes.shown(holdsOne))
        assertEquals(StoryMembership.NONE, changes.shown(holdsNone))
        assertTrue(changes.changes.isEmpty())
    }

    @Test
    fun `a tap ticks a clear or partial box and clears a ticked one`() {
        val changes = StoryChanges(listOf("a", "b"))
        changes.toggle(holdsNone)
        changes.toggle(holdsOne)
        changes.toggle(holdsBoth)
        assertEquals(StoryMembership.ALL, changes.shown(holdsNone))
        assertEquals(StoryMembership.ALL, changes.shown(holdsOne))
        assertEquals(StoryMembership.NONE, changes.shown(holdsBoth))
        assertEquals(
            mapOf("none" to StoryMembership.ALL, "one" to StoryMembership.ALL, "both" to StoryMembership.NONE),
            changes.changes,
        )
    }

    @Test
    fun `tapping back to what's saved is no change, and partial can only be left`() {
        val changes = StoryChanges(listOf("a", "b"))
        changes.toggle(holdsNone)
        changes.toggle(holdsNone)
        assertEquals(StoryMembership.NONE, changes.shown(holdsNone))
        // Partial → ticked → cleared: never partial again, and cleared is a change.
        changes.toggle(holdsOne)
        changes.toggle(holdsOne)
        assertEquals(StoryMembership.NONE, changes.shown(holdsOne))
        assertEquals(mapOf("one" to StoryMembership.NONE), changes.changes)
    }
}
