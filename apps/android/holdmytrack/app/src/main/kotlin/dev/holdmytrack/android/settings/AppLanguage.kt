package dev.holdmytrack.android.settings

import android.app.LocaleManager
import android.content.Context
import android.os.LocaleList

/**
 * The account's Language (Settings, `docs/SPEC.md` FR-1.7) as the app's language, through the
 * platform's per-app language (`LocaleManager`, minSdk 34) — the same setting Android's own
 * "App languages" screen writes, so the two stay one choice rather than two that fight.
 *
 * "Automatic" is the one case that can't be told apart from a choice made in Android's
 * settings: an account that has never picked a language also reads as automatic. So a sign-in or
 * a session check only ever *applies* an account's explicit language ([followAccount]), and
 * only a save from Settings, where the user just chose Automatic, clears a per-app language
 * ([apply] with `clearOnAutomatic`).
 */
object AppLanguage {

    /** A sign-in or `GET /v1/auth/me`: take the account's language if it has chosen one. */
    fun followAccount(context: Context, locale: String) = apply(context, locale, clearOnAutomatic = false)

    /**
     * Sets the app's language to [locale] (a language tag, or empty for automatic). Does
     * nothing when it already is — setting it recreates every open screen.
     */
    fun apply(context: Context, locale: String, clearOnAutomatic: Boolean) {
        if (locale.isEmpty() && !clearOnAutomatic) return
        val manager = context.getSystemService(LocaleManager::class.java) ?: return
        val wanted = if (locale.isEmpty()) LocaleList.getEmptyLocaleList() else LocaleList.forLanguageTags(locale)
        if (manager.applicationLocales.toLanguageTags() == wanted.toLanguageTags()) return
        manager.applicationLocales = wanted
    }
}
