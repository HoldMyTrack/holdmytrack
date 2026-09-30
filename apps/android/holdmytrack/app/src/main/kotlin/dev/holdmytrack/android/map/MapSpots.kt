package dev.holdmytrack.android.map

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.PointF
import android.graphics.RectF
import android.net.Uri
import androidx.annotation.DrawableRes
import androidx.annotation.StringRes
import androidx.appcompat.content.res.AppCompatResources
import androidx.core.content.edit
import androidx.core.graphics.createBitmap
import androidx.core.graphics.toColorInt
import dev.holdmytrack.android.BuildConfig
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.Spot
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.Style
import org.maplibre.android.style.expressions.Expression
import org.maplibre.android.style.layers.FillLayer
import org.maplibre.android.style.layers.Layer
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
import java.time.Instant

/**
 * The Spots layers (`docs/SPEC.md` FR-15, root `docs/IMPLEMENTATION.md` §4.25), the web's
 * `map/spots.ts`: outdoor places from OpenStreetMap, each a badge over its area, drawn in every
 * map mode for the categories the Layers menu has on ([LayersMenu]). The tiles are
 * `/tiles/v1/spots`, from zoom 13; below it, between zoom 10 and 13, "Show in this area"
 * ([ShowInArea]) loads a view's places on request into a GeoJSON source of its own.
 *
 * Unlike every other overlay they go on top of the whole stack, basemap labels included: a spot
 * is something to find, so neither the Fog veil nor a label may bury it. Fog's label dimming
 * ([MapOverlays]) leaves the two badge layers alone.
 *
 * The choice is per device, every category off until ticked, kept in SharedPreferences like
 * [MapPaths].
 */
object MapSpots {

    /** The categories, in the order the Layers menu lists them (the web's `SPOT_CATEGORIES`). */
    enum class Category(
        val wire: String,
        @param:DrawableRes val icon: Int,
        @param:StringRes val menuLabel: Int,
        @param:StringRes val label: Int,
    ) {
        PLAYGROUND("playground", R.drawable.ic_seesaw, R.string.layers_playgrounds, R.string.spots_category_playground),
        DOG_PARK("dog_park", R.drawable.ic_dog, R.string.layers_dog_parks, R.string.spots_category_dog_park),
        MONUMENT("monument", R.drawable.ic_landmark, R.string.layers_monuments, R.string.spots_category_monument),
        VIEWPOINT("viewpoint", R.drawable.ic_binoculars, R.string.layers_viewpoints, R.string.spots_category_viewpoint),
        HISTORY("history", R.drawable.ic_castle, R.string.layers_history, R.string.spots_category_history),
        ;

        companion object {
            fun fromWire(wire: String): Category? = entries.firstOrNull { it.wire == wire }
        }
    }

    /** The server sends nothing below it (internal/httpapi's `spotsMinZoom`): the paths' zoom,
     *  so places and paths appear together. */
    const val MIN_ZOOM = 13.0

    /** The lowest zoom "Show in this area" offers places at: a metro area in view. */
    const val IN_AREA_MIN_ZOOM = 10.0

    /** Past it the tiles are overzoomed, as the tracks tiles are past 14. */
    private const val MAX_ZOOM = 14f

    private const val SOURCE_ID = "spots"
    private const val IN_AREA_SOURCE_ID = "spots-in-area"
    private const val AREA_FILL_LAYER_ID = "spots-area-fill"
    private const val AREA_LINE_LAYER_ID = "spots-area-line"
    private const val CIRCLE_LINE_LAYER_ID = "spots-circle-line"
    private const val IN_AREA_LAYER_ID = "spots-in-area-icons"
    private const val LAYER_ID = "spots-icons"
    private const val GUIDE_SOURCE_ID = "spots-capture-guide"
    private const val GUIDE_LAYER_ID = "spots-capture-guide"

    /** Bottom to top: every area's fill, the outlines, the circles' dashed edges, "Show in this
     *  area"'s badges, the tiles' badges. The two badge layers never overlap: one stops where
     *  the other starts, at [MIN_ZOOM]. */
    private val LAYER_IDS = listOf(AREA_FILL_LAYER_ID, AREA_LINE_LAYER_ID, CIRCLE_LINE_LAYER_ID, IN_AREA_LAYER_ID, LAYER_ID)

    /** The badge layers — what a tap can hit, and what Fog's label dimming must skip. */
    val BADGE_LAYER_IDS = setOf(IN_AREA_LAYER_ID, LAYER_ID)

    // The backend query's two ST_AsMVT layers: one point per spot, and each spot's area.
    private const val SOURCE_LAYER = "spots"
    private const val AREA_SOURCE_LAYER = "spot_areas"

    // The badge: the category icon in ink on white, ringed in the accent — or, for a place the
    // account has captured, white on the accent, ringed darker. Fixed colors, not the theme's,
    // since the badge carries its own background and reads the same on either basemap.
    private const val INK = "#202b25"
    private const val ACCENT = "#b07e2e"
    private const val ACCENT_DARK = "#7d5820"
    private const val BADGE_DP = 30f

    /** The areas' fill: faint normally, darker in capture mode, darkest for its target. */
    private const val AREA_OPACITY = 0.15f
    private const val CAPTURING_AREA_OPACITY = 0.35f
    private const val TARGET_AREA_OPACITY = 0.55f

    /** How far from a badge's edge a tap still counts. */
    private const val TAP_SLOP_DP = 4f

    private const val PREFS = "map_spots"
    private const val KEY = "categories"

    private var wanted: List<Category> = emptyList()
    private var editing = false
    private var inArea: FeatureCollection = FeatureCollection.fromFeatures(emptyList())

    /** The places the account has captured (`docs/SPEC.md` FR-15.6), by id: drawn with the
     *  captured badge, and dated in their popup. */
    var captured: Map<Long, Instant> = emptyMap()
        private set

    /** The place capture mode is aiming at, or null with it off. */
    private var target: Long? = null

    /** Capture mode's dashed line, from the user to the nearest point of the target's area. */
    private var guide: FeatureCollection = FeatureCollection.fromFeatures(emptyList())

    fun get(context: Context): List<Category> {
        val saved = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getStringSet(KEY, emptySet()).orEmpty()
        return Category.entries.filter { it.wire in saved }
    }

    fun set(context: Context, categories: List<Category>) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit {
            putStringSet(KEY, categories.map { it.wire }.toSet())
        }
    }

    /**
     * Adds the badge images, both sources and the five layers if they aren't there yet, and
     * shows [categories]. Re-runnable: a style reload (a day/night swap) drops all of them.
     */
    fun attach(style: Style, context: Context, categories: List<Category>) {
        wanted = categories
        addImages(style, context)
        if (style.getSource(SOURCE_ID) == null) style.addSource(tileSource())
        if (style.getSource(IN_AREA_SOURCE_ID) == null) style.addSource(GeoJsonSource(IN_AREA_SOURCE_ID, inArea))
        if (style.getSource(GUIDE_SOURCE_ID) == null) style.addSource(GeoJsonSource(GUIDE_SOURCE_ID, guide))
        addLayers(style)
        apply(style)
    }

    /** The tiles fetched again at a new tile version — the import bumps it. Replaced, since a
     *  vector source's URL can't change in place, each layer back at its place in the stack. */
    fun refresh(style: Style) {
        if (style.getSource(SOURCE_ID) == null) return
        listOf(AREA_FILL_LAYER_ID, AREA_LINE_LAYER_ID, CIRCLE_LINE_LAYER_ID, LAYER_ID).forEach { id ->
            style.getLayer(id)?.let(style::removeLayer)
        }
        style.removeSource(SOURCE_ID)
        style.addSource(tileSource())
        addLayers(style)
        apply(style)
    }

    /** Shows [categories] and hides the rest. */
    fun setCategories(style: Style, categories: List<Category>) {
        wanted = categories
        apply(style)
    }

    /** The account's captures, newest from `GET /v1/spots/captures` or one just made. */
    fun setCaptured(style: Style?, captures: Map<Long, Instant>) {
        captured = captures
        style?.let(::apply)
    }

    /**
     * Capture mode (`apps/android/docs/SPEC.md` FR-2.8) on [targetId], or off with null: every
     * area shaded darker, the target's darkest and its edge thicker, so the places stand out
     * from the map while the user is looking for one.
     */
    fun setCapturing(style: Style?, targetId: Long?) {
        target = targetId
        if (targetId == null) guide = FeatureCollection.fromFeatures(emptyList())
        style?.let {
            it.getSourceAs<GeoJsonSource>(GUIDE_SOURCE_ID)?.setGeoJson(guide)
            apply(it)
        }
    }

    /** Capture mode's guide line, from [from] to [to]; null clears it (the user is inside). */
    fun setGuide(style: Style?, from: GeoPoint?, to: GeoPoint?) {
        guide = FeatureCollection.fromFeatures(
            if (from == null || to == null) emptyList()
            else listOf(Feature.fromGeometry(LineString.fromLngLats(listOf(Point.fromLngLat(from.lon, from.lat), Point.fromLngLat(to.lon, to.lat))))),
        )
        style?.getSourceAs<GeoJsonSource>(GUIDE_SOURCE_ID)?.setGeoJson(guide)
    }

    /** A track is being edited: the places step aside with every other track, as on the web
     *  (`docs/SPEC.md` FR-15.2). */
    fun setEditing(style: Style, next: Boolean) {
        editing = next
        apply(style)
    }

    /** Draws "Show in this area"'s places, replacing the last ones; null clears. Kept here too,
     *  so a style reload puts them back. */
    fun setInArea(style: Style?, spots: List<Spot>?) {
        inArea = FeatureCollection.fromFeatures(
            spots.orEmpty().map { spot ->
                Feature.fromGeometry(Point.fromLngLat(spot.lon, spot.lat)).apply {
                    addNumberProperty("id", spot.id)
                    addStringProperty("category", spot.category)
                    addNumberProperty("lon", spot.lon)
                    addNumberProperty("lat", spot.lat)
                    spot.name?.let { addStringProperty("name", it) }
                    spot.address?.let { addStringProperty("address", it) }
                    spot.description?.let { addStringProperty("description", it) }
                    spot.inscription?.let { addStringProperty("inscription", it) }
                    spot.memorial?.let { addStringProperty("memorial", it) }
                    spot.startDate?.let { addStringProperty("start_date", it) }
                    spot.wikipedia?.let { addStringProperty("wikipedia", it) }
                }
            },
        )
        style?.getSourceAs<GeoJsonSource>(IN_AREA_SOURCE_ID)?.setGeoJson(inArea)
    }

    /** The showing spot at a screen point, if any — from the tiles or from "Show in this
     *  area". Asked before the tracks, so a tap on a spot doesn't also select a track. */
    fun spotAt(map: MapLibreMap, screen: PointF, density: Float): Spot? {
        val style = map.style ?: return null
        val layers = BADGE_LAYER_IDS.filter { id -> style.getLayer(id)?.visibility?.value == Property.VISIBLE }
        if (layers.isEmpty()) return null
        val slop = TAP_SLOP_DP * density
        val box = RectF(screen.x - slop, screen.y - slop, screen.x + slop, screen.y + slop)
        return map.queryRenderedFeatures(box, *layers.toTypedArray()).firstOrNull()?.let(::toSpot)
    }

    private fun toSpot(feature: Feature): Spot {
        fun text(key: String) = feature.getStringProperty(key)?.takeIf { it.isNotEmpty() }
        fun number(key: String) = if (feature.hasNonNullValueForProperty(key)) feature.getNumberProperty(key) else null
        return Spot(
            id = number("id")?.toLong() ?: 0L,
            category = feature.getStringProperty("category").orEmpty(),
            name = text("name"),
            address = text("address"),
            description = text("description"),
            inscription = text("inscription"),
            memorial = text("memorial"),
            startDate = text("start_date"),
            wikipedia = text("wikipedia"),
            lon = number("lon")?.toDouble() ?: 0.0,
            lat = number("lat")?.toDouble() ?: 0.0,
        )
    }

    private fun tileSource(): VectorSource {
        val version = Session.tileVersion.let { if (it.isEmpty()) "" else "?cv=${Uri.encode(it)}" }
        // "2.2.0" is the TileJSON version this describes, not the tile set's own version.
        val tiles = TileSet("2.2.0", "${BuildConfig.API_BASE_URL}/tiles/v1/spots/{z}/{x}/{y}.mvt$version").apply {
            minZoom = MIN_ZOOM.toFloat()
            maxZoom = MAX_ZOOM
        }
        return VectorSource(SOURCE_ID, tiles)
    }

    private fun addLayers(style: Style) {
        val accent = ACCENT.toColorInt()
        add(style, FillLayer(AREA_FILL_LAYER_ID, SOURCE_ID).withSourceLayer(AREA_SOURCE_LAYER).withProperties(
            PropertyFactory.visibility(Property.NONE),
            PropertyFactory.fillColor(accent),
            PropertyFactory.fillOpacity(AREA_OPACITY),
        ).apply { minZoom = MIN_ZOOM.toFloat() })
        for (id in listOf(AREA_LINE_LAYER_ID, CIRCLE_LINE_LAYER_ID)) {
            // Two line layers, since a dash pattern can't vary per feature: the place's own OSM
            // outline solid, the 30 m circle standing in for a place mapped as a point dashed.
            val layer = LineLayer(id, SOURCE_ID).withSourceLayer(AREA_SOURCE_LAYER).withProperties(
                PropertyFactory.visibility(Property.NONE),
                PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
                PropertyFactory.lineColor(accent),
                PropertyFactory.lineWidth(lineWidth(null)),
            ).apply { minZoom = MIN_ZOOM.toFloat() }
            if (id == CIRCLE_LINE_LAYER_ID) layer.setProperties(PropertyFactory.lineDasharray(arrayOf(2f, 2f)))
            add(style, layer)
        }
        if (style.getLayer(GUIDE_LAYER_ID) == null) {
            // Under the badges, over the areas: not one of LAYER_IDS, since it isn't filtered
            // by category, and it's on its own source, which refresh leaves alone.
            val guideLayer = LineLayer(GUIDE_LAYER_ID, GUIDE_SOURCE_ID).withProperties(
                PropertyFactory.lineColor(accent),
                PropertyFactory.lineWidth(2.5f),
                PropertyFactory.lineDasharray(arrayOf(1.5f, 1.5f)),
                PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
            )
            if (style.getLayer(IN_AREA_LAYER_ID) != null) style.addLayerBelow(guideLayer, IN_AREA_LAYER_ID) else style.addLayer(guideLayer)
        }
        add(style, badges(IN_AREA_LAYER_ID, IN_AREA_SOURCE_ID, null).apply { minZoom = IN_AREA_MIN_ZOOM.toFloat(); maxZoom = MIN_ZOOM.toFloat() })
        add(style, badges(LAYER_ID, SOURCE_ID, SOURCE_LAYER).apply { minZoom = MIN_ZOOM.toFloat() })
    }

    private fun badges(id: String, source: String, sourceLayer: String?): SymbolLayer =
        SymbolLayer(id, source).apply {
            sourceLayer?.let(::setSourceLayer)
            setProperties(
                PropertyFactory.visibility(Property.NONE),
                PropertyFactory.iconImage(iconImage()),
                // Every spot is drawn, however close to another: a hidden playground is a missed one.
                PropertyFactory.iconAllowOverlap(true),
                PropertyFactory.iconIgnorePlacement(true),
            )
        }

    /** Adds [layer] under the first of [LAYER_IDS] above it that's already on the map, or on
     *  top — so one re-added by [refresh] goes back to its place. */
    private fun add(style: Style, layer: Layer) {
        if (style.getLayer(layer.id) != null) return
        val above = LAYER_IDS.drop(LAYER_IDS.indexOf(layer.id) + 1).firstOrNull { style.getLayer(it) != null }
        if (above != null) style.addLayerBelow(layer, above) else style.addLayer(layer)
    }

    /** The areas' edge, 1 at [MIN_ZOOM] to 2 at 17 — doubled for capture mode's target. */
    private fun lineWidth(target: Expression?): Expression {
        fun width(base: Float): Any =
            if (target == null) base else Expression.switchCase(target, Expression.literal(base * 2), Expression.literal(base))
        return Expression.interpolate(Expression.linear(), Expression.zoom(), Expression.stop(MIN_ZOOM, width(1f)), Expression.stop(17, width(2f)))
    }

    /** Each badge's image: `spot-captured-{category}` for a captured place, `spot-{category}`
     *  otherwise. */
    private fun iconImage(): Expression = Expression.switchCase(
        Expression.`in`(Expression.toNumber(Expression.get("id")), Expression.literal(captured.keys.map { it.toDouble() }.toTypedArray<Any>())),
        Expression.concat(Expression.literal("spot-captured-"), Expression.get("category")),
        Expression.concat(Expression.literal("spot-"), Expression.get("category")),
    )

    /** Every layer filtered to the wanted categories and shown, or all hidden with none wanted
     *  or a track being edited; the badges drawn captured or not. */
    private fun apply(style: Style) {
        val shown = wanted.isNotEmpty() && !editing
        val visibility = PropertyFactory.visibility(if (shown) Property.VISIBLE else Property.NONE)
        val byCategory = Expression.`in`(Expression.get("category"), Expression.literal(wanted.map { it.wire }.toTypedArray<Any>()))
        val filters = mapOf(
            AREA_FILL_LAYER_ID to byCategory,
            AREA_LINE_LAYER_ID to Expression.all(Expression.not(Expression.toBool(Expression.get("circle"))), byCategory),
            CIRCLE_LINE_LAYER_ID to Expression.all(Expression.toBool(Expression.get("circle")), byCategory),
            IN_AREA_LAYER_ID to byCategory,
            LAYER_ID to byCategory,
        )
        val capturing = target?.let { id -> Expression.eq(Expression.toNumber(Expression.get("id")), Expression.literal(id.toDouble())) }
        style.getLayer(GUIDE_LAYER_ID)?.setProperties(PropertyFactory.visibility(if (shown && target != null) Property.VISIBLE else Property.NONE))
        (style.getLayer(AREA_FILL_LAYER_ID) as? FillLayer)?.setProperties(
            PropertyFactory.fillOpacity(
                if (capturing == null) Expression.literal(AREA_OPACITY)
                else Expression.switchCase(capturing, Expression.literal(TARGET_AREA_OPACITY), Expression.literal(CAPTURING_AREA_OPACITY)),
            ),
        )
        for (id in listOf(AREA_LINE_LAYER_ID, CIRCLE_LINE_LAYER_ID)) {
            (style.getLayer(id) as? LineLayer)?.setProperties(PropertyFactory.lineWidth(lineWidth(capturing)))
        }
        for (id in LAYER_IDS) {
            val layer = style.getLayer(id) ?: continue
            layer.setProperties(visibility)
            when (layer) {
                is FillLayer -> layer.setFilter(filters.getValue(id))
                is LineLayer -> layer.setFilter(filters.getValue(id))
                is SymbolLayer -> {
                    layer.setFilter(filters.getValue(id))
                    layer.setProperties(PropertyFactory.iconImage(iconImage()))
                }
            }
        }
    }

    /** The badges, `spot-{category}` and `spot-captured-{category}` for each of the five, drawn
     *  once per style load: a style reload drops its images with its layers. */
    private fun addImages(style: Style, context: Context) {
        val metrics = context.resources.displayMetrics
        val size = (BADGE_DP * metrics.density).toInt()
        val unit = size / BADGE_DP
        for (category in Category.entries) for (isCaptured in listOf(false, true)) {
            val name = if (isCaptured) "spot-captured-${category.wire}" else "spot-${category.wire}"
            if (style.getImage(name) != null) continue
            val (fill, ring, ink) = if (isCaptured) Triple(ACCENT, ACCENT_DARK, "#ffffff") else Triple("#ffffff", ACCENT, INK)
            val bitmap = createBitmap(size, size).apply { density = metrics.densityDpi }
            val canvas = Canvas(bitmap)
            val paint = Paint(Paint.ANTI_ALIAS_FLAG)
            paint.style = Paint.Style.FILL
            paint.color = fill.toColorInt()
            canvas.drawCircle(size / 2f, size / 2f, 13.5f * unit, paint)
            paint.style = Paint.Style.STROKE
            paint.strokeWidth = 2f * unit
            paint.color = ring.toColorInt()
            canvas.drawCircle(size / 2f, size / 2f, 13.5f * unit, paint)
            // Lucide draws in a 24-unit box; 16 of the badge's 30 units leaves a comfortable margin.
            val icon = AppCompatResources.getDrawable(context, category.icon)!!.mutate()
            icon.setTint(ink.toColorInt())
            icon.setBounds((7 * unit).toInt(), (7 * unit).toInt(), (23 * unit).toInt(), (23 * unit).toInt())
            icon.draw(canvas)
            style.addImage(name, bitmap)
        }
    }
}
