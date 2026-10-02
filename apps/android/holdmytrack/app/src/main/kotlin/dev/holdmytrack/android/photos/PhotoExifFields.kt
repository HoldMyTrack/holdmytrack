package dev.holdmytrack.android.photos

import dev.holdmytrack.android.net.PhotoExif
import java.time.Instant
import java.time.LocalDateTime
import java.time.ZoneOffset
import kotlin.math.abs
import kotlin.math.roundToLong

/**
 * What a picked photo's EXIF says about when and where it was taken (`docs/SPEC.md` FR-16.1), from
 * the tag values as `android.media.ExifInterface` reads them — the web's `readExif`
 * (`apps/web/src/ui/photoPrep.ts`) less the byte parsing, which the platform does, for JPEG, HEIC
 * and WebP alike.
 *
 * The capture time is DateTimeOriginal, or the file's DateTime. Its zone comes from
 * OffsetTimeOriginal when the camera wrote one; else from the GPS clock, which is UTC — the
 * camera's wall clock minus the GPS time, rounded to the nearest 15 minutes, is its zone, which
 * keeps the camera's own seconds rather than the fix's, which a phone can leave a minute or two
 * stale; with neither, the time goes as a zoneless wall clock. A zero position or an all-zero
 * date is a camera writing nothing, and reads as absent.
 */
object PhotoExifFields {

    private val DATE_TIME = Regex("""^(\d{4}):(\d{2}):(\d{2}) (\d{2}):(\d{2}):(\d{2})""")
    private val GPS_DATE = Regex("""^(\d{4}):(\d{2}):(\d{2})$""")
    private val OFFSET = Regex("""^([+-])(\d{2}):(\d{2})$""")
    private const val QUARTER_S = 15 * 60L
    private const val MIN_ZONE_S = -12 * 3600L
    private const val MAX_ZONE_S = 14 * 3600L

    fun resolve(
        dateTimeOriginal: String?,
        dateTime: String?,
        offsetTimeOriginal: String?,
        lat: Double?,
        lon: Double?,
        gpsDate: String?,
        gpsTime: String?,
    ): PhotoExif {
        val placed = lat != null && lon != null && lat.isFinite() && lon.isFinite() &&
            abs(lat) <= 90 && abs(lon) <= 180 && (lat != 0.0 || lon != 0.0)
        val local = wallClock(dateTimeOriginal) ?: wallClock(dateTime)
        val gps = gpsInstant(gpsDate, gpsTime)
        val offset = offsetTimeOriginal?.trim()?.let(OFFSET::matchEntire)

        var takenAt: Instant? = null
        var takenLocal: String? = null
        when {
            local != null && offset != null -> {
                val (sign, hours, minutes) = offset.destructured
                val seconds = (if (sign == "-") -1 else 1) * (hours.toInt() * 3600 + minutes.toInt() * 60)
                takenAt = local.toInstant(ZoneOffset.ofTotalSeconds(seconds))
            }
            local != null && gps != null -> {
                val wall = local.toEpochSecond(ZoneOffset.UTC)
                val zone = ((wall - gps.epochSecond) / QUARTER_S.toDouble()).roundToLong() * QUARTER_S
                takenAt = if (zone in MIN_ZONE_S..MAX_ZONE_S) Instant.ofEpochSecond(wall - zone) else gps
            }
            local != null -> takenLocal = local.toString().let { if (it.length == 16) "$it:00" else it }
            gps != null -> takenAt = gps
        }
        return PhotoExif(
            takenAt = takenAt,
            takenLocal = takenLocal,
            lat = lat.takeIf { placed },
            lon = lon.takeIf { placed },
        )
    }

    /** "YYYY:MM:DD HH:MM:SS" as a wall clock, or null for anything else — the all-blank or
     *  all-zero value a camera with no clock writes included. */
    private fun wallClock(value: String?): LocalDateTime? {
        val m = value?.trim()?.let(DATE_TIME::find) ?: return null
        val (y, mo, d, h, mi, s) = m.destructured
        if (y == "0000") return null
        return runCatching { LocalDateTime.of(y.toInt(), mo.toInt(), d.toInt(), h.toInt(), mi.toInt(), s.toInt()) }.getOrNull()
    }

    /** The GPS date ("YYYY:MM:DD") and time — "HH:MM:SS" as the platform gives it, or the raw
     *  rationals ("12/1,34/1,56/1") — as an instant, UTC. */
    private fun gpsInstant(date: String?, time: String?): Instant? {
        val d = date?.trim()?.let(GPS_DATE::matchEntire) ?: return null
        val parts = time?.trim()?.split(':', ',')?.takeIf { it.size >= 3 } ?: return null
        val (h, mi, s) = parts.take(3).map { rational(it) ?: return null }
        val (y, mo, day) = d.destructured
        return runCatching {
            LocalDateTime.of(y.toInt(), mo.toInt(), day.toInt(), h.toInt(), mi.toInt(), s.toInt()).toInstant(ZoneOffset.UTC)
        }.getOrNull()
    }

    private fun rational(text: String): Double? {
        val (num, den) = text.split('/').let { if (it.size == 2) it[0] to it[1] else it[0] to "1" }
        val n = num.trim().toDoubleOrNull() ?: return null
        val q = den.trim().toDoubleOrNull()?.takeIf { it != 0.0 } ?: 1.0
        return n / q
    }
}
