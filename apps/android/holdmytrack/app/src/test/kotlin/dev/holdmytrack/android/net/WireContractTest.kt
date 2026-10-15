package dev.holdmytrack.android.net

import java.io.File
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.descriptors.StructureKind
import kotlinx.serialization.Serializable
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.yaml.snakeyaml.Yaml

/**
 * Holds every class in Wire.kt to the API contract, services/server/api: each response class to
 * the response it decodes, each request class to the body it sends. A field the server renames,
 * drops or makes nullable fails here, in CI, rather than in a decode on a phone. Gradle reruns this
 * when the contract changes (the test task declares it as an input).
 */
class WireContractTest {

    private val contract = Contract.find()

    @Test
    fun `every wire class matches the contract`() {
        val problems = mutableListOf<String>()
        val seen = mutableSetOf<String>()
        fun response(desc: SerialDescriptor, path: String, method: String, status: Int = 200) =
            WireCheck(seen).compare(desc, contract.response(path, method, status), "$method $path $status", Mode.RESPONSE, problems)
        fun request(desc: SerialDescriptor, path: String, method: String) =
            WireCheck(seen).compare(desc, contract.request(path, method), "$method $path request", Mode.REQUEST, problems)

        response(ErrorJson.serializer().descriptor, "/stories/{id}", "get", 404)
        response(AccountJson.serializer().descriptor, "/auth/me", "get")
        for (path in listOf("/auth/login", "/auth/handoff", "/auth/google/token")) {
            response(AccountJson.serializer().descriptor, path, "post")
            response(SessionTokenJson.serializer().descriptor, path, "post")
        }
        response(SessionTokenJson.serializer().descriptor, "/auth/signup", "post", 201)
        response(SessionTokenJson.serializer().descriptor, "/auth/demo", "post", 201)
        response(AccountJson.serializer().descriptor, "/account/settings", "patch")
        response(MessageJson.serializer().descriptor, "/auth/resend-verification", "post")
        response(ProvidersJson.serializer().descriptor, "/auth/providers", "get")
        response(SettingsOptionsJson.serializer().descriptor, "/account/settings/options", "get")
        response(ExportStatusJson.serializer().descriptor, "/account/export", "get")
        response(ExportStatusJson.serializer().descriptor, "/account/export", "post", 202)
        request(CredentialsJson.serializer().descriptor, "/auth/login", "post")
        request(CredentialsJson.serializer().descriptor, "/auth/signup", "post")
        request(GoogleTokenJson.serializer().descriptor, "/auth/google/token", "post")
        request(HandoffJson.serializer().descriptor, "/auth/handoff", "post")
        request(SettingsJson.serializer().descriptor, "/account/settings", "patch")
        request(EmailJson.serializer().descriptor, "/account", "delete")
        request(EmailJson.serializer().descriptor, "/stories/{id}/send", "post")

        response(ActivitiesJson.serializer().descriptor, "/activities", "get")
        response(ActivityRowJson.serializer().descriptor, "/activities/{id}", "patch")
        request(ActivityPatchJson.serializer().descriptor, "/activities/{id}", "patch")
        response(HistogramJson.serializer().descriptor, "/activities/histogram", "get")
        response(CoverageStatusJson.serializer().descriptor, "/coverage/status", "get")
        response(TrackMetricsJson.serializer().descriptor, "/activities/track-metrics/{id}", "get")
        response(TrackPointsJson.serializer().descriptor, "/activities/track-points/{id}", "get")
        request(TrackEditRequestJson.serializer().descriptor, "/activities/track-edit/{id}", "post")
        request(OverlapsRequestJson.serializer().descriptor, "/activities/overlaps", "post")
        response(OverlapsJson.serializer().descriptor, "/activities/overlaps", "post")
        response(UploadResultJson.serializer().descriptor, "/activities/upload", "post", 202)
        response(UploadResultJson.serializer().descriptor, "/activities/upload", "post", 200)

        request(SyncActivitiesRequestJson.serializer().descriptor, "/sync/activities", "post")
        response(SyncResultsJson.serializer().descriptor, "/sync/activities", "post")
        request(SyncKnownRequestJson.serializer().descriptor, "/sync/known", "post")
        response(SyncKnownJson.serializer().descriptor, "/sync/known", "post")

        response(ListSerializer(PrivateLocationJson.serializer()).descriptor, "/private-locations", "get")
        response(PrivateLocationJson.serializer().descriptor, "/private-locations", "post", 201)
        response(PrivateLocationJson.serializer().descriptor, "/private-locations/{id}", "patch")
        request(PrivateLocationInputJson.serializer().descriptor, "/private-locations", "post")
        request(PrivateLocationInputJson.serializer().descriptor, "/private-locations/{id}", "patch")

        response(StoriesJson.serializer().descriptor, "/stories", "get")
        response(StoryJson.serializer().descriptor, "/stories", "post", 201)
        response(StoryJson.serializer().descriptor, "/stories/{id}", "get")
        response(StoryJson.serializer().descriptor, "/stories/{id}", "patch")
        response(StoryJson.serializer().descriptor, "/stories/{id}/activities", "post")
        response(StoryJson.serializer().descriptor, "/stories/{id}/activities", "delete")
        request(StoryCreateJson.serializer().descriptor, "/stories", "post")
        request(StoryInputJson.serializer().descriptor, "/stories/{id}", "patch")
        request(StoryActivitiesJson.serializer().descriptor, "/stories/{id}/activities", "post")
        request(StoryActivitiesJson.serializer().descriptor, "/stories/{id}/activities", "delete")
        response(StoryInboxJson.serializer().descriptor, "/story-sends", "get")

        response(PhotosJson.serializer().descriptor, "/photos", "get")
        response(PhotoJson.serializer().descriptor, "/photos", "post", 201)
        response(PhotoJson.serializer().descriptor, "/photos/{id}", "patch")
        request(PhotoPlaceRequestJson.serializer().descriptor, "/photos/place", "post")
        response(PhotoPlaceJson.serializer().descriptor, "/photos/place", "post")
        request(PhotoPatchJson.serializer().descriptor, "/photos/{id}", "patch")

        response(SpotsInAreaJson.serializer().descriptor, "/spots", "get")
        response(SpotDetailJson.serializer().descriptor, "/spots/{id}", "get")
        response(SpotCapturesJson.serializer().descriptor, "/spots/captures", "get")
        response(SpotCaptureJson.serializer().descriptor, "/spots/{id}/captures", "post", 201)
        response(SpotCaptureJson.serializer().descriptor, "/spots/{id}/captures", "post", 200)
        request(SpotCaptureRequestJson.serializer().descriptor, "/spots/{id}/captures", "post")

        assertEquals("", problems.joinToString("\n"))

        // Every class in Wire.kt is reached by some check above, so a new one can't go unchecked.
        val declared = Regex("""data class (\w+Json)\(""").findAll(contract.wireSource.readText()).map { it.groupValues[1] }.toSet()
        val unchecked = declared - seen.map { it.substringAfterLast('.') }.toSet()
        assertEquals("Wire.kt classes no check reaches", emptySet<String>(), unchecked)
    }

    // The checker's own rules, against schemas written here.

    @Serializable private data class NonNull(val a: String)
    @Serializable private data class Nullable(val a: String?)
    @Serializable private data class Defaulted(val a: String = "")
    @Serializable private data class Number(val a: Long)

    private fun problems(desc: SerialDescriptor, schema: Map<String, Any?>, mode: Mode = Mode.RESPONSE): List<String> {
        val out = mutableListOf<String>()
        WireCheck(mutableSetOf()).compare(desc, schema, "test", mode, out)
        return out
    }

    private fun obj(required: List<String>, vararg props: Pair<String, Map<String, Any?>>) =
        mapOf("type" to "object", "required" to required, "properties" to mapOf(*props))

    @Test
    fun `a field the contract makes nullable must be nullable`() {
        val schema = obj(listOf("a"), "a" to mapOf("type" to "string", "nullable" to true))
        assertTrue(problems(NonNull.serializer().descriptor, schema).single().contains("can be null"))
        assertEquals(emptyList<String>(), problems(Nullable.serializer().descriptor, schema))
    }

    @Test
    fun `a field the contract doesn't require needs a default or null`() {
        val schema = obj(emptyList(), "a" to mapOf("type" to "string"))
        assertTrue(problems(NonNull.serializer().descriptor, schema).single().contains("require"))
        assertEquals(emptyList<String>(), problems(Defaulted.serializer().descriptor, schema))
    }

    @Test
    fun `a field missing from the contract, or of another type, is a problem`() {
        assertTrue(problems(NonNull.serializer().descriptor, obj(listOf("b"), "b" to mapOf("type" to "string"))).single().contains("not in the contract"))
        assertTrue(problems(Number.serializer().descriptor, obj(listOf("a"), "a" to mapOf("type" to "string"))).single().contains("integer"))
    }

    @Test
    fun `a request must send what the contract requires`() {
        val schema = obj(listOf("a"), "a" to mapOf("type" to "string"))
        assertTrue(problems(Defaulted.serializer().descriptor, schema, Mode.REQUEST).single().contains("requires"))
        assertEquals(emptyList<String>(), problems(NonNull.serializer().descriptor, schema, Mode.REQUEST))
    }
}

internal enum class Mode { RESPONSE, REQUEST }

/** Compares a class's serial descriptor with the schema it decodes or encodes, recording each
 *  disagreement in `problems` and every class reached in [seen]. */
internal class WireCheck(private val seen: MutableSet<String>) {
    fun compare(desc: SerialDescriptor, schema: Map<String, Any?>, where: String, mode: Mode, problems: MutableList<String>) {
        val type = schema["type"] as String?
        when (desc.kind) {
            StructureKind.CLASS -> {
                seen += desc.serialName.removeSuffix("?")
                if (type != "object") return run { problems += "$where: ${desc.serialName} is an object, the contract a $type" }
                @Suppress("UNCHECKED_CAST")
                val props = schema["properties"] as Map<String, Any?>? ?: emptyMap()
                @Suppress("UNCHECKED_CAST")
                val required = (schema["required"] as List<String>?).orEmpty().toSet()
                for (i in 0 until desc.elementsCount) {
                    val name = desc.getElementName(i)
                    val element = desc.getElementDescriptor(i)
                    val at = "$where: ${desc.serialName.substringAfterLast('.')}.$name"
                    @Suppress("UNCHECKED_CAST")
                    val prop = props[name] as Map<String, Any?>? ?: run { problems += "$at is not in the contract"; null } ?: continue
                    val optional = desc.isElementOptional(i) || element.isNullable
                    when (mode) {
                        Mode.RESPONSE -> {
                            if (!optional && name !in required) problems += "$at: the contract doesn't require it, so give it a default or make it nullable"
                            if (prop["nullable"] == true && !element.isNullable) problems += "$at: the contract says it can be null"
                        }
                        Mode.REQUEST ->
                            if (optional && name in required) problems += "$at: the contract requires it, so it can't be left out"
                    }
                    compare(element, prop, at, mode, problems)
                }
                if (mode == Mode.REQUEST) {
                    val sent = (0 until desc.elementsCount).map(desc::getElementName).toSet()
                    for (name in required - sent) problems += "$where: the contract requires $name, which ${desc.serialName.substringAfterLast('.')} doesn't send"
                }
            }
            StructureKind.LIST -> {
                if (type != "array") return run { problems += "$where is a list, the contract a $type" }
                @Suppress("UNCHECKED_CAST")
                compare(desc.getElementDescriptor(0), schema["items"] as Map<String, Any?>, "$where[]", mode, problems)
            }
            StructureKind.MAP -> {
                val values = schema["additionalProperties"]
                if (type != "object" || values !is Map<*, *>) return run { problems += "$where is a map, the contract has no additionalProperties" }
                @Suppress("UNCHECKED_CAST")
                compare(desc.getElementDescriptor(1), values as Map<String, Any?>, "$where{}", mode, problems)
            }
            PrimitiveKind.STRING -> if (type != "string") problems += "$where is a string, the contract a $type"
            PrimitiveKind.INT, PrimitiveKind.LONG -> if (type != "integer") problems += "$where is an integer, the contract a $type"
            PrimitiveKind.DOUBLE -> if (type != "number" && type != "integer") problems += "$where is a number, the contract a $type"
            PrimitiveKind.BOOLEAN -> if (type != "boolean") problems += "$where is a boolean, the contract a $type"
            else -> problems += "$where: ${desc.kind} isn't one the check knows"
        }
    }
}

/** The API contract, read from services/server/api with every `$ref` inlined — followed relative
 *  to the file it's written in — and each `allOf` merged into one schema, so a schema here is a
 *  plain tree. The contract has no recursive schemas, so inlining ends. */
internal class Contract private constructor(root: File) {
    private val files = mutableMapOf<File, Map<String, Any?>>()
    private val index = File(root, "services/server/api/openapi.yaml")

    val wireSource: File = File(root, "apps/android/holdmytrack/app/src/main/kotlin/dev/holdmytrack/android/net/Wire.kt")

    fun response(path: String, method: String, status: Int): Map<String, Any?> {
        val response = operation(path, method).obj("responses").obj(status.toString())
        return response.obj("content").obj("application/json").obj("schema")
    }

    fun request(path: String, method: String): Map<String, Any?> =
        operation(path, method).obj("requestBody").obj("content").obj("application/json").obj("schema")

    @Suppress("UNCHECKED_CAST")
    private fun operation(path: String, method: String): Map<String, Any?> =
        (inline(load(index).obj("paths").obj(path), index) as Map<String, Any?>).obj(method)

    /** [value] with every `$ref` in it replaced by what it points at, read relative to [file]. */
    @Suppress("UNCHECKED_CAST")
    fun inline(value: Any?, file: File): Any? = when (value) {
        is List<*> -> value.map { inline(it, file) }
        is Map<*, *> -> {
            val map = value as Map<String, Any?>
            val ref = map["\$ref"] as String?
            when {
                ref != null -> {
                    val target = ref.substringBefore('#')
                    val targetFile = if (target.isEmpty()) file else File(file.parentFile, target)
                    var node: Any? = load(targetFile)
                    for (key in ref.substringAfter('#', "").split('/').filter { it.isNotEmpty() }) {
                        node = (node as Map<String, Any?>)[key.replace("~1", "/").replace("~0", "~")]
                    }
                    inline(node, targetFile)
                }
                "allOf" in map -> merge((map["allOf"] as List<Any?>).map { inline(it, file) as Map<String, Any?> } + listOf(map - "allOf"))
                else -> map.mapValues { (_, v) -> inline(v, file) }
            }
        }
        else -> value
    }

    private fun load(file: File): Map<String, Any?> = files.getOrPut(file.canonicalFile) { Yaml().load(file.readText()) }

    companion object {
        /** The contract of the checkout this test runs in, found upward from the working directory. */
        fun find(): Contract {
            var dir: File? = File(System.getProperty("user.dir")).absoluteFile
            while (dir != null && !File(dir, "services/server/api/openapi.yaml").isFile) dir = dir.parentFile
            return Contract(dir ?: error("services/server/api/openapi.yaml not found above ${System.getProperty("user.dir")}"))
        }

        /** `allOf`'s parts as one schema: their properties together, a property two parts
         *  describe merged too (one part's enum narrowing another's string), and every required
         *  field. */
        @Suppress("UNCHECKED_CAST")
        fun merge(parts: List<Map<String, Any?>>): Map<String, Any?> {
            val properties = mutableMapOf<String, Map<String, Any?>>()
            val required = mutableSetOf<String>()
            var merged = emptyMap<String, Any?>()
            for (part in parts) {
                merged = merged + part
                (part["properties"] as Map<String, Map<String, Any?>>?)?.forEach { (k, v) -> properties[k] = properties[k].orEmpty() + v }
                required += (part["required"] as List<String>?).orEmpty()
            }
            return merged + ("properties" to properties) + ("required" to required.toList())
        }

        @Suppress("UNCHECKED_CAST")
        private fun Map<String, Any?>.obj(key: String): Map<String, Any?> =
            this[key] as Map<String, Any?>? ?: error("no $key in the contract here: ${this.keys}")
    }
}
