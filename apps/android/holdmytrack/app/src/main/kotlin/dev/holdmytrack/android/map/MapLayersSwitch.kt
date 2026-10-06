package dev.holdmytrack.android.map

import android.content.Context
import androidx.core.content.edit

/**
 * Show layers, the Layers menu's first switch (`docs/SPEC.md` FR-4.13, [LayersMenu]): whether the picked
 * paths ([MapPaths]) and places ([MapSpots]) are drawn at all. Off hides them and keeps the picks.
 * A per-device choice, on until turned off, kept in SharedPreferences like the picks themselves.
 */
object MapLayersSwitch {

    private const val PREFS = "map_layers"
    private const val KEY = "shown"

    fun isOn(context: Context): Boolean =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(KEY, true)

    fun set(context: Context, on: Boolean) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit { putBoolean(KEY, on) }
    }
}
