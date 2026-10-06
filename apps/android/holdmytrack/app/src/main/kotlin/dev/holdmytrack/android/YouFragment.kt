package dev.holdmytrack.android

import android.content.Intent
import android.os.Bundle
import android.view.View
import android.widget.ImageView
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.browser.customtabs.CustomTabsIntent
import androidx.fragment.app.Fragment
import com.google.android.material.button.MaterialButtonToggleGroup
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Profile
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.SettingsOptions
import dev.holdmytrack.android.panel.PanelFormat
import dev.holdmytrack.android.privacy.PrivacyActivity
import dev.holdmytrack.android.profile.ProfileStats
import dev.holdmytrack.android.profile.RecentWeeksView
import dev.holdmytrack.android.recording.RecordingFormat
import dev.holdmytrack.android.settings.AccountDeletion
import dev.holdmytrack.android.settings.AppLanguage
import dev.holdmytrack.android.settings.AppTheme
import dev.holdmytrack.android.settings.ChoicePicker
import dev.holdmytrack.android.settings.SettingsActivity
import dev.holdmytrack.android.ui.LargeText
import java.time.LocalDate
import java.time.ZoneId
import java.util.Locale

/**
 * The bottom bar's You tab: the account — avatar, name, email, and "country · timezone · units"
 * — over its all-time numbers and the last weeks of its activity graph, one card whose halves
 * open Settings and Profile; then Privacy; this phone's Theme ([AppTheme]) and the account's
 * Language; the web's About, Help and Contacts, opened in a browser tab, with Donate where
 * `BuildConfig.DONATE_LINK` allows it, which the Play build doesn't; Sign out and Delete
 * account ([AccountDeletion]); and last the app's version, a line to read rather than an action.
 *
 * Read again each time it shows: Settings, a Private location, a sync can each change what it
 * says. The demo account sees no Delete account, and its Language can't be changed — the server
 * keeps a demo account's settings as they are.
 */
class YouFragment : Fragment(R.layout.fragment_you) {

    private lateinit var avatar: ImageView
    private lateinit var initials: TextView
    private lateinit var name: TextView
    private lateinit var email: TextView
    private lateinit var place: TextView
    private lateinit var weeks: RecentWeeksView
    private lateinit var privacySub: TextView
    private lateinit var languageValue: TextView

    private var profile: Profile? = null
    private var options: SettingsOptions? = null

    /** The fragment's own views, found the way an Activity finds its own. */
    private fun <T : View> findViewById(id: Int): T = requireView().findViewById(id)

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        super.onViewCreated(view, savedInstanceState)
        val context = requireContext()
        avatar = findViewById(R.id.you_avatar)
        initials = findViewById(R.id.you_initials)
        name = findViewById(R.id.you_name)
        email = findViewById(R.id.you_email)
        place = findViewById(R.id.you_place)
        weeks = findViewById(R.id.you_weeks)
        privacySub = findViewById(R.id.you_privacy_sub)
        languageValue = findViewById(R.id.you_language_value)

        findViewById<View>(R.id.you_account).setOnClickListener { SettingsActivity.open(context) }
        findViewById<View>(R.id.you_stats).setOnClickListener {
            startActivity(Intent(context, ProfileActivity::class.java))
        }
        findViewById<View>(R.id.you_privacy).setOnClickListener {
            startActivity(Intent(context, PrivacyActivity::class.java))
        }
        findViewById<View>(R.id.you_language).apply {
            isEnabled = !Session.isDemo
            setOnClickListener { pickLanguage() }
        }
        findViewById<View>(R.id.you_donate).apply {
            visibility = if (BuildConfig.DONATE_LINK) View.VISIBLE else View.GONE
            setOnClickListener { openWebPage("/about#funding") }
        }
        findViewById<View>(R.id.you_about).setOnClickListener { openWebPage("/about") }
        findViewById<View>(R.id.you_help).setOnClickListener { openWebPage("/help") }
        findViewById<View>(R.id.you_contacts).setOnClickListener { openWebPage("/contacts") }
        findViewById<View>(R.id.you_sign_out).setOnClickListener {
            it.isEnabled = false
            HoldMyTrackApi.signOut { SignInActivity.open(context) }
        }
        findViewById<View>(R.id.you_delete).apply {
            visibility = if (Session.isDemo) View.GONE else View.VISIBLE
            setOnClickListener { AccountDeletion.confirm(requireActivity() as AppCompatActivity) }
        }
        findViewById<TextView>(R.id.you_version).text = getString(R.string.menu_version, HoldMyTrackApi.appVersion)
        for (row in listOf(R.id.you_stats_row, R.id.you_theme_inner, R.id.you_language_inner, R.id.you_web_row)) {
            LargeText.stack(findViewById(row))
        }
        bindTheme()
        renderAccount()
    }

    override fun onResume() {
        super.onResume()
        // Back from Settings or Profile is a resume; a hidden tab reads when it shows.
        if (!isHidden) load()
    }

    override fun onHiddenChanged(hidden: Boolean) {
        super.onHiddenChanged(hidden)
        if (!hidden) load()
    }

    /** The account, its numbers and its Private locations, each read afresh. */
    private fun load() {
        HoldMyTrackApi.verifySession { result ->
            result.onSuccess { loaded ->
                profile = loaded
                Session.update(loaded)
                if (view != null) renderAccount()
            }
        }
        val today = LocalDate.now(runCatching { ZoneId.of(Session.timezone) }.getOrDefault(ZoneId.systemDefault()))
        HoldMyTrackApi.activityDays(EPOCH_DAY, "${today.year}-12-31") { result ->
            val days = result.getOrNull() ?: return@activityDays
            if (view == null) return@activityDays
            val stats = ProfileStats.statsOf(days.days)
            setStat(R.id.you_stat_activities, R.string.profile_card_activities, PanelFormat.count(resources, stats.count))
            setStat(R.id.you_stat_distance, R.string.profile_card_distance, PanelFormat.totalDistance(resources, stats.distanceMeters))
            setStat(R.id.you_stat_active_days, R.string.profile_card_active_days, PanelFormat.count(resources, stats.activeDays))
            setStat(R.id.you_stat_streak, R.string.profile_card_longest_streak, PanelFormat.count(resources, stats.longestStreakDays))
            weeks.setLevels(ProfileStats.recentWeeks(days.days, today, RECENT_WEEKS))
        }
        HoldMyTrackApi.privateLocations { result ->
            val locations = result.getOrNull() ?: return@privateLocations
            if (view == null) return@privateLocations
            privacySub.text = if (locations.isEmpty()) {
                getString(R.string.you_privacy_sub)
            } else {
                resources.getQuantityString(R.plurals.you_privacy_count, locations.size, locations.size)
            }
        }
    }

    /** Who is signed in, from the session until the fresh profile is in. */
    private fun renderAccount() {
        val loaded = profile
        val displayName = loaded?.displayName.orEmpty()
        name.text = displayName.ifEmpty { Session.email }
        email.text = Session.email
        email.visibility = if (displayName.isEmpty()) View.GONE else View.VISIBLE
        initials.text = initialsOf(displayName.ifEmpty { Session.email })
        val parts = listOfNotNull(
            Session.country.takeIf { it.isNotEmpty() }?.let { Locale("", it).getDisplayCountry(locale()) },
            Session.timezone.takeIf { it.isNotEmpty() },
            getString(if (RecordingFormat.imperial()) R.string.panel_unit_mi else R.string.panel_unit_km),
        )
        place.text = parts.joinToString(" · ")
        place.visibility = if (Session.country.isEmpty()) View.GONE else View.VISIBLE
        languageValue.text = languageLabel(loaded?.locale.orEmpty())
        val url = loaded?.avatarUrl.orEmpty()
        if (url.isEmpty()) {
            avatar.visibility = View.GONE
            return
        }
        HoldMyTrackApi.avatar(url) { image ->
            image.onSuccess { bitmap ->
                if (view == null) return@onSuccess
                avatar.setImageBitmap(bitmap)
                avatar.visibility = View.VISIBLE
            }
        }
    }

    /** The first letters of the first two words — "Sam Taylor" is "ST", an email its first. */
    private fun initialsOf(text: String): String =
        text.substringBefore('@').split(' ', '.', '_', '-').filter { it.isNotEmpty() }.take(2)
            .joinToString("") { it.take(1) }.uppercase(locale())

    /** The language's own name, as the server's list says it once it's in; "Automatic". */
    private fun languageLabel(tag: String): String {
        if (tag.isEmpty()) return getString(R.string.settings_language_auto)
        options?.languages?.firstOrNull { it.value == tag }?.let { return it.label }
        val language = Locale.forLanguageTag(tag)
        return language.getDisplayName(language).replaceFirstChar { it.titlecase(language) }
    }

    /**
     * Language, from the server's list ([SettingsOptions.languages]), saved at once. The save is
     * Settings' — `PATCH /v1/account/settings` replaces Name, Country and Timezone along with it
     * — so it sends the account's own, as just read. A new language recreates the app's screens
     * ([AppLanguage]), this tab coming back as it was.
     */
    private fun pickLanguage() {
        val current = profile ?: return
        val loaded = options
        if (loaded == null) {
            HoldMyTrackApi.settingsOptions { result ->
                result.onSuccess {
                    options = it
                    if (view != null) pickLanguage()
                }
            }
            return
        }
        val choices = listOf(ChoicePicker.Choice("", getString(R.string.settings_language_auto))) +
            loaded.languages.map { ChoicePicker.Choice(it.value, it.label) }
        ChoicePicker.show(requireContext(), getString(R.string.settings_language), current.locale, choices) { choice ->
            if (choice.value == current.locale) return@show
            val appContext = requireContext().applicationContext
            HoldMyTrackApi.updateSettings(current.displayName, current.country, current.timezone, choice.value) { result ->
                result.onSuccess { saved ->
                    profile = saved
                    Session.update(saved)
                    if (view != null) renderAccount()
                    AppLanguage.apply(appContext, saved.locale, clearOnAutomatic = true)
                }
            }
        }
    }

    /** The Theme toggle: this device's choice, applied at once — AppTheme recreates the screen,
     *  which comes back with the toggle already on the new choice. Not the account's. */
    private fun bindTheme() {
        val buttons = mapOf(
            AppTheme.Choice.SYSTEM to R.id.you_theme_system,
            AppTheme.Choice.LIGHT to R.id.you_theme_light,
            AppTheme.Choice.DARK to R.id.you_theme_dark,
        )
        val group = findViewById<MaterialButtonToggleGroup>(R.id.you_theme)
        group.check(buttons.getValue(AppTheme.current(requireContext())))
        group.addOnButtonCheckedListener { _, id, checked ->
            if (!checked) return@addOnButtonCheckedListener
            buttons.entries.firstOrNull { it.value == id }?.let { AppTheme.set(requireContext().applicationContext, it.key) }
        }
    }

    private fun setStat(id: Int, label: Int, value: String) {
        val stat = findViewById<View>(id)
        stat.findViewById<TextView>(R.id.stat_value).text = value
        stat.findViewById<TextView>(R.id.stat_label).setText(label)
    }

    private fun locale(): Locale = resources.configuration.locales[0]

    private fun openWebPage(path: String) {
        CustomTabsIntent.Builder().build().launchUrl(requireContext(), HoldMyTrackApi.webPageUri(path))
    }

    private companion object {
        /** About four months — as many weeks as fit beside the link on a phone. */
        const val RECENT_WEEKS = 16

        /** The histogram's `from` for "since the beginning", as Profile reads it. */
        const val EPOCH_DAY = "1970-01-01"
    }
}
