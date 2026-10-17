package dev.holdmytrack.android.map

import java.io.File
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Reads the style the server serves, as built from the web's `coverageStyle.ts`. */
class CoverageStyleTest {

    private fun served(flavor: String): CoverageStyle.Overlays {
        var dir: File? = File(System.getProperty("user.dir")).absoluteFile
        val path = "services/server/internal/mapstyle/styles/en/$flavor.json"
        while (dir != null && !File(dir, path).isFile) dir = dir.parentFile
        val file = File(dir ?: error("$path not found above ${System.getProperty("user.dir")}"), path)
        return CoverageStyle.read(StyleMetadata.parse(file.readText())) ?: error("no overlays in $path")
    }

    private val light = served("light")

    @Test
    fun everyLayerDrawsFromASourceTheOverlaysCarry() {
        assertTrue(light.layers.isNotEmpty())
        for (layer in light.layers) {
            layer.source?.let { assertNotNull(layer.id, light.sources[it]) }
        }
        val fog = light.sources.getValue("fog-vector")
        assertEquals("/tiles/v1/fog-vector/{z}/{x}/{y}.mvt", fog.tiles)
        assertEquals(14f, fog.maxZoom)
    }

    @Test
    fun everyModeHasAViewOverImageryOrNot() {
        for (satellite in listOf(false, true)) {
            for (mode in MapMode.entries) assertNotNull(CoverageStyle.view(light, mode, satellite))
        }
    }

    @Test
    fun fogShowsTheVeilAndMutesLabels() {
        val fog = CoverageStyle.view(light, MapMode.FOG, false)!!.properties
        val normal = CoverageStyle.view(light, MapMode.NORMAL, false)!!.properties
        assertEquals(CoverageStyle.Value.Literal("visible"), fog.getValue("fog-veil").getValue("visibility"))
        assertEquals(CoverageStyle.Value.Literal("none"), normal.getValue("fog-veil").getValue("visibility"))
        val label = fog.keys.first { it.startsWith("places_") }
        assertEquals(CoverageStyle.Value.Literal(0.4f), fog.getValue(label).getValue("text-opacity"))
        assertEquals(CoverageStyle.Value.Literal(1f), normal.getValue(label).getValue("text-opacity"))
    }

    @Test
    fun imageryTakesTheDarkVeil() {
        fun veil(satellite: Boolean) = CoverageStyle.view(light, MapMode.FOG, satellite)!!.properties.getValue("fog-veil").getValue("fill-color")
        assertEquals(CoverageStyle.Value.Literal("#202b25"), veil(false))
        assertEquals(CoverageStyle.Value.Literal("#f7f4ec"), veil(true))
    }

    @Test
    fun layersCarryExpressionsAsJson() {
        val heat = light.layers.first { it.id == "heat-points-circles" }
        assertEquals("heatmap", heat.type)
        assertEquals("heat", heat.sourceLayer)
        assertTrue(heat.properties.getValue("heatmap-radius") is CoverageStyle.Value.Expression)
        assertEquals(CoverageStyle.Value.Literal(2.5f), heat.properties.getValue("heatmap-intensity"))
        val veil = light.layers.first { it.id == "fog-veil" }
        assertEquals("""["==",["get","kind"],"veil"]""", veil.filter)
    }

    @Test
    fun tiersSwitchWhereTheFillsHandOver() {
        assertEquals(ZoomTier.COUNTRY, CoverageStyle.tier(light.tiers, 2.99))
        assertEquals(ZoomTier.REGION, CoverageStyle.tier(light.tiers, 3.0))
        assertEquals(ZoomTier.REGION, CoverageStyle.tier(light.tiers, 6.99))
        assertEquals(ZoomTier.CITY, CoverageStyle.tier(light.tiers, 7.0))
        assertEquals(ZoomTier.CITY, CoverageStyle.tier(emptyList(), 1.0))
    }

    @Test
    fun valuesAreExpressionsOrLiterals() {
        assertEquals(CoverageStyle.Value.Expression("""["get","w"]"""), CoverageStyle.value(JsonArray(listOf(JsonPrimitive("get"), JsonPrimitive("w")))))
        assertEquals(CoverageStyle.Value.Literal(0.82f), CoverageStyle.value(JsonPrimitive(0.82)))
        assertEquals(CoverageStyle.Value.Literal(true), CoverageStyle.value(JsonPrimitive(true)))
        assertEquals(CoverageStyle.Value.Literal("butt"), CoverageStyle.value(JsonPrimitive("butt")))
    }

    @Test
    fun aStyleWithoutOverlaysReadsAsNone() {
        assertNull(CoverageStyle.read(StyleMetadata.parse("""{"version":8,"sources":{},"layers":[]}""")))
        assertNull(CoverageStyle.read(StyleMetadata.parse("not json")))
        assertNull(CoverageStyle.read(StyleMetadata.parse("""{"metadata":{"holdmytrack:overlays":{"sources":1}}}""")))
    }

    @Test
    fun metadataIsReadFromTheStyle() {
        val metadata = StyleMetadata.parse("""{"version":8,"metadata":{"holdmytrack:satellite-road-opacity":0.4}}""")
        assertEquals(JsonPrimitive(0.4), metadata["holdmytrack:satellite-road-opacity"])
        assertTrue(StyleMetadata.parse("[]").isEmpty())
    }
}
