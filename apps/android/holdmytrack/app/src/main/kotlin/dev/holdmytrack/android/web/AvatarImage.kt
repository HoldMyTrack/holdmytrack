package dev.holdmytrack.android.web

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import androidx.core.graphics.scale
import java.io.ByteArrayOutputStream

/**
 * A picked avatar made small before Settings' page uploads it: a phone photo is often past the
 * server's 5 MB limit, or in a format it won't take (HEIC), and an avatar is never shown larger
 * than a few hundred pixels — so it's decoded at a reduced size, scaled to at most [MAX_PX] on
 * its long side and handed over as JPEG. Run off the main thread.
 */
object AvatarImage {

    private const val MAX_PX = 512

    /** The image at [uri] as a small JPEG, or null when it can't be decoded. */
    fun shrink(context: Context, uri: Uri): ByteArray? {
        val resolver = context.contentResolver
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null
        var sample = 1
        while (maxOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= MAX_PX) sample *= 2
        val decoded = resolver.openInputStream(uri)?.use {
            BitmapFactory.decodeStream(it, null, BitmapFactory.Options().apply { inSampleSize = sample })
        } ?: return null
        val scale = MAX_PX.toFloat() / maxOf(decoded.width, decoded.height)
        val sized = if (scale < 1f) decoded.scale((decoded.width * scale).toInt(), (decoded.height * scale).toInt()) else decoded
        return ByteArrayOutputStream().use { out ->
            sized.compress(Bitmap.CompressFormat.JPEG, 90, out)
            out.toByteArray()
        }
    }
}
