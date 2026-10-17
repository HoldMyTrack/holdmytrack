package dev.holdmytrack.android.map

import java.util.WeakHashMap
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import org.maplibre.android.maps.Style

/**
 * The served style's `metadata`, which carries the overlays ([CoverageStyle]) and the satellite
 * lists ([MapSatellite]). `Style.json` serializes the whole document, a few hundred KB, so each
 * [Style] is parsed once and kept by weak reference: a reload replaces the [Style], and its entry
 * goes with it.
 */
object StyleMetadata {

    private val parsed = WeakHashMap<Style, JsonObject>()

    fun of(style: Style): JsonObject = parsed.getOrPut(style) { parse(style.json) }

    /** [styleJson]'s metadata, empty for a style without any or one that doesn't parse. */
    fun parse(styleJson: String): JsonObject =
        runCatching { Json.parseToJsonElement(styleJson).jsonObject["metadata"]?.jsonObject }.getOrNull() ?: JsonObject(emptyMap())
}
