package dev.holdmytrack.android.photos

import android.content.ContentResolver
import android.graphics.Bitmap
import android.graphics.ImageDecoder
import android.media.ExifInterface
import android.net.Uri
import android.provider.OpenableColumns
import dev.holdmytrack.android.net.PhotoExif
import dev.holdmytrack.android.net.PhotoImage
import java.io.ByteArrayOutputStream
import java.io.IOException
import kotlin.math.max
import kotlin.math.roundToInt

/** The picked file couldn't be decoded as an image. */
class UnreadablePhotoException : IOException("unreadable image")

/**
 * Getting a picked photo ready for upload (`docs/SPEC.md` FR-16.6, ADR-0024), the web's
 * `apps/web/src/ui/photoPrep.ts`: read what its EXIF says about when and where it was taken
 * ([PhotoExifFields]), then redraw it at most [MAX_SIDE] px on its long side, plus a
 * [THUMB_SIDE] px thumbnail, as WebP. Redrawing re-encodes the pixels alone, so no EXIF field —
 * the position included — reaches the server inside the file; the time and position travel as
 * plain upload fields, and the server keeps only a moment on the track.
 *
 * `ImageDecoder` turns the picture upright by its EXIF orientation and decodes HEIC as well as
 * JPEG and WebP, so a phone's own camera format needs nothing extra. Blocking: call it off the
 * main thread.
 */
object PhotoPrep {

    const val MAX_SIDE = 2048
    const val THUMB_SIDE = 320
    private const val QUALITY = 85

    fun prepare(resolver: ContentResolver, uri: Uri): PreparedPhoto {
        val exif = runCatching { readExif(resolver, uri) }.getOrDefault(PhotoExif())
        val bitmap = try {
            ImageDecoder.decodeBitmap(ImageDecoder.createSource(resolver, uri)) { decoder, info, _ ->
                // Software, so it can be compressed; scaled while decoding, so a 50 MP photo is
                // never held at full size.
                decoder.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
                val scale = minOf(1.0, MAX_SIDE.toDouble() / max(info.size.width, info.size.height))
                if (scale < 1.0) {
                    decoder.setTargetSize(
                        (info.size.width * scale).roundToInt().coerceAtLeast(1),
                        (info.size.height * scale).roundToInt().coerceAtLeast(1),
                    )
                }
            }
        } catch (e: IOException) {
            throw UnreadablePhotoException()
        } catch (e: IllegalArgumentException) {
            throw UnreadablePhotoException()
        }
        try {
            val thumbScale = minOf(1.0, THUMB_SIDE.toDouble() / max(bitmap.width, bitmap.height))
            val thumb = Bitmap.createScaledBitmap(
                bitmap,
                (bitmap.width * thumbScale).roundToInt().coerceAtLeast(1),
                (bitmap.height * thumbScale).roundToInt().coerceAtLeast(1),
                true,
            )
            val prepared = PreparedPhoto(encode(bitmap), encode(thumb), exif)
            if (thumb !== bitmap) thumb.recycle()
            return prepared
        } finally {
            bitmap.recycle()
        }
    }

    private fun encode(bitmap: Bitmap): PhotoImage {
        val out = ByteArrayOutputStream()
        if (!bitmap.compress(Bitmap.CompressFormat.WEBP_LOSSY, QUALITY, out)) throw UnreadablePhotoException()
        return PhotoImage(out.toByteArray(), "image/webp")
    }

    private fun readExif(resolver: ContentResolver, uri: Uri): PhotoExif {
        val exif = resolver.openInputStream(uri)?.use { ExifInterface(it) } ?: return PhotoExif()
        // A float pair: under half a metre of precision, well within the 500 m the server
        // snaps a position by.
        val latLong = FloatArray(2).takeIf { exif.getLatLong(it) }
        return PhotoExifFields.resolve(
            dateTimeOriginal = exif.getAttribute(ExifInterface.TAG_DATETIME_ORIGINAL),
            dateTime = exif.getAttribute(ExifInterface.TAG_DATETIME),
            offsetTimeOriginal = exif.getAttribute(ExifInterface.TAG_OFFSET_TIME_ORIGINAL),
            lat = latLong?.get(0)?.toDouble(),
            lon = latLong?.get(1)?.toDouble(),
            gpsDate = exif.getAttribute(ExifInterface.TAG_GPS_DATESTAMP),
            gpsTime = exif.getAttribute(ExifInterface.TAG_GPS_TIMESTAMP),
        )
    }

    /** The file's own name, as the picker gives it — what a failure names it by. */
    fun displayName(resolver: ContentResolver, uri: Uri): String =
        runCatching {
            resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor ->
                if (cursor.moveToFirst()) cursor.getString(0) else null
            }
        }.getOrNull() ?: uri.lastPathSegment.orEmpty()
}
