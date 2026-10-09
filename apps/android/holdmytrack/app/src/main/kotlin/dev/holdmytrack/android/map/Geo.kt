package dev.holdmytrack.android.map

import kotlin.math.asin
import kotlin.math.cos
import kotlin.math.min
import kotlin.math.sin
import kotlin.math.sqrt

/** Distances on the ground, the server's way: haversine on the IUGG mean radius. */
object Geo {

    private const val EARTH_RADIUS_M = 6_371_008.8

    fun haversineM(lat1: Double, lon1: Double, lat2: Double, lon2: Double): Double {
        val rad = Math.PI / 180
        val dLat = (lat2 - lat1) * rad
        val dLon = (lon2 - lon1) * rad
        val h = sin(dLat / 2).let { it * it } + cos(lat1 * rad) * cos(lat2 * rad) * sin(dLon / 2).let { it * it }
        return 2 * EARTH_RADIUS_M * asin(min(1.0, sqrt(h)))
    }

    /** The length of the line through [lats] and [lons], point to point. */
    fun lengthM(lats: DoubleArray, lons: DoubleArray): Double {
        var total = 0.0
        for (i in 1 until lats.size) total += haversineM(lats[i - 1], lons[i - 1], lats[i], lons[i])
        return total
    }
}
