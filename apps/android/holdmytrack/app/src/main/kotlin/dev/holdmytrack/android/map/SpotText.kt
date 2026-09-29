package dev.holdmytrack.android.map

import dev.holdmytrack.android.net.Spot
import java.net.URLEncoder
import java.util.Locale

/** The spot popup's text, the web's `SpotPopup.tsx` helpers, kept apart from the view so
 *  they're unit-testable. */
object SpotText {

    /** What Copy address copies: OSM's address, or the coordinates when it has none — every
     *  navigator accepts "lat, lon". */
    fun address(spot: Spot): String =
        spot.address ?: "%.6f, %.6f".format(Locale.ROOT, spot.lat, spot.lon)

    /** The article OSM's `wikipedia` tag names, "lang:Article title", on that language's
     *  Wikipedia — the web's `wikipediaURL`, `encodeURIComponent` included. */
    fun wikipediaUrl(tag: String): String {
        val colon = tag.indexOf(':')
        val title = tag.substring(colon + 1).trim().replace(' ', '_')
        // URLEncoder is form encoding: its "+" for a space can't occur (spaces are underscores
        // by now), and the characters encodeURIComponent leaves alone are put back.
        val encoded = URLEncoder.encode(title, Charsets.UTF_8)
            .replace("%21", "!").replace("%27", "'").replace("%28", "(").replace("%29", ")").replace("%7E", "~")
        return "https://${tag.substring(0, colon)}.wikipedia.org/wiki/$encoded"
    }

    /** OSM's `memorial` type as words: "war_memorial" → "War memorial". OSM's own English
     *  vocabulary, shown as it is in every language, as on the web. */
    fun memorialLabel(memorial: String): String {
        val words = memorial.replace(Regex("[_;]+"), " ").trim()
        return words.replaceFirstChar { it.uppercase(Locale.ROOT) }
    }
}
