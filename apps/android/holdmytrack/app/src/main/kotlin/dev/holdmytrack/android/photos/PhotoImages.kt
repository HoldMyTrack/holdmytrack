package dev.holdmytrack.android.photos

import android.graphics.Bitmap
import android.util.LruCache
import dev.holdmytrack.android.net.HoldMyTrackApi

/**
 * Photo thumbnails, fetched once and kept: the map's markers and the Photos tab's rows show the
 * same ones, and a marker is rebuilt on every regrouping. A photo's images never change under
 * its URL (`docs/SPEC.md` FR-16.3), so nothing here ever goes stale. Main thread only.
 */
object PhotoImages {

    /** About 8 MB: a 320 px thumbnail is 400 KB at most, so tens of a trip's at once. */
    private val cache = object : LruCache<String, Bitmap>(8 * 1024 * 1024) {
        override fun sizeOf(key: String, value: Bitmap) = value.allocationByteCount
    }
    private val waiting = HashMap<String, MutableList<(Bitmap?) -> Unit>>()

    /** The thumbnail at [path] (a [dev.holdmytrack.android.net.Photo.thumbUrl]) if it's here. */
    fun cached(path: String): Bitmap? = cache.get(path)

    /** The thumbnail at [path] — at once when it's here, else once fetched; null when it can't be. */
    fun thumb(path: String, onLoaded: (Bitmap?) -> Unit) {
        cache.get(path)?.let {
            onLoaded(it)
            return
        }
        waiting[path]?.let {
            it += onLoaded
            return
        }
        waiting[path] = mutableListOf(onLoaded)
        HoldMyTrackApi.image(path, maxSide = null) { result ->
            val bitmap = result.getOrNull()
            if (bitmap != null) cache.put(path, bitmap)
            waiting.remove(path)?.forEach { it(bitmap) }
        }
    }
}
