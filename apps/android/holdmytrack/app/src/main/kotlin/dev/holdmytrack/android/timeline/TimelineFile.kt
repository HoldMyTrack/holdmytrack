package dev.holdmytrack.android.timeline

import android.util.JsonReader
import android.util.JsonToken
import java.io.InputStream
import java.io.InputStreamReader

/**
 * Reads `Timeline.json` as a stream into [TimelineReader]'s raw entries. Streamed, where the web
 * parses the whole file at once (`JSON.parse`): three months is about 40 MB, and years of history
 * can be hundreds, which as one tree of objects would not fit in an app's memory. Only an
 * activity's times, end points and mode and the path's points are kept; the visits, the home and
 * work labels and the month of raw Wi-Fi and GPS signals are read past.
 *
 * Throws [TimelineFormatException] for a file that isn't an Android Timeline export, naming the
 * iPhone's export and the older Takeout location file when it is one of those, and an
 * [java.io.IOException] for one that isn't JSON at all.
 */
object TimelineFile {

    fun read(input: InputStream): TimelineRead =
        JsonReader(InputStreamReader(input, Charsets.UTF_8)).use { reader ->
            when (reader.peek()) {
                JsonToken.BEGIN_ARRAY -> throw TimelineFormatException(if (looksLikeIos(reader)) TimelineFormat.IOS else null)
                JsonToken.BEGIN_OBJECT -> readExport(reader)
                else -> throw TimelineFormatException(null)
            }
        }

    private fun readExport(reader: JsonReader): TimelineRead {
        val path = mutableListOf<RawPathPoint>()
        val activities = mutableListOf<RawActivity>()
        var semantic = false
        var takeout = false
        reader.beginObject()
        while (reader.hasNext()) {
            when (reader.nextName()) {
                "semanticSegments" -> if (reader.peek() == JsonToken.BEGIN_ARRAY) {
                    semantic = true
                    reader.beginArray()
                    while (reader.hasNext()) readSegment(reader, path, activities)
                    reader.endArray()
                } else {
                    reader.skipValue()
                }
                "timelineObjects", "locations" -> {
                    takeout = true
                    reader.skipValue()
                }
                else -> reader.skipValue()
            }
        }
        reader.endObject()
        if (!semantic) throw TimelineFormatException(if (takeout) TimelineFormat.TAKEOUT else null)
        return TimelineReader.read(path, activities)
    }

    /** One `semanticSegments` entry: a `timelinePath`'s points, an `activity`, or neither (a visit). */
    private fun readSegment(reader: JsonReader, path: MutableList<RawPathPoint>, activities: MutableList<RawActivity>) {
        if (reader.peek() != JsonToken.BEGIN_OBJECT) {
            reader.skipValue()
            return
        }
        var startTime: String? = null
        var endTime: String? = null
        var activity: Triple<String?, String?, String?>? = null
        reader.beginObject()
        while (reader.hasNext()) {
            when (reader.nextName()) {
                "startTime" -> startTime = stringOrNull(reader)
                "endTime" -> endTime = stringOrNull(reader)
                "activity" -> activity = readActivity(reader)
                "timelinePath" -> readPath(reader, path)
                else -> reader.skipValue()
            }
        }
        reader.endObject()
        activity?.let { (from, to, mode) -> activities += RawActivity(startTime, endTime, from, to, mode) }
    }

    /** An activity's `start.latLng`, `end.latLng` and `topCandidate.type`; null when it isn't an object. */
    private fun readActivity(reader: JsonReader): Triple<String?, String?, String?>? {
        if (reader.peek() != JsonToken.BEGIN_OBJECT) {
            reader.skipValue()
            return null
        }
        var from: String? = null
        var to: String? = null
        var mode: String? = null
        reader.beginObject()
        while (reader.hasNext()) {
            when (reader.nextName()) {
                "start" -> from = field(reader, "latLng")
                "end" -> to = field(reader, "latLng")
                "topCandidate" -> mode = field(reader, "type")
                else -> reader.skipValue()
            }
        }
        reader.endObject()
        return Triple(from, to, mode)
    }

    private fun readPath(reader: JsonReader, path: MutableList<RawPathPoint>) {
        if (reader.peek() != JsonToken.BEGIN_ARRAY) {
            reader.skipValue()
            return
        }
        reader.beginArray()
        while (reader.hasNext()) {
            if (reader.peek() != JsonToken.BEGIN_OBJECT) {
                reader.skipValue()
                continue
            }
            var point: String? = null
            var time: String? = null
            reader.beginObject()
            while (reader.hasNext()) {
                when (reader.nextName()) {
                    "point" -> point = stringOrNull(reader)
                    "time" -> time = stringOrNull(reader)
                    else -> reader.skipValue()
                }
            }
            reader.endObject()
            path += RawPathPoint(point, time)
        }
        reader.endArray()
    }

    /** The string [name] of the object the reader is at, or null when it isn't one or lacks it. */
    private fun field(reader: JsonReader, name: String): String? {
        if (reader.peek() != JsonToken.BEGIN_OBJECT) {
            reader.skipValue()
            return null
        }
        var value: String? = null
        reader.beginObject()
        while (reader.hasNext()) {
            if (reader.nextName() == name) value = stringOrNull(reader) else reader.skipValue()
        }
        reader.endObject()
        return value
    }

    private fun stringOrNull(reader: JsonReader): String? =
        if (reader.peek() == JsonToken.STRING) reader.nextString() else {
            reader.skipValue()
            null
        }

    /** The web's iPhone check: an array with an entry that has `startTime` and a `visit`,
     *  `activity` or `timelinePath`. */
    private fun looksLikeIos(reader: JsonReader): Boolean {
        var ios = false
        reader.beginArray()
        while (reader.hasNext()) {
            if (ios || reader.peek() != JsonToken.BEGIN_OBJECT) {
                reader.skipValue()
                continue
            }
            var timed = false
            var kind = false
            reader.beginObject()
            while (reader.hasNext()) {
                when (reader.nextName()) {
                    "startTime" -> timed = true
                    "visit", "activity", "timelinePath" -> kind = true
                }
                reader.skipValue()
            }
            reader.endObject()
            ios = timed && kind
        }
        reader.endArray()
        return ios
    }
}
