package dev.holdmytrack.android.map

import dev.holdmytrack.android.panel.PanelRules
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
