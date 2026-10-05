package dev.holdmytrack.android.settings

import android.content.Context
import androidx.appcompat.app.AppCompatDelegate

/**
 * The app's light/dark theme (Settings' Theme toggle, `docs/SPEC.md` FR-4.12): the phone's own
 * setting by default, or always light or always dark. A per-device choice, like the web's
 * account-menu toggle — kept here in SharedPreferences, not on the account.
 *
 * Applied through AppCompat's night mode, which recreates the open screens: their colors come
 * from `res/values-night/colors.xml` in night mode, and the map picks the matching basemap
 * flavor and fog veil from the same configuration (`MapFragment.styleUrl`, `MapOverlays`).
 */
object AppTheme {

    enum class Choice(val key: String, val nightMode: Int) {
        SYSTEM("system", AppCompatDelegate.MODE_NIGHT_FOLLOW_SYSTEM),
        LIGHT("light", AppCompatDelegate.MODE_NIGHT_NO),
        DARK("dark", AppCompatDelegate.MODE_NIGHT_YES),
    }

    private const val PREFS = "app_theme"
    private const val KEY = "choice"

    fun current(context: Context): Choice {
        val key = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(KEY, null)
        return Choice.entries.firstOrNull { it.key == key } ?: Choice.SYSTEM
    }

    /** At process start (HoldMyTrackApplication), before any screen inflates. */
    fun applySaved(context: Context) {
        AppCompatDelegate.setDefaultNightMode(current(context).nightMode)
    }

    /** Saves [choice] and applies it at once; does nothing when it already is the choice. */
    fun set(context: Context, choice: Choice) {
        if (choice == current(context)) return
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putString(KEY, choice.key).apply()
        AppCompatDelegate.setDefaultNightMode(choice.nightMode)
    }
}
