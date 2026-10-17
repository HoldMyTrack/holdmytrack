package dev.holdmytrack.android.map

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

/**
 * Fog of War and Heatmap as the served style carries them, in its metadata under
 * [OVERLAYS_METADATA] (root `docs/ARCHITECTURE.md` §2.1, ADR-0047): the sources, the layers, what
 * each mode shows and how, and the zoom tiers, defined once in the web's
 * `apps/web/src/map/coverageStyle.ts`. This reads them; [CoverageLayers] puts them on a map. Pure,
 * so nothing here needs MapLibre.
 */
object CoverageStyle {

    const val OVERLAYS_METADATA = "holdmytrack:overlays"

    data class Source(val tiles: String, val minZoom: Float, val maxZoom: Float, val attribution: String?)

    /** One layer: its MapLibre type, where it draws from, and every property it's added with. */
    data class Layer(
        val id: String,
        val type: String,
        val source: String?,
        val sourceLayer: String?,
        val minZoom: Float?,
        val maxZoom: Float?,
        val filter: String?,
        val properties: Map<String, Value>,
    )

    /** A view: by layer id, the properties to set, layout and paint alike (their names don't overlap). */
    data class View(val properties: Map<String, Map<String, Value>>)

    data class Tier(val tier: ZoomTier, val below: Double?)

    data class Overlays(
        val sources: Map<String, Source>,
        val layers: List<Layer>,
        val plain: Map<MapMode, View>,
        val satellite: Map<MapMode, View>,
        val tiers: List<Tier>,
    )

    /** A property's value as MapLibre Native takes it: an expression, as its JSON, or a literal. */
    sealed interface Value {
        data class Expression(val json: String) : Value
        data class Literal(val value: Any) : Value
    }

    /** The overlays in a style's [metadata] ([StyleMetadata]), or null for one without them or
     *  with them malformed. */
    fun read(metadata: JsonObject): Overlays? = runCatching {
        parse(metadata[OVERLAYS_METADATA]?.jsonObject ?: return null)
    }.getOrNull()

    /** The view for [mode], over satellite imagery or not. */
    fun view(overlays: Overlays, mode: MapMode, satellite: Boolean): View? =
        (if (satellite) overlays.satellite else overlays.plain)[mode]

    /** The tier in view at [zoom]: the first whose `below` is past it. */
    fun tier(tiers: List<Tier>, zoom: Double): ZoomTier =
        tiers.firstOrNull { it.below == null || zoom < it.below }?.tier ?: ZoomTier.CITY

    /** Every array is an expression (the contract `coverageStyle.ts` keeps); numbers are floats,
     *  which is what MapLibre Native's number properties take. */
    fun value(json: JsonElement): Value? = when {
        json is JsonArray -> Value.Expression(json.toString())
        json is JsonPrimitive && json.isString -> Value.Literal(json.content)
        json is JsonPrimitive -> json.booleanOrNull?.let { Value.Literal(it) }
            ?: json.doubleOrNull?.let { Value.Literal(it.toFloat()) }
        else -> null
    }

    private fun parse(o: JsonObject): Overlays {
        val sources = o.obj("sources").mapValues { (_, s) ->
            val source = s.jsonObject
            Source(
                tiles = source.string("tiles")!!,
                minZoom = source.float("minzoom")!!,
                maxZoom = source.float("maxzoom")!!,
                attribution = source.string("attribution"),
            )
        }
        val layers = (o["layers"] as JsonArray).map { layer(it.jsonObject) }
        val views = o.obj("views")
        val tiers = (o["tiers"] as JsonArray).map { t ->
            val tier = t.jsonObject
            Tier(ZoomTier.valueOf(tier.string("tier")!!.uppercase()), tier["below"]?.jsonPrimitive?.doubleOrNull)
        }
        return Overlays(sources, layers, views(views.obj("plain")), views(views.obj("satellite")), tiers)
    }

    private fun layer(l: JsonObject): Layer = Layer(
        id = l.string("id")!!,
        type = l.string("type")!!,
        source = l.string("source"),
        sourceLayer = l.string("source-layer"),
        minZoom = l.float("minzoom"),
        maxZoom = l.float("maxzoom"),
        filter = l["filter"]?.toString(),
        properties = properties(l["layout"]) + properties(l["paint"]),
    )

    private fun views(modes: JsonObject): Map<MapMode, View> = MapMode.entries.mapNotNull { mode ->
        val view = modes[mode.name.lowercase()]?.jsonObject ?: return@mapNotNull null
        val byLayer = mutableMapOf<String, Map<String, Value>>()
        for (kind in listOf("layout", "paint")) {
            view[kind]?.jsonObject?.forEach { (id, props) -> byLayer[id] = byLayer[id].orEmpty() + properties(props) }
        }
        mode to View(byLayer)
    }.toMap()

    private fun properties(json: JsonElement?): Map<String, Value> =
        (json as? JsonObject)?.mapNotNull { (name, v) -> value(v)?.let { name to it } }?.toMap().orEmpty()

    private fun JsonObject.obj(key: String): JsonObject = this[key]!!.jsonObject
    private fun JsonObject.string(key: String): String? = (this[key] as? JsonPrimitive)?.takeIf { it.isString }?.content
    private fun JsonObject.float(key: String): Float? = (this[key] as? JsonPrimitive)?.doubleOrNull?.toFloat()
}
