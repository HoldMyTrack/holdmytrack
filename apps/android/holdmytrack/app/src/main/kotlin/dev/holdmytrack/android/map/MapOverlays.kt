package dev.holdmytrack.android.map

import android.net.Uri
import dev.holdmytrack.android.BuildConfig
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.TrackMetricPoint
import dev.holdmytrack.android.recording.RecordedPoint
import org.maplibre.android.maps.Style
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.CircleLayer
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.layers.SymbolLayer
import org.maplibre.android.style.sources.GeoJsonSource
import org.maplibre.android.style.sources.TileSet
import org.maplibre.android.style.sources.VectorSource
import org.maplibre.geojson.Feature
import org.maplibre.geojson.FeatureCollection
import org.maplibre.geojson.LineString
import org.maplibre.geojson.Point

/**
 * The three mutually exclusive views `docs/SPEC.md` FR-4.4 names — one toggle, not
 * three checkboxes. Both Fog and Heatmap hide the track lines: the veil or the heat already
 * shows where the user has been, and lines drawn on top of it read as noise rather than as extra
 * information.
 */
enum class MapMode { NORMAL, FOG, HEATMAP }

/** The three levels Fog and Heatmap draw at by zoom (`docs/IMPLEMENTATION.md` §4.2.4): whole
 *  countries, then whole states or provinces, then exactly where you've been. */
enum class ZoomTier { COUNTRY, REGION, CITY }

/**
 * The user layers on top of the served basemap: the live tracks MVT layer, and Fog's veil and
 * Heatmap's points as the style describes them ([CoverageLayers]).
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

    /**
     * Tracks draw from z4 inward, not from Fog's city tier (z7): Normal mode has no Country/Region
     * fallback, so hiding tracks below z7 left an empty map, and a road trip too long to fit
     * at z7 had nothing drawn once the camera fit it. Mirrors `apps/web/src/map/tracks.ts`'s
     * `TRACKS_MIN_ZOOM` (`docs/IMPLEMENTATION.md` §5.3 has the tile-size measurement).
     */
    const val TRACKS_MIN_ZOOM = 4f

    /**
     * The basemap archive's own maximum zoom (`docs/SPEC.md` FR-4.15), and the tracks tiles'.
     * MapLibre overzooms past a declared
     * maximum rather than stopping, so the layers stay drawn as the user keeps zooming in.
     */
    private const val MAX_ZOOM = 14f

    const val TRACK_COLOR = "#b07e2e"
    private const val TRACK_WIDTH = 2.5f
    private const val TRACK_OPACITY = 0.9f

    /** The web's `EMPHASIS_WIDTH` and `CASING_WIDTH` (1.5 of halo showing each side) — the
     *  Sync tab's picked candidate is drawn the same ([SyncCandidatesOverlay]). */
    const val SELECTED_WIDTH = 4.5f
    const val CASING_WIDTH = SELECTED_WIDTH + 3f
    const val CASING_COLOR = "#202b25"
    const val CASING_OPACITY = 0.95f

    /** The selected activity's pace bands (`TrackBands`), the web's `track-bands`:
     *  one coloured line per run, over its track, under the labels. */
    private const val BAND_SOURCE_ID = "track-bands"
    private const val BAND_LAYER_ID = "track-bands-line"
    const val BAND_WIDTH = 6f
    const val BAND_OPACITY = 0.95f

    /** The track layers' ids, in paint order — what mode and recording show and hide together. */
    private val TRACK_LAYER_IDS = listOf(TRACKS_CASING_LAYER_ID, TRACKS_LAYER_ID, TRACKS_SELECTED_LAYER_ID)

    /** Which tracks aren't painted, and which are drawn bold — the selected one, or the checked
     *  group — kept here rather than only on the layers, because [setTrackRange] replaces the
     *  layers and has to put both back. */
    private var hiddenTracks: Set<String> = emptySet()
    private var selectedTracks: Set<String> = emptySet()

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

    /** The Sync tab shows: the account's tracks and bands step aside, so the map draws only
     *  what's on offer (`SyncCandidatesOverlay`), not a slice of what's already synced. */
    private var syncing = false

    /** The tracks and their bands — what [setRecording] hides with the overlays. */
    private val USER_LAYER_IDS = TRACK_LAYER_IDS + BAND_LAYER_ID

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
     * Fog and Heatmap are the served style's ([CoverageLayers]); [satellite] says imagery is
     * drawn, which picks their view over it. [story] is the open Story, or null ([setTrackStory]).
     */
    fun attach(style: Style, mode: MapMode, range: DateRange?, satellite: Boolean, night: Boolean, story: String?) {
        trackStory = story
        val beforeId = labelInsertionPoint(style)
        CoverageLayers.load(style)
        CoverageLayers.add(style, beforeId)
        // Every path over the veil and the heat, under the tracks (`docs/SPEC.md` FR-4.13).
        MapPaths.raise(style, beforeId)
        MapBikePaths.add(style, beforeId, night)
        addTracks(style, beforeId, range)
        addBands(style, beforeId)
        CoverageLayers.apply(style, mode, satellite)
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
        source.setGeoJson(FeatureCollection.fromFeatures(bandFeatures(points)))
    }

    /** [points]' band runs as lines, each with its `color` — none under two points. */
    fun bandFeatures(points: List<TrackMetricPoint>): List<Feature> {
        if (points.size < 2) return emptyList()
        return TrackBands.runs(points, TrackBands.scale(points)).map { run ->
            val line = LineString.fromLngLats(points.subList(run.startIndex, run.endIndex + 1).map { Point.fromLngLat(it.lon, it.lat) })
            Feature.fromGeometry(line).apply { addStringProperty("color", TrackBands.COLORS[run.band]) }
        }
    }

    fun clearTrackBands(style: Style) = setTrackBands(style, emptyList())

    /** The tracks tiles fetched again for [range] — after a delete, or a reprocess landing. */
    fun refreshTracks(style: Style, range: DateRange?) {
        tracksVersion += 1
        setTrackRange(style, range)
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
    fun setTrackFilter(style: Style, hidden: Set<String>, selected: Set<String>) {
        hiddenTracks = hidden
        selectedTracks = selected
        applyTrackFilter(style)
    }

    private fun applyTrackFilter(style: Style) {
        val visible = if (hiddenTracks.isEmpty()) {
            Expression.literal(true)
        } else {
            Expression.not(Expression.`in`(Expression.get("id"), Expression.literal(hiddenTracks.toTypedArray<Any>())))
        }
        val selected = selectedTracks - hiddenTracks
        val only = if (selected.isEmpty()) {
            Expression.literal(false)
        } else {
            Expression.`in`(Expression.get("id"), Expression.literal(selected.toTypedArray<Any>()))
        }
        (style.getLayer(TRACKS_LAYER_ID) as? LineLayer)?.setFilter(visible)
        (style.getLayer(TRACKS_CASING_LAYER_ID) as? LineLayer)?.setFilter(only)
        (style.getLayer(TRACKS_SELECTED_LAYER_ID) as? LineLayer)?.setFilter(only)
    }

    /** See [editingTrack]. */
    fun setEditingTrack(style: Style, editing: Boolean, mode: MapMode) {
        editingTrack = editing
        setMode(style, mode)
    }

    /** See [syncing]. */
    fun setSyncing(style: Style, on: Boolean, mode: MapMode) {
        syncing = on
        setMode(style, mode)
    }

    /** [mode]'s view of Fog and Heatmap, labels included ([CoverageLayers]), and the tracks,
     *  which only Normal draws. */
    fun setMode(style: Style, mode: MapMode) {
        CoverageLayers.apply(style, mode)
        val tracks = mode == MapMode.NORMAL && !editingTrack && !syncing
        TRACK_LAYER_IDS.forEach { setVisible(style, it, tracks) }
        setVisible(style, BAND_LAYER_ID, tracks)
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
            CoverageLayers.apply(style, MapMode.NORMAL)
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
}
