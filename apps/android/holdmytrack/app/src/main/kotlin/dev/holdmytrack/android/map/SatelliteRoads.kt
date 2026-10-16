package dev.holdmytrack.android.map

/**
 * Whether the basemap's roads come off the map: over satellite imagery in Fog and Heatmap, the
 * web's `roadsHiddenOverImagery` (apps/web/src/map/satelliteRoads.ts). There the picture is the
 * ground and what the user covered of it; Normal keeps the roads, see-through, as the way through
 * the imagery.
 */
fun roadsHiddenOverImagery(satellite: Boolean, mode: MapMode): Boolean = satellite && mode != MapMode.NORMAL
