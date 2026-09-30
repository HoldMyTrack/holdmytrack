package dev.holdmytrack.android.panel

import dev.holdmytrack.android.net.Story

/** How much of the Edit window's activities a Story holds — the web's `StoryMembership`. */
enum class StoryMembership { ALL, SOME, NONE }

/**
 * The Edit window's Stories tab rules (`apps/android/docs/SPEC.md` FR-2.7 item 17):
 * a Story's box is ticked when it holds every one of the window's activities, partly ticked
 * when it holds some, clear when it holds none; a tap ticks a clear or partial box and clears a
 * ticked one, so partial is the one state a tap can't choose, only leave. [changes] is what the
 * user picked, by Story id, where it differs from what the Story holds now — what Save writes.
 * Plain data and rules, no views, so it's what the unit tests exercise.
 */
class StoryChanges(private val activityIds: List<String>) {

    private val picked = LinkedHashMap<String, StoryMembership>()

    /** Ticked ([StoryMembership.ALL]) or cleared ([StoryMembership.NONE]), by Story id. */
    val changes: Map<String, StoryMembership> get() = picked

    fun membershipOf(story: Story): StoryMembership {
        val held = activityIds.count { it in story.activityIds }
        return when (held) {
            0 -> StoryMembership.NONE
            activityIds.size -> StoryMembership.ALL
            else -> StoryMembership.SOME
        }
    }

    /** What [story]'s box shows: the pick, else what it holds. */
    fun shown(story: Story): StoryMembership = picked[story.id] ?: membershipOf(story)

    fun toggle(story: Story) {
        val next = if (shown(story) == StoryMembership.ALL) StoryMembership.NONE else StoryMembership.ALL
        if (next == membershipOf(story)) picked.remove(story.id) else picked[story.id] = next
    }
}
