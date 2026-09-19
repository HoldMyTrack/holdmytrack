package dev.fitmap.android.map

import dev.fitmap.android.BuildConfig
import org.maplibre.android.maps.Style
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.layers.RasterLayer
import org.maplibre.android.style.layers.SymbolLayer
import org.maplibre.android.style.sources.RasterSource
import org.maplibre.android.style.sources.TileSet
import org.maplibre.android.style.sources.VectorSource

/**
 * The three mutually exclusive views `docs/IMPLEMENTATION.md` §4.2.2 names — one toggle, not
 * three checkboxes. Both Fog and Heatmap hide the track lines: the raster already encodes
 * where the user has been, and lines drawn on top of it read as noise rather than as extra
 * information.
 */
enum class MapMode { NORMAL, FOG, HEATMAP }

/**
 * The three user layers that sit on top of the served basemap: the live tracks MVT layer and
 * the two server-rendered raster masks.
 *
 * All three are behind `requireAuth` (`services/server/internal/httpapi/server.go`), which is
 * why they are attached and detached with the session rather than added once at style load —
 * a signed-out map is the basemap alone, and the basemap alone is a complete, working map.
 *
 * Layer ordering is the part that is awkward to retrofit, so it is fixed here: everything goes
 * *beneath* the basemap's first symbol layer. Fog painted over place labels buries them and
 * reads as a rendering bug rather than a design choice. Within that, fog and heatmap are added
 * before tracks so the lines paint above the veil — a cleared route should be visible through
 * the fog, not hidden under it.
 */
object MapOverlays {

    /** Mirrors `tilesPrefix` in `services/server/internal/httpapi/server.go`. */
    private const val TILES_V1 = "/tiles/v1"

    private const val TRACKS_SOURCE_ID = "tracks"
    private const val TRACKS_LAYER_ID = "tracks-line"

    /** Must match `ST_AsMVT(t, 'tracks', ...)` in the backend's own tile query. */
    private const val TRACKS_SOURCE_LAYER = "tracks"

    private const val FOG_SOURCE_ID = "fog"
    private const val FOG_LAYER_ID = "fog-raster"
    private const val HEATMAP_SOURCE_ID = "heatmap"
    private const val HEATMAP_LAYER_ID = "heatmap-raster"

    /**
     * The basemap archive's own maximum zoom (`docs/IMPLEMENTATION.md` §5.4), and the same
     * ceiling the server renders fog and heatmap tiles to. MapLibre overzooms past a declared
     * maximum rather than stopping, so the layers stay drawn as the user keeps zooming in.
     */
    private const val MAX_ZOOM = 14f

    /** Matches `internal/fog.TileSize` — the server renders 512px masks, not 256px ones. */
    private const val RASTER_TILE_SIZE = 512

    private const val TRACK_COLOR = "#b07e2e"
    private const val TRACK_WIDTH = 2.5f
    private const val TRACK_OPACITY = 0.9f

    /**
     * Adds all three layers, hidden or visible per [mode]. Safe to call against a style that
     * already has them: a style reload (a day/night flavor swap) discards custom layers, so
     * this has to be re-runnable rather than one-shot.
     *
     * Every tile URL here is unfiltered. All three endpoints accept `from`/`to`/`types`/
     * `exclude`, and the web client drives them from its date-range picker and hidden set —
     * neither of which the Android app has yet. Those controls are part of the design pass
     * (`apps/android/docs/ROADMAP.md`'s note on Phase 3 of the root roadmap), and an omitted
     * filter already means "no restriction" server-side, so the unfiltered URL is the whole
     * history rather than a placeholder for something missing.
     */
    fun attach(style: Style, mode: MapMode) {
        val beforeId = labelInsertionPoint(style)
        addRaster(style, FOG_SOURCE_ID, FOG_LAYER_ID, tileUrl("fog", "png"), beforeId)
        addRaster(style, HEATMAP_SOURCE_ID, HEATMAP_LAYER_ID, tileUrl("heatmap", "png"), beforeId)
        addTracks(style, beforeId)
        setMode(style, mode)
    }

    /** Removes all three, layers before sources — a source still in use cannot be removed. */
    fun detach(style: Style) {
        for (layerId in listOf(TRACKS_LAYER_ID, HEATMAP_LAYER_ID, FOG_LAYER_ID)) {
            style.removeLayer(layerId)
        }
        for (sourceId in listOf(TRACKS_SOURCE_ID, HEATMAP_SOURCE_ID, FOG_SOURCE_ID)) {
            style.removeSource(sourceId)
        }
    }

    fun setMode(style: Style, mode: MapMode) {
        setVisible(style, FOG_LAYER_ID, mode == MapMode.FOG)
        setVisible(style, HEATMAP_LAYER_ID, mode == MapMode.HEATMAP)
        setVisible(style, TRACKS_LAYER_ID, mode == MapMode.NORMAL)
    }

    /**
     * The id of the basemap's first label layer, which is what everything above is inserted
     * beneath. Null for a style with no labels at all, in which case the caller appends — there
     * is nothing to stay underneath.
     */
    private fun labelInsertionPoint(style: Style): String? =
        style.layers.firstOrNull { it is SymbolLayer }?.id

    private fun addRaster(style: Style, sourceId: String, layerId: String, url: String, beforeId: String?) {
        if (style.getSource(sourceId) == null) {
            style.addSource(RasterSource(sourceId, tileSet(url), RASTER_TILE_SIZE))
        }
        if (style.getLayer(layerId) == null) {
            insert(style, RasterLayer(layerId, sourceId), beforeId)
        }
    }

    private fun addTracks(style: Style, beforeId: String?) {
        if (style.getSource(TRACKS_SOURCE_ID) == null) {
            style.addSource(VectorSource(TRACKS_SOURCE_ID, tileSet(tileUrl("tracks", "mvt"))))
        }
        if (style.getLayer(TRACKS_LAYER_ID) == null) {
            val layer = LineLayer(TRACKS_LAYER_ID, TRACKS_SOURCE_ID)
                .withSourceLayer(TRACKS_SOURCE_LAYER)
                .withProperties(
                    PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
                    PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
                    PropertyFactory.lineColor(TRACK_COLOR),
                    PropertyFactory.lineWidth(TRACK_WIDTH),
                    PropertyFactory.lineOpacity(TRACK_OPACITY),
                )
            insert(style, layer, beforeId)
        }
    }

    private fun insert(style: Style, layer: org.maplibre.android.style.layers.Layer, beforeId: String?) {
        if (beforeId != null) style.addLayerBelow(layer, beforeId) else style.addLayer(layer)
    }

    private fun setVisible(style: Style, layerId: String, visible: Boolean) {
        val layer = style.getLayer(layerId) ?: return
        layer.setProperties(PropertyFactory.visibility(if (visible) Property.VISIBLE else Property.NONE))
    }

    /** "2.2.0" is the TileJSON version this describes, not the tile set's own version. */
    private fun tileSet(url: String) = TileSet("2.2.0", url).apply {
        minZoom = 0f
        maxZoom = MAX_ZOOM
    }

    private fun tileUrl(kind: String, extension: String): String =
        "${BuildConfig.API_BASE_URL}$TILES_V1/$kind/{z}/{x}/{y}.$extension"
}
