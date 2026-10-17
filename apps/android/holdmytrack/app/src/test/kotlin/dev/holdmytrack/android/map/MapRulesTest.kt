package dev.holdmytrack.android.map

import dev.holdmytrack.android.net.Activity
import dev.holdmytrack.android.panel.PanelRules
import dev.holdmytrack.android.panel.PanelState
import dev.holdmytrack.android.photos.PhotoMarkerScope
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.double
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * The map rules every client implements alike, run against the shared cases in
 * `testdata/map-session` (its README has the format), so a rule can't change here without the
 * web's failing too.
 */
class MapRulesTest {

    private val dir: File = run {
        var at: File? = File(System.getProperty("user.dir")).absoluteFile
        while (at != null && !File(at, "testdata/map-session").isDirectory) at = at.parentFile
        File(at ?: error("testdata/map-session not found above ${System.getProperty("user.dir")}"), "testdata/map-session")
    }

    private fun JsonObject.string(key: String): String? = (this[key] as? JsonPrimitive)?.takeIf { it !is JsonNull }?.content

    private fun JsonObject.strings(key: String): List<String> = (this[key] as? JsonArray)?.map { it.jsonPrimitive.content }.orEmpty()

    private fun ids(values: Collection<String>): JsonArray = buildJsonArray { values.sorted().forEach { add(JsonPrimitive(it)) } }

    /** Each rule, from a case's input to the JSON its expected value is written in. */
    private val rules: Map<String, (JsonObject) -> JsonElement> = mapOf(
        "defaultRange" to { input ->
            val range = DefaultRange.of(input.strings("visibleDays"), input.string("earliest"), input.string("today")!!)
            buildJsonObject {
                put("from", range.from)
                put("to", range.to)
            }
        },
        "pendingChange" to { input ->
            val change = PendingDiff.of(input.strings("previous").toSet(), input.strings("now").toSet(), input.strings("awaiting").toSet())
            buildJsonObject {
                put("started", change.started)
                put("finished", change.finished)
                put("left", ids(change.left))
                put("landed", ids(change.landed))
                put("awaiting", ids(change.awaiting))
            }
        },
        "toolbarTarget" to { input -> ids(PanelRules.targetIds(input.strings("checked").toSet(), input.string("focused"))) },
        "targetVisibility" to { input -> ids(PanelRules.toggledVisibility(input.strings("hidden").toSet(), input.strings("target").toSet())) },
        "photoScope" to { input ->
            val edit = (input["editWindowIds"] as? JsonArray)?.map { it.jsonPrimitive.content }
            val scope = PhotoMarkerScope.scope(
                MapMode.valueOf(input.string("mode")!!.uppercase()),
                editOpen = edit != null,
                editSingleId = edit?.singleOrNull(),
                focused = input.string("focused"),
                storyId = input.string("storyId"),
            )
            scope?.split(':', limit = 2)?.let { (kind, id) -> buildJsonObject { put(kind, id) } } ?: JsonNull
        },
        "photoGroup" to { input ->
            val points = input["photos"]!!.jsonArray.map { it.jsonObject["lat"]!!.jsonPrimitive.double to it.jsonObject["lon"]!!.jsonPrimitive.double }
            PhotoMarkerScope.groupFit(points)?.let { box ->
                buildJsonObject { put("fit", buildJsonArray { box.forEach { add(JsonPrimitive(it)) } }) }
            } ?: JsonPrimitive("open")
        },
        "session" to { input ->
            val state = input["state"]!!.jsonObject
            val panel = PanelState()
            val session = MapSession(panel)
            (state["range"] as? JsonObject)?.let { session.restore(range(it), state["userPicked"]?.jsonPrimitive?.booleanOrNull ?: false) }
            // The Story first: opening one resets the selection set up below.
            state.string("storyId")?.let { session.enterStory(it); session.consumeFly() }
            val ids = (state.strings("checked") + state.strings("hidden")).distinct()
            panel.setActivities(ids.map(::activity))
            // Hidden first: hiding goes through the toolbar's target, the checked group.
            state.strings("hidden").forEach { panel.toggleChecked(it) }
            panel.toggleTargetVisibility()
            panel.clearChecked()
            state.strings("checked").forEach { panel.toggleChecked(it) }
            state.strings("excludedTypes").forEach(panel::toggleType)
            val event = input["event"]!!.jsonObject
            when (val type = event.string("type")) {
                "rangePicked" -> session.pickRange(range(event["range"]!!.jsonObject))
                "defaultRange" -> session.offerDefault(range(event["range"]!!.jsonObject), event["editOpen"]?.jsonPrimitive?.booleanOrNull ?: false)
                "storyEntered" -> session.enterStory(event.string("id")!!)
                "storyExited" -> session.exitStory()
                else -> error("no Android event $type")
            }
            buildJsonObject {
                session.range?.let { put("range", buildJsonObject { put("from", it.from); put("to", it.to) }) }
                put("userPicked", session.userPicked)
                session.storyId?.let { put("storyId", it) }
                put("checked", ids(panel.checked))
                put("hidden", ids(panel.hidden))
                put("excludedTypes", ids(panel.excludedTypes))
                put("flyRequest", session.flyRequest)
            }
        },
    )

    private fun range(json: JsonObject) = DateRange(json.string("from")!!, json.string("to")!!)

    private fun activity(id: String) = Activity(
        id = id, startedAt = "2026-10-01T08:00:00Z", activityType = "walking", name = null, distanceMeters = 1000.0,
        durationSeconds = 600, description = null, bbox = listOf(0.0, 0.0, 1.0, 1.0), pending = false, edited = false,
    )

    /** Numbers compared as numbers, so 24.75 and 24.750 agree. */
    private fun normalized(element: JsonElement): JsonElement = when (element) {
        is JsonObject -> JsonObject(element.mapValues { normalized(it.value) })
        is JsonArray -> JsonArray(element.map(::normalized))
        is JsonPrimitive -> if (element.isString || element.booleanOrNull != null || element is JsonNull) element else JsonPrimitive(element.double)
    }

    @Test
    fun `every shared case holds`() {
        val files = dir.listFiles { file -> file.extension == "json" }.orEmpty().sortedBy { it.name }
        assertTrue("no cases in $dir", files.isNotEmpty())
        for (file in files) {
            val doc = Json.parseToJsonElement(file.readText()).jsonObject
            val rule = doc.string("rule")!!
            val run = rules[rule] ?: error("${file.name}: no Android implementation of $rule")
            for (case in doc["cases"]!!.jsonArray.map { it.jsonObject }) {
                val expected = case["expected"] ?: JsonNull
                assertEquals("$rule: ${case.string("name")}", normalized(expected), normalized(run(case["input"]!!.jsonObject)))
            }
        }
    }
}
