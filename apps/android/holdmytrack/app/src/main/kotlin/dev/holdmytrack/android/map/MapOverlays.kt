package dev.holdmytrack.android.map

import android.net.Uri
import dev.holdmytrack.android.BuildConfig
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.TrackMetricPoint
import dev.holdmytrack.android.recording.RecordedPoint
import org.maplibre.android.maps.Style
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.BackgroundLayer
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
import org.maplibre.geojson.Feature
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
    const val TRACKS_LAYER_ID = "tracks-line"

    /** The selected track's dark halo, drawn under every line, and the selected line itself,
     *  wider, over them — the web's `tracks-casing` and its `selected` feature-state width
     *  (`apps/web/src/map/tracks.ts`). MapLibre Android has no feature-state, so both are
     *  their own layers, filtered to the one selected id. */
    private const val TRACKS_CASING_LAYER_ID = "tracks-casing"
    private const val TRACKS_SELECTED_LAYER_ID = "tracks-selected"

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

    /** Heatmap mode's wash over the basemap, beneath the heat — the web's `heatmap-dim`. */
    private const val HEATMAP_DIM_LAYER_ID = "heatmap-dim"

    /** Must match `ST_AsMVT(t, 'countries'/'regions', ...)` in the backend's own tile queries. */
    private const val COUNTRIES_SOURCE_LAYER = "countries"
    private const val REGIONS_SOURCE_LAYER = "regions"

    /**
     * The three zoom-dependent tiers Fog and Heatmap fall back to below city zoom
     * (`docs/IMPLEMENTATION.md` §4.2.4), mirroring `apps/web/src/map/zoomTiers.ts` exactly so
     * the boundary can't drift between the two clients. Adopted starting bands, tunable
     * visually, not scientifically derived.
     */
    private const val COUNTRY_MAX_ZOOM = 3f
    private const val REGION_MIN_ZOOM = 3f
    private const val REGION_MAX_ZOOM = 7f
    private const val CITY_MIN_ZOOM = 7f

    /**
     * Tracks draw from z4 inward — not [CITY_MIN_ZOOM]: Normal mode has no Country/Region
     * fallback, so hiding tracks below z7 left an empty map, and a road trip too long to fit
     * at z7 had nothing drawn once the camera fit it. Mirrors `apps/web/src/map/tracks.ts`'s
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

    /** The web's `EMPHASIS_WIDTH` and `CASING_WIDTH` (1.5 of halo showing each side). */
    private const val SELECTED_WIDTH = 4.5f
    private const val CASING_WIDTH = SELECTED_WIDTH + 3f
    private const val CASING_COLOR = "#202b25"
    private const val CASING_OPACITY = 0.95f

    /** The selected activity's pace bands (`TrackBands`), the web's `track-bands`:
     *  one coloured line per run, over its track, under the labels. */
    private const val BAND_SOURCE_ID = "track-bands"
    private const val BAND_LAYER_ID = "track-bands-line"
    private const val BAND_WIDTH = 6f
    private const val BAND_OPACITY = 0.95f

    /** The track layers' ids, in paint order — what mode and recording show and hide together. */
    private val TRACK_LAYER_IDS = listOf(TRACKS_CASING_LAYER_ID, TRACKS_LAYER_ID, TRACKS_SELECTED_LAYER_ID)

    /** Which tracks aren't painted, and which one is selected — kept here rather than only on
     *  the layers, because [setTrackRange] replaces the layers and has to put both back. */
    private var hiddenTracks: Set<String> = emptySet()
    private var selectedTrack: String? = null

    /** The open Story the tracks are narrowed to, or null — kept here for the same reason, so
     *  every replacement of the tracks source carries it ([setTrackStory]). */
    private var trackStory: String? = null

    /**
     * Bumped by [refreshTracks] and sent as `v` (the server ignores it), the web's
     * `tracksVersion`: every tile URL also carries the account's tile version as `cv`
     * ([Session.tileVersion]), under which MapLibre keeps the tile for good, and a change the
     * app hasn't read a new version for yet (a type edit) would otherwise be answered from that
     * cache. Fog and Heatmap need no counter of their own — they only change with the version.
     */
    private var tracksVersion = 0

    /** A track is being edited (`TrackEditOverlay`): every other track and the bands step
     *  aside for it, as the web's `setMapMode(…, editingTrack)` has them. */
    private var editingTrack = false

    /** Every Fog and Heatmap layer and the source under it — what [refreshCoverage] replaces. */
    private val COVERAGE_LAYERS = listOf(
        FOG_LAYER_ID to FOG_SOURCE_ID,
        HEATMAP_LAYER_ID to HEATMAP_SOURCE_ID,
        COUNTRY_FOG_LAYER_ID to COUNTRY_FOG_SOURCE_ID,
        COUNTRY_HEATMAP_LAYER_ID to COUNTRY_HEATMAP_SOURCE_ID,
        REGION_FOG_LAYER_ID to REGION_FOG_SOURCE_ID,
        REGION_HEATMAP_LAYER_ID to REGION_HEATMAP_SOURCE_ID,
    )

    /** The same two veils as the raster tier's (`internal/fog/raster.go`'s LightVeil and
     *  DarkVeil), each the opposite of the basemap under it: dark ink over the light flavor, a
     *  cream mist over the dark one. */
    private const val FOG_FILL_COLOR = "#202b25"
    private const val FOG_FILL_OPACITY = 0.82f
    private const val FOG_FILL_COLOR_DARK = "#f7f4ec"
    private const val FOG_FILL_OPACITY_DARK = 0.6f

    /** Whether the style [attach] last drew on is the dark flavor — which veil and wash
     *  [addCoverage] uses, and keeps using across a [refreshCoverage]. */
    private var darkVeil = false

    /** The heatmap ramp's own base colour (the server's `heatmapRamp`, the deep red a single
     *  visit is drawn in) at a fixed opacity — "you've been somewhere in this country," not
     *  graded by how much. The web's `heatmap.ts` uses the same pair. */
    private const val HEATMAP_FILL_COLOR = "#b3261e"
    private const val HEATMAP_FILL_OPACITY = 0.55f

    /** The wash's two colors, the web's `LIGHT_DIM` and `DARK_DIM` (`heatmap.ts`): the heat
     *  reads against a quieter map, as Fog mode quiets the labels. Cream on the light flavor,
     *  black on the dark one and satellite — the fog veils' own pairing, lightened. */
    private const val HEATMAP_DIM_COLOR = "#f7f4ec"
    private const val HEATMAP_DIM_OPACITY = 0.45f
    private const val HEATMAP_DIM_COLOR_DARK = "#000000"
    private const val HEATMAP_DIM_OPACITY_DARK = 0.35f

    /** How far Fog mode mutes basemap labels — the web's `FOG_LABEL_OPACITY`: dim enough to
     *  recede behind the veil, still readable enough to orient by. */
    private const val FOG_LABEL_OPACITY = 0.4f

    /** Every user layer [attach] adds — what [setRecording] hides wholesale. */
    private val USER_LAYER_IDS = listOf(
        HEATMAP_DIM_LAYER_ID, FOG_LAYER_ID, HEATMAP_LAYER_ID,
        COUNTRY_FOG_LAYER_ID, COUNTRY_HEATMAP_LAYER_ID,
        REGION_FOG_LAYER_ID, REGION_HEATMAP_LAYER_ID,
    ) + TRACK_LAYER_IDS + BAND_LAYER_ID

    private const val LIVE_TRACK_SOURCE_ID = "live-track"
    private const val LIVE_TRACK_LAYER_ID = "live-track-line"
    private const val LIVE_POSITION_SOURCE_ID = "live-position"
    private const val LIVE_POSITION_LAYER_ID = "live-position-dot"
    private const val LIVE_TRACK_WIDTH = 4f
    private const val LIVE_POSITION_RADIUS = 6f

    /**
     * Adds all three layers, hidden or visible per [mode], with the tracks narrowed to [range]
     * (null draws the whole history). Safe to call against a style that already has them: a
     * style reload (a day/night flavor swap) discards custom layers, so this has to be
     * re-runnable rather than one-shot.
     *
     * Only the tracks tile carries a filter: `from`/`to`, or an open Story's `story` in their
     * place ([setTrackStory]) — the same as the web, whose date range and Story narrow the
     * tracks alone: Fog and Heatmap show coverage no filter narrows
     * (`docs/SPEC.md` FR-4.2, FR-4.3). The Activities panel's TYPE/DISTANCE filters, hidden
     * set and selection are applied on the client, as layer filters ([setTrackFilter]).
     *
     * [dark] says the basemap reads dark — the dark flavor, or satellite imagery — so Fog gets
     * the cream veil and Heatmap the black wash ([setDarkVeil]); [story] is
     * the open Story, or null ([setTrackStory]).
     */
    fun attach(style: Style, mode: MapMode, range: DateRange?, dark: Boolean, story: String?) {
        darkVeil = dark
        trackStory = story
        val beforeId = labelInsertionPoint(style)
        addCoverage(style, beforeId)
        addTracks(style, beforeId, range)
        addBands(style, beforeId)
        setMode(style, mode)
    }

    private fun addBands(style: Style, beforeId: String?) {
        if (style.getSource(BAND_SOURCE_ID) == null) style.addSource(GeoJsonSource(BAND_SOURCE_ID))
        if (style.getLayer(BAND_LAYER_ID) == null) {
            val layer = LineLayer(BAND_LAYER_ID, BAND_SOURCE_ID).withProperties(
                PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
                PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
                PropertyFactory.lineColor(Expression.get("color")),
                PropertyFactory.lineWidth(BAND_WIDTH),
                PropertyFactory.lineOpacity(BAND_OPACITY),
            )
            insert(style, layer, beforeId)
        }
    }

    /**
     * Colours [points] — the selected activity's track, `GET /v1/activities/track-metrics` —
     * by pace, one line per band run; an empty list clears it. The web's `setTrackBands`.
     */
    fun setTrackBands(style: Style, points: List<TrackMetricPoint>) {
        val source = style.getSourceAs<GeoJsonSource>(BAND_SOURCE_ID) ?: return
        if (points.size < 2) {
            source.setGeoJson(FeatureCollection.fromFeatures(emptyList()))
            return
        }
        val runs = TrackBands.runs(points, TrackBands.scale(points))
        val features = runs.map { run ->
            val line = LineString.fromLngLats(points.subList(run.startIndex, run.endIndex + 1).map { Point.fromLngLat(it.lon, it.lat) })
            Feature.fromGeometry(line).apply { addStringProperty("color", TrackBands.COLORS[run.band]) }
        }
        source.setGeoJson(FeatureCollection.fromFeatures(features))
    }

    fun clearTrackBands(style: Style) = setTrackBands(style, emptyList())

    /** Fog and Heatmap at every tier, each inserted below [beforeId] — Heatmap's wash first,
     *  so every heat layer added after it lands above it. */
    private fun addCoverage(style: Style, beforeId: String?) {
        if (style.getLayer(HEATMAP_DIM_LAYER_ID) == null) {
            val dim = BackgroundLayer(HEATMAP_DIM_LAYER_ID).withProperties(
                PropertyFactory.backgroundColor(if (darkVeil) HEATMAP_DIM_COLOR_DARK else HEATMAP_DIM_COLOR),
                PropertyFactory.backgroundOpacity(if (darkVeil) HEATMAP_DIM_OPACITY_DARK else HEATMAP_DIM_OPACITY),
                PropertyFactory.visibility(Property.NONE),
            )
            insert(style, dim, beforeId)
        }
        val fogUrl = coverageUrl("fog", "png").let { if (darkVeil) it + (if ('?' in it) "&" else "?") + "theme=dark" else it }
        val fogColor = if (darkVeil) FOG_FILL_COLOR_DARK else FOG_FILL_COLOR
        val fogOpacity = if (darkVeil) FOG_FILL_OPACITY_DARK else FOG_FILL_OPACITY
        addRaster(style, FOG_SOURCE_ID, FOG_LAYER_ID, fogUrl, beforeId, minZoom = CITY_MIN_ZOOM)
        addRaster(style, HEATMAP_SOURCE_ID, HEATMAP_LAYER_ID, coverageUrl("heatmap", "png"), beforeId, minZoom = CITY_MIN_ZOOM)
        addFill(
            style, COUNTRY_FOG_SOURCE_ID, COUNTRY_FOG_LAYER_ID, COUNTRIES_SOURCE_LAYER,
            coverageUrl("country-fog", "mvt"), beforeId, 0f, COUNTRY_MAX_ZOOM, fogColor, fogOpacity,
        )
        addFill(
            style, COUNTRY_HEATMAP_SOURCE_ID, COUNTRY_HEATMAP_LAYER_ID, COUNTRIES_SOURCE_LAYER,
            coverageUrl("country-heatmap", "mvt"), beforeId, 0f, COUNTRY_MAX_ZOOM, HEATMAP_FILL_COLOR, HEATMAP_FILL_OPACITY,
        )
        addFill(
            style, REGION_FOG_SOURCE_ID, REGION_FOG_LAYER_ID, REGIONS_SOURCE_LAYER,
            coverageUrl("region-fog", "mvt"), beforeId, REGION_MIN_ZOOM, REGION_MAX_ZOOM, fogColor, fogOpacity,
        )
        addFill(
            style, REGION_HEATMAP_SOURCE_ID, REGION_HEATMAP_LAYER_ID, REGIONS_SOURCE_LAYER,
            coverageUrl("region-heatmap", "mvt"), beforeId, REGION_MIN_ZOOM, REGION_MAX_ZOOM, HEATMAP_FILL_COLOR, HEATMAP_FILL_OPACITY,
        )
    }

    /**
     * Switches Fog to the other veil, and Heatmap to the other wash, when the basemap under it changes lightness without a
     * style reload — satellite imagery (`MapSatellite`) reads dark, so it takes the dark
     * flavor's cream veil, like the web's `isDarkBase`. Re-adds the coverage layers the way
     * [refreshCoverage] does, since their colors and tile URLs are fixed when added.
     */
    fun setDarkVeil(style: Style, dark: Boolean) {
        if (darkVeil == dark) return
        darkVeil = dark
        refreshCoverage(style)
    }

    /** The tracks tiles fetched again for [range] — after a delete, or a reprocess landing. */
    fun refreshTracks(style: Style, range: DateRange?) {
        tracksVersion += 1
        setTrackRange(style, range)
    }

    /**
     * Every Fog and Heatmap tile fetched again, once the server has re-rendered them
     * (`CoverageWatch`) — the web's `refreshFogLayers`/`refreshHeatmapLayers`. Replaced, not
     * updated, for the same reason as [setTrackRange], each at the same place in the stack —
     * under the tracks — and with its old visibility.
     */
    fun refreshCoverage(style: Style) {
        if (style.getLayer(FOG_LAYER_ID) == null) return
        val visibility = (COVERAGE_LAYERS.map { it.first } + HEATMAP_DIM_LAYER_ID)
            .associateWith { style.getLayer(it)?.visibility?.value }
        COVERAGE_LAYERS.forEach { (layer, source) ->
            style.getLayer(layer)?.let(style::removeLayer)
            style.removeSource(source)
        }
        style.getLayer(HEATMAP_DIM_LAYER_ID)?.let(style::removeLayer)
        val beforeId = if (style.getLayer(TRACKS_CASING_LAYER_ID) != null) TRACKS_CASING_LAYER_ID else labelInsertionPoint(style)
        addCoverage(style, beforeId)
        visibility.forEach { (layer, value) ->
            if (value != null) style.getLayer(layer)?.setProperties(PropertyFactory.visibility(value))
        }
    }

    /**
     * Narrows the tracks to [range]. MapLibre Native has no way to change a vector source's
     * tile URL in place, so the tracks layer and its source are replaced, at the same place in
     * the layer stack and with the old layer's visibility — whatever mode or recording state
     * had set it.
     */
    fun setTrackRange(style: Style, range: DateRange?) {
        val old = style.getLayer(TRACKS_LAYER_ID) ?: return
        val visibility = old.visibility.value
        TRACK_LAYER_IDS.forEach { id -> style.getLayer(id)?.let(style::removeLayer) }
        style.removeSource(TRACKS_SOURCE_ID)
        // Back under the bands, which stay drawn over them.
        val beforeId = if (style.getLayer(BAND_LAYER_ID) != null) BAND_LAYER_ID else labelInsertionPoint(style)
        addTracks(style, beforeId, range)
        TRACK_LAYER_IDS.forEach { id -> style.getLayer(id)?.setProperties(PropertyFactory.visibility(visibility)) }
    }

    /** Narrows the tracks to [story]'s activities, all of them — the tile's `story` filter
     *  (`docs/SPEC.md` FR-14.4), with no range — or, with null, back to [range]'s. */
    fun setTrackStory(style: Style, story: String?, range: DateRange?) {
        trackStory = story
        setTrackRange(style, range)
    }

    /**
     * Leaves [hidden] off the map — filtered out by TYPE/DISTANCE, hidden with Show/Hide, or
     * Pending — and draws [selected] bold over its halo, unless it's one of those. The web's
     * `setHiddenTracks` and `setSelectedTracks`, as layer filters on the tile's `id` property.
     */
    fun setTrackFilter(style: Style, hidden: Set<String>, selected: String?) {
        hiddenTracks = hidden
        selectedTrack = selected
        applyTrackFilter(style)
    }

    private fun applyTrackFilter(style: Style) {
        val visible = if (hiddenTracks.isEmpty()) {
            Expression.literal(true)
        } else {
            Expression.not(Expression.`in`(Expression.get("id"), Expression.literal(hiddenTracks.toTypedArray<Any>())))
        }
        val selected = selectedTrack?.takeIf { it !in hiddenTracks }
        val only = if (selected == null) Expression.literal(false) else Expression.eq(Expression.get("id"), selected)
        (style.getLayer(TRACKS_LAYER_ID) as? LineLayer)?.setFilter(visible)
        (style.getLayer(TRACKS_CASING_LAYER_ID) as? LineLayer)?.setFilter(only)
        (style.getLayer(TRACKS_SELECTED_LAYER_ID) as? LineLayer)?.setFilter(only)
    }

    /** See [editingTrack]. */
    fun setEditingTrack(style: Style, editing: Boolean, mode: MapMode) {
        editingTrack = editing
        setMode(style, mode)
    }

    fun setMode(style: Style, mode: MapMode) {
        setVisible(style, FOG_LAYER_ID, mode == MapMode.FOG)
        setVisible(style, COUNTRY_FOG_LAYER_ID, mode == MapMode.FOG)
        setVisible(style, REGION_FOG_LAYER_ID, mode == MapMode.FOG)
        setVisible(style, HEATMAP_DIM_LAYER_ID, mode == MapMode.HEATMAP)
        setVisible(style, HEATMAP_LAYER_ID, mode == MapMode.HEATMAP)
        setVisible(style, COUNTRY_HEATMAP_LAYER_ID, mode == MapMode.HEATMAP)
        setVisible(style, REGION_HEATMAP_LAYER_ID, mode == MapMode.HEATMAP)
        TRACK_LAYER_IDS.forEach { setVisible(style, it, mode == MapMode.NORMAL && !editingTrack) }
        setVisible(style, BAND_LAYER_ID, mode == MapMode.NORMAL && !editingTrack)
        setLabelOpacity(style, if (mode == MapMode.FOG) FOG_LABEL_OPACITY else 1f)
    }

    /**
     * Basemap labels stay above the veil so they're legible, but at full strength they read
     * brighter than the fog itself and pull the eye off what's actually been cleared — so Fog
     * mode mutes them, as the web does (`apps/web/src/map/mapMode.ts`). Every symbol layer on
     * the map is the basemap's own except the Spots badges ([MapSpots.BADGE_LAYER_IDS]), which
     * Fog must not dim, and the served style never sets text-/icon-opacity, so 1 restores
     * exactly its default.
     */
    private fun setLabelOpacity(style: Style, opacity: Float) {
        style.layers.filterIsInstance<SymbolLayer>().filter { it.id !in MapSpots.BADGE_LAYER_IDS }.forEach {
            it.setProperties(PropertyFactory.textOpacity(opacity), PropertyFactory.iconOpacity(opacity))
        }
    }

    /**
     * While a recording is in progress the map shows that recording and nothing else — no
     * history tracks, no fog, no heatmap, whichever [mode] was selected. Ending it restores
     * [mode]. Only the user layers are touched; the live track ([attachLiveTrack]) is always
     * on and simply empty when nothing is recording.
     */
    fun setRecording(style: Style, recording: Boolean, mode: MapMode) {
        if (recording) {
            USER_LAYER_IDS.forEach { setVisible(style, it, false) }
            setLabelOpacity(style, 1f)
        } else {
            setMode(style, mode)
        }
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

    private fun addTracks(style: Style, beforeId: String?, range: DateRange?) {
        if (style.getSource(TRACKS_SOURCE_ID) == null) {
            val params = buildList {
                // An open Story is drawn whole, whatever the range (`docs/SPEC.md` FR-14.6).
                val story = trackStory
                if (story != null) {
                    add("story=$story")
                } else if (range != null) {
                    add("from=${range.from}")
                    add("to=${range.to}")
                }
                Session.tileVersion.takeIf { it.isNotEmpty() }?.let { add("cv=${Uri.encode(it)}") }
                if (tracksVersion > 0) add("v=$tracksVersion")
            }
            val query = if (params.isEmpty()) "" else params.joinToString("&", prefix = "?")
            style.addSource(VectorSource(TRACKS_SOURCE_ID, tileSet(tileUrl("tracks", "mvt") + query)))
        }
        // Halo first, then every line, then the selected line, all under the labels.
        addTrackLine(style, TRACKS_CASING_LAYER_ID, beforeId, CASING_COLOR, CASING_WIDTH, CASING_OPACITY)
        addTrackLine(style, TRACKS_LAYER_ID, beforeId, TRACK_COLOR, TRACK_WIDTH, TRACK_OPACITY)
        addTrackLine(style, TRACKS_SELECTED_LAYER_ID, beforeId, TRACK_COLOR, SELECTED_WIDTH, 1f)
        applyTrackFilter(style)
    }

    private fun addTrackLine(style: Style, layerId: String, beforeId: String?, color: String, width: Float, opacity: Float) {
        if (style.getLayer(layerId) != null) return
        val layer = LineLayer(layerId, TRACKS_SOURCE_ID)
            .withSourceLayer(TRACKS_SOURCE_LAYER)
            .withProperties(
                PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
                PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
                PropertyFactory.lineColor(color),
                PropertyFactory.lineWidth(width),
                PropertyFactory.lineOpacity(opacity),
            )
            .apply { setMinZoom(TRACKS_MIN_ZOOM) }
        insert(style, layer, beforeId)
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

    private fun coverageUrl(kind: String, extension: String): String =
        tileUrl(kind, extension) + Session.tileVersion.let { if (it.isEmpty()) "" else "?cv=${Uri.encode(it)}" }
}
