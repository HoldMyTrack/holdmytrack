package dev.holdmytrack.android.map

import dev.holdmytrack.android.BuildConfig
import dev.holdmytrack.android.recording.RecordedPoint
import org.maplibre.android.maps.Style
import org.maplibre.android.style.layers.CircleLayer
import org.maplibre.android.style.layers.FillLayer
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.layers.RasterLayer
import org.maplibre.android.style.layers.SymbolLayer
import org.maplibre.android.style.sources.GeoJsonSource
import org.maplibre.android.style.sources.RasterSource
import org.maplibre.android.style.sources.TileSet
import org.maplibre.android.style.sources.VectorSource
import org.maplibre.geojson.FeatureCollection
import org.maplibre.geojson.LineString
import org.maplibre.geojson.Point

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
 * why they are attached only once the stored session is verified rather than at style load —
 * an unverified token would otherwise surface as a wall of 401s on tile requests.
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

    private const val COUNTRY_FOG_SOURCE_ID = "country-fog"
    private const val COUNTRY_FOG_LAYER_ID = "country-fog-fill"
    private const val COUNTRY_HEATMAP_SOURCE_ID = "country-heatmap"
    private const val COUNTRY_HEATMAP_LAYER_ID = "country-heatmap-fill"
    private const val REGION_FOG_SOURCE_ID = "region-fog"
    private const val REGION_FOG_LAYER_ID = "region-fog-fill"
    private const val REGION_HEATMAP_SOURCE_ID = "region-heatmap"
    private const val REGION_HEATMAP_LAYER_ID = "region-heatmap-fill"

    /** Must match `ST_AsMVT(t, 'countries'/'regions', ...)` in the backend's own tile queries. */
    private const val COUNTRIES_SOURCE_LAYER = "countries"
    private const val REGIONS_SOURCE_LAYER = "regions"

    /**
     * The three zoom-dependent tiers Fog and Heatmap fall back to below city zoom
     * (`docs/IMPLEMENTATION.md` §4.2.4), mirroring `apps/web/src/map/zoomTiers.ts` exactly so
     * the boundary can't drift between the two clients. Adopted starting bands, tunable
     * visually, not scientifically derived.
     */
    private const val COUNTRY_MAX_ZOOM = 5f
    private const val REGION_MIN_ZOOM = 5f
    private const val REGION_MAX_ZOOM = 8f
    private const val CITY_MIN_ZOOM = 8f

    /**
     * Tracks draw from z4 inward — not [CITY_MIN_ZOOM]: Normal mode has no Country/Region
     * fallback, so hiding tracks below z8 left an empty map, and a road trip too long to fit
     * at z8 had nothing drawn once the camera fit it. Mirrors `apps/web/src/map/tracks.ts`'s
     * `TRACKS_MIN_ZOOM` (`docs/IMPLEMENTATION.md` §5.3 has the tile-size measurement).
     */
    private const val TRACKS_MIN_ZOOM = 4f

    /**
     * The basemap archive's own maximum zoom (`docs/IMPLEMENTATION.md` §5.4), and the same
     * ceiling the server renders fog and heatmap tiles to. MapLibre overzooms past a declared
     * maximum rather than stopping, so the layers stay drawn as the user keeps zooming in.
     */
    private const val MAX_ZOOM = 14f

    /** Matches `internal/fog.TileSize` — the server renders 512px masks, not 256px ones. */
    private const val RASTER_TILE_SIZE = 512

    const val TRACK_COLOR = "#b07e2e"
    private const val TRACK_WIDTH = 2.5f
    private const val TRACK_OPACITY = 0.9f

    /** Same dark veil colour/opacity as the raster tier's own fog_colour/fog_opacity
     *  (`internal/fog/raster.go`'s RenderFogPNG: #202b25 @ 0.82). */
    private const val FOG_FILL_COLOR = "#202b25"
    private const val FOG_FILL_OPACITY = 0.82f

    /** The heatmap ramp's own base hue (also `TRACK_COLOR` above) at a fixed moderate
     *  opacity — "you've been somewhere in this country," not graded by how much. */
    private const val HEATMAP_FILL_COLOR = "#b07e2e"
    private const val HEATMAP_FILL_OPACITY = 0.45f

    /** Every user layer [attach] adds — what [setRecording] hides wholesale. */
    private val USER_LAYER_IDS = listOf(
        FOG_LAYER_ID, HEATMAP_LAYER_ID,
        COUNTRY_FOG_LAYER_ID, COUNTRY_HEATMAP_LAYER_ID,
        REGION_FOG_LAYER_ID, REGION_HEATMAP_LAYER_ID,
        TRACKS_LAYER_ID,
    )

    private const val LIVE_TRACK_SOURCE_ID = "live-track"
    private const val LIVE_TRACK_LAYER_ID = "live-track-line"
    private const val LIVE_POSITION_SOURCE_ID = "live-position"
    private const val LIVE_POSITION_LAYER_ID = "live-position-dot"
    private const val LIVE_TRACK_WIDTH = 4f
    private const val LIVE_POSITION_RADIUS = 6f

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
        addRaster(style, FOG_SOURCE_ID, FOG_LAYER_ID, tileUrl("fog", "png"), beforeId, minZoom = CITY_MIN_ZOOM)
        addRaster(style, HEATMAP_SOURCE_ID, HEATMAP_LAYER_ID, tileUrl("heatmap", "png"), beforeId, minZoom = CITY_MIN_ZOOM)
        addFill(
            style, COUNTRY_FOG_SOURCE_ID, COUNTRY_FOG_LAYER_ID, COUNTRIES_SOURCE_LAYER,
            tileUrl("country-fog", "mvt"), beforeId, 0f, COUNTRY_MAX_ZOOM, FOG_FILL_COLOR, FOG_FILL_OPACITY,
        )
        addFill(
            style, COUNTRY_HEATMAP_SOURCE_ID, COUNTRY_HEATMAP_LAYER_ID, COUNTRIES_SOURCE_LAYER,
            tileUrl("country-heatmap", "mvt"), beforeId, 0f, COUNTRY_MAX_ZOOM, HEATMAP_FILL_COLOR, HEATMAP_FILL_OPACITY,
        )
        addFill(
            style, REGION_FOG_SOURCE_ID, REGION_FOG_LAYER_ID, REGIONS_SOURCE_LAYER,
            tileUrl("region-fog", "mvt"), beforeId, REGION_MIN_ZOOM, REGION_MAX_ZOOM, FOG_FILL_COLOR, FOG_FILL_OPACITY,
        )
        addFill(
            style, REGION_HEATMAP_SOURCE_ID, REGION_HEATMAP_LAYER_ID, REGIONS_SOURCE_LAYER,
            tileUrl("region-heatmap", "mvt"), beforeId, REGION_MIN_ZOOM, REGION_MAX_ZOOM, HEATMAP_FILL_COLOR, HEATMAP_FILL_OPACITY,
        )
        addTracks(style, beforeId)
        setMode(style, mode)
    }

    fun setMode(style: Style, mode: MapMode) {
        setVisible(style, FOG_LAYER_ID, mode == MapMode.FOG)
        setVisible(style, COUNTRY_FOG_LAYER_ID, mode == MapMode.FOG)
        setVisible(style, REGION_FOG_LAYER_ID, mode == MapMode.FOG)
        setVisible(style, HEATMAP_LAYER_ID, mode == MapMode.HEATMAP)
        setVisible(style, COUNTRY_HEATMAP_LAYER_ID, mode == MapMode.HEATMAP)
        setVisible(style, REGION_HEATMAP_LAYER_ID, mode == MapMode.HEATMAP)
        setVisible(style, TRACKS_LAYER_ID, mode == MapMode.NORMAL)
    }

    /**
     * While a recording is in progress the map shows that recording and nothing else — no
     * history tracks, no fog, no heatmap, whichever [mode] was selected. Ending it restores
     * [mode]. Only the user layers are touched; the live track ([attachLiveTrack]) is always
     * on and simply empty when nothing is recording.
     */
    fun setRecording(style: Style, recording: Boolean, mode: MapMode) {
        if (recording) USER_LAYER_IDS.forEach { setVisible(style, it, false) } else setMode(style, mode)
    }

    /**
     * The in-progress recording's line and a dot at its latest fix, drawn from the points
     * `RecordingService` holds on the device — not from the server, which has never seen them.
     * Added on top of everything, labels included: it's the one thing on the map that
     * matters while recording. Unlike [attach] this needs no session, so it goes on at style
     * load. Re-runnable, like [attach].
     */
    fun attachLiveTrack(style: Style) {
        if (style.getSource(LIVE_TRACK_SOURCE_ID) == null) style.addSource(GeoJsonSource(LIVE_TRACK_SOURCE_ID))
        if (style.getSource(LIVE_POSITION_SOURCE_ID) == null) style.addSource(GeoJsonSource(LIVE_POSITION_SOURCE_ID))
        if (style.getLayer(LIVE_TRACK_LAYER_ID) == null) {
            style.addLayer(
                LineLayer(LIVE_TRACK_LAYER_ID, LIVE_TRACK_SOURCE_ID).withProperties(
                    PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
                    PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
                    PropertyFactory.lineColor(TRACK_COLOR),
                    PropertyFactory.lineWidth(LIVE_TRACK_WIDTH),
                ),
            )
        }
        if (style.getLayer(LIVE_POSITION_LAYER_ID) == null) {
            style.addLayer(
                CircleLayer(LIVE_POSITION_LAYER_ID, LIVE_POSITION_SOURCE_ID).withProperties(
                    PropertyFactory.circleColor(TRACK_COLOR),
                    PropertyFactory.circleRadius(LIVE_POSITION_RADIUS),
                    PropertyFactory.circleStrokeColor("#ffffff"),
                    PropertyFactory.circleStrokeWidth(2f),
                ),
            )
        }
    }

    /** Redraws the live track from scratch — an empty list clears it. */
    fun updateLiveTrack(style: Style, points: List<RecordedPoint>) {
        val coordinates = points.map { Point.fromLngLat(it.lon, it.lat) }
        val empty = FeatureCollection.fromFeatures(emptyList())
        style.getSourceAs<GeoJsonSource>(LIVE_TRACK_SOURCE_ID)?.let { source ->
            if (coordinates.size < 2) source.setGeoJson(empty) else source.setGeoJson(LineString.fromLngLats(coordinates))
        }
        style.getSourceAs<GeoJsonSource>(LIVE_POSITION_SOURCE_ID)?.let { source ->
            val last = coordinates.lastOrNull()
            if (last == null) source.setGeoJson(empty) else source.setGeoJson(last)
        }
    }

    /**
     * The id of the basemap's first label layer, which is what everything above is inserted
     * beneath. Null for a style with no labels at all, in which case the caller appends — there
     * is nothing to stay underneath.
     */
    private fun labelInsertionPoint(style: Style): String? =
        style.layers.firstOrNull { it is SymbolLayer }?.id

    private fun addRaster(
        style: Style, sourceId: String, layerId: String, url: String, beforeId: String?, minZoom: Float,
    ) {
        if (style.getSource(sourceId) == null) {
            style.addSource(RasterSource(sourceId, tileSet(url), RASTER_TILE_SIZE))
        }
        if (style.getLayer(layerId) == null) {
            val layer = RasterLayer(layerId, sourceId).apply { setMinZoom(minZoom) }
            insert(style, layer, beforeId)
        }
    }

    /**
     * Adds one Country/Region tier fill layer — a flat colour fill over whichever polygons the
     * given endpoint returns (locked countries/regions for Fog, unlocked ones for Heatmap; see
     * `services/server/internal/httpapi/admin_country_tiles.go`/`admin_region_tiles.go`), gated
     * to [minZoom]/[maxZoom] so it and the raster/vector tiers it hands off to never both paint
     * at the same zoom.
     */
    private fun addFill(
        style: Style,
        sourceId: String,
        layerId: String,
        sourceLayer: String,
        url: String,
        beforeId: String?,
        minZoom: Float,
        maxZoom: Float,
        color: String,
        opacity: Float,
    ) {
        if (style.getSource(sourceId) == null) {
            style.addSource(VectorSource(sourceId, tileSet(url).apply { this.minZoom = minZoom; this.maxZoom = maxZoom }))
        }
        if (style.getLayer(layerId) == null) {
            val layer = FillLayer(layerId, sourceId)
                .withSourceLayer(sourceLayer)
                .withProperties(
                    PropertyFactory.fillColor(color),
                    PropertyFactory.fillOpacity(opacity),
                )
                .apply { setMinZoom(minZoom); setMaxZoom(maxZoom) }
            insert(style, layer, beforeId)
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
                .apply { setMinZoom(TRACKS_MIN_ZOOM) }
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
