package dev.holdmytrack.android.settings

import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import android.os.Bundle
import android.view.View
import android.widget.Button
import android.widget.TextView
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.graphics.scale
import androidx.core.view.isVisible
import androidx.lifecycle.lifecycleScope
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.button.MaterialButtonToggleGroup
import com.google.android.material.imageview.ShapeableImageView
import com.google.android.material.textfield.MaterialAutoCompleteTextView
import com.google.android.material.textfield.TextInputEditText
import com.google.android.material.textfield.TextInputLayout
import dev.holdmytrack.android.MainActivity
import dev.holdmytrack.android.R
import dev.holdmytrack.android.SignInActivity
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Profile
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.SettingsOptions
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import dev.holdmytrack.android.sync.SyncCursor
import java.io.ByteArrayOutputStream
import java.util.concurrent.Executors
import kotlinx.coroutines.launch

/**
 * The account's Settings (`docs/SPEC.md` FR-1.7), after the web's page: the avatar, which takes
 * effect as soon as it's picked or removed, then Name, Country, Timezone and Language, saved
 * together. The lists come from the server (`GET /v1/account/settings/options`), so every choice
 * is one the save accepts.
 *
 * What a save changes here as well as on the server: the app's language (`AppLanguage`), and
 * its units (`RecordingFormat`, from [Session.country]).
 *
 * Also the first run, as on the web: a real account that has never saved Settings (no Country)
 * is sent here from the map (`MainActivity.syncSession`) with [EXTRA_ONBOARDING] — the welcome
 * title and intro, "Save and continue", and the root of its own task, with no map behind it to
 * go back to. A demo account sees every field disabled, with the web's note.
 *
 * Last, Delete account (root `docs/SPEC.md` FR-1.11), as on the web: a dialog asks for the
 * account's email, the server closes the account, and this device forgets it.
 */
class SettingsActivity : AppCompatActivity() {

    private val onboarding get() = intent.getBooleanExtra(EXTRA_ONBOARDING, false)

    private lateinit var notice: TextView
    private lateinit var error: TextView
    private lateinit var avatar: ShapeableImageView
    private lateinit var avatarChoose: Button
    private lateinit var avatarRemove: Button
    private lateinit var nameField: TextInputEditText
    private lateinit var countryLayout: TextInputLayout
    private lateinit var countryField: TextInputEditText
    private lateinit var timezoneLayout: TextInputLayout
    private lateinit var timezoneField: TextInputEditText
    private lateinit var languageField: MaterialAutoCompleteTextView
    private lateinit var save: Button
    private lateinit var delete: Button

    private var options: SettingsOptions? = null
    private var profile: Profile? = null
    private var country = ""
    private var timezone = ""
    private var locale = ""

    /** The string behind the notice showing, so a recreation can put it back translated. */
    private var noticeRes = 0

    /** The locale values behind `languageField`'s items: "" (automatic), then the server's. */
    private var localeValues: List<String> = listOf("")

    /** Android's photo picker — no storage permission, only the image the user picks. */
    private val pickImage = registerForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        if (uri != null) uploadAvatar(uri)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (!Session.isSignedIn) {
            finish()
            return
        }
        setContentView(R.layout.activity_settings)
        setTitle(if (onboarding) R.string.settings_onboarding_title else R.string.settings_title)

        notice = findViewById(R.id.settings_notice)
        error = findViewById(R.id.settings_error)
        avatar = findViewById(R.id.settings_avatar)
        avatarChoose = findViewById(R.id.settings_avatar_choose)
        avatarRemove = findViewById(R.id.settings_avatar_remove)
        nameField = findViewById(R.id.settings_name)
        countryLayout = findViewById(R.id.settings_country_layout)
        countryField = findViewById(R.id.settings_country)
        timezoneLayout = findViewById(R.id.settings_timezone_layout)
        timezoneField = findViewById(R.id.settings_timezone)
        languageField = findViewById(R.id.settings_language)
        save = findViewById(R.id.settings_save)
        delete = findViewById(R.id.settings_delete)

        findViewById<View>(R.id.settings_intro).visibility = if (onboarding) View.VISIBLE else View.GONE
        findViewById<View>(R.id.settings_demo_notice).visibility = if (Session.isDemo) View.VISIBLE else View.GONE
        save.setText(if (onboarding) R.string.settings_save_continue else R.string.settings_save)

        countryField.setOnClickListener { pickCountry() }
        countryLayout.setEndIconOnClickListener { pickCountry() }
        timezoneField.setOnClickListener { pickTimezone() }
        timezoneLayout.setEndIconOnClickListener { pickTimezone() }
        languageField.setOnItemClickListener { _, _, position, _ -> locale = localeValues[position] }
        avatarChoose.setOnClickListener {
            pickImage.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
        }
        avatarRemove.setOnClickListener { removeAvatar() }
        save.setOnClickListener { onSave() }
        findViewById<View>(R.id.settings_delete_section).visibility = if (Session.isDemo) View.GONE else View.VISIBLE
        delete.setOnClickListener { confirmDelete() }
        bindTheme()

        // A save that changed the language recreates the screen; its "Saved." survives that —
        // kept as the string's id, so it comes back in the language just chosen.
        savedInstanceState?.getInt(STATE_NOTICE)?.takeIf { it != 0 }?.let { showNotice(it) }
        load()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        if (notice.isVisible) outState.putInt(STATE_NOTICE, noticeRes)
    }

    /** The profile and the lists, together; nothing is editable until both are in. A notice
     *  carried over a recreation stays up while they load. */
    private fun load() {
        setEnabled(false, clearMessages = false)
        HoldMyTrackApi.verifySession { result ->
            result.onSuccess { loaded ->
                profile = loaded
                Session.update(loaded)
                bind()
            }.onFailure { show(getString(R.string.settings_load_failed, it.message.orEmpty()), failed = true) }
        }
        HoldMyTrackApi.settingsOptions { result ->
            result.onSuccess { loaded ->
                options = loaded
                bind()
            }.onFailure { show(getString(R.string.settings_load_failed, it.message.orEmpty()), failed = true) }
        }
    }

    /** The Theme toggle: shows this device's choice and applies a new one at once — AppTheme
     *  recreates the screen, which comes back with the toggle already on the new choice. Not
     *  part of [onSave]: the theme belongs to the phone, not the account. */
    private fun bindTheme() {
        val buttons = mapOf(
            AppTheme.Choice.SYSTEM to R.id.settings_theme_system,
            AppTheme.Choice.LIGHT to R.id.settings_theme_light,
            AppTheme.Choice.DARK to R.id.settings_theme_dark,
        )
        val group = findViewById<MaterialButtonToggleGroup>(R.id.settings_theme)
        group.check(buttons.getValue(AppTheme.current(this)))
        group.addOnButtonCheckedListener { _, id, checked ->
            if (!checked) return@addOnButtonCheckedListener
            buttons.entries.firstOrNull { it.value == id }?.let { AppTheme.set(applicationContext, it.key) }
        }
    }

    private fun bind() {
        val profile = profile ?: return
        val options = options ?: return
        nameField.setText(profile.displayName)
        country = profile.country
        timezone = profile.timezone
        locale = profile.locale
        countryField.setText(options.countries.firstOrNull { it.value == country }?.label.orEmpty())
        timezoneField.setText(timezoneLabel(timezone))
        localeValues = listOf("") + options.languages.map { it.value }
        val localeLabels = listOf(getString(R.string.settings_language_auto)) + options.languages.map { it.label }
        languageField.setSimpleItems(localeLabels.toTypedArray())
        languageField.setText(localeLabels[localeValues.indexOf(locale).coerceAtLeast(0)], false)
        showAvatar(profile.avatarUrl)
        setEnabled(!Session.isDemo)
    }

    private fun timezoneLabel(id: String): String {
        val options = options ?: return id
        for (group in options.timezones) {
            group.options.firstOrNull { it.value == id }?.let { return "${it.label} — ${group.label}" }
        }
        return id
    }

    private fun pickCountry() {
        val options = options ?: return
        ChoicePicker.show(
            this,
            getString(R.string.settings_country),
            country,
            options.countries.map { ChoicePicker.Choice(it.value, it.label) },
        ) { choice ->
            country = choice.value
            countryField.setText(choice.label)
        }
    }

    private fun pickTimezone() {
        val options = options ?: return
        val choices = options.timezones.flatMap { group ->
            group.options.map { ChoicePicker.Choice(it.value, it.label, group.label) }
        }
        ChoicePicker.show(this, getString(R.string.settings_timezone), timezone, choices) { choice ->
            timezone = choice.value
            timezoneField.setText(timezoneLabel(choice.value))
        }
    }

    /**
     * One save for Name, Country, Timezone and Language, as the web's form. The server judges
     * it (a missing Country, a demo account) and its wording is what's shown. On success the
     * app takes the account's new language and units at once, and a first run goes on to the
     * map.
     */
    private fun onSave() {
        setEnabled(false)
        HoldMyTrackApi.updateSettings(nameField.text.toString(), country, timezone, locale) { result ->
            result.onSuccess { saved ->
                profile = saved
                Session.update(saved)
                if (onboarding) {
                    startActivity(
                        Intent(this, MainActivity::class.java)
                            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK),
                    )
                    finish()
                } else {
                    setEnabled(true)
                    showNotice(R.string.settings_saved)
                }
                // Last: a changed language recreates this screen (or the map just opened).
                AppLanguage.apply(applicationContext, saved.locale, clearOnAutomatic = true)
            }.onFailure {
                setEnabled(true)
                show(it.message.orEmpty(), failed = true)
            }
        }
    }

    /**
     * Delete account: the email typed into a dialog, as the web's form asks for it. The server
     * says whether it matched; the dialog stays up with its error until it does or is cancelled.
     */
    private fun confirmDelete() {
        val view = layoutInflater.inflate(R.layout.dialog_delete_account, null)
        val layout = view.findViewById<TextInputLayout>(R.id.delete_email_layout)
        val field = view.findViewById<TextInputEditText>(R.id.delete_email)
        view.findViewById<TextView>(R.id.delete_message).text = getString(R.string.settings_delete_confirm, Session.email)
        val dialog = MaterialAlertDialogBuilder(this, R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(R.string.settings_delete_title)
            .setView(view)
            .setNegativeButton(android.R.string.cancel, null)
            .setPositiveButton(R.string.settings_delete_submit, null)
            .show()
        val confirm = dialog.getButton(android.app.AlertDialog.BUTTON_POSITIVE)
        confirm.setOnClickListener {
            layout.error = null
            confirm.isEnabled = false
            HoldMyTrackApi.deleteAccount(field.text.toString()) { result ->
                result.onSuccess {
                    dialog.dismiss()
                    forgetAccount()
                }.onFailure {
                    confirm.isEnabled = true
                    layout.error = it.message.orEmpty()
                }
            }
        }
    }

    /**
     * What this device keeps for the deleted account goes too: its unsynced recordings and its
     * Health Connect watermark — both keyed by the email, which a new account can take again —
     * then the session, and on to the sign-in screen, which says the account was deleted.
     */
    private fun forgetAccount() {
        val email = Session.email
        lifecycleScope.launch {
            RecordedActivityStore(applicationContext).deleteAll()
            SyncCursor(applicationContext, email).reset()
            Session.clear()
            SignInActivity.open(this@SettingsActivity, R.string.account_deleted)
        }
    }

    /**
     * The picked image, made small before it goes: a phone photo is often past the server's 5 MB
     * limit, and an avatar is never shown larger than a few hundred pixels — so it's decoded at
     * a reduced size, scaled to at most [AVATAR_MAX_PX] on its long side and sent as JPEG, off
     * the main thread.
     */
    private fun uploadAvatar(uri: Uri) {
        setEnabled(false)
        io.execute {
            val bytes = runCatching { shrink(uri) }.getOrNull()
            runOnUiThread {
                if (bytes == null) {
                    setEnabled(true)
                    show(getString(R.string.settings_avatar_unreadable), failed = true)
                    return@runOnUiThread
                }
                HoldMyTrackApi.uploadAvatar(bytes, "image/jpeg") { result ->
                    setEnabled(true)
                    result.onSuccess { updated ->
                        profile = updated
                        Session.update(updated)
                        showAvatar(updated.avatarUrl)
                        showNotice(R.string.settings_avatar_updated)
                    }.onFailure { show(it.message.orEmpty(), failed = true) }
                }
            }
        }
    }

    private fun shrink(uri: Uri): ByteArray? {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        contentResolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null
        var sample = 1
        while (maxOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= AVATAR_MAX_PX) sample *= 2
        val decoded = contentResolver.openInputStream(uri)?.use {
            BitmapFactory.decodeStream(it, null, BitmapFactory.Options().apply { inSampleSize = sample })
        } ?: return null
        val scale = AVATAR_MAX_PX.toFloat() / maxOf(decoded.width, decoded.height)
        val sized = if (scale < 1f) {
            decoded.scale((decoded.width * scale).toInt(), (decoded.height * scale).toInt())
        } else {
            decoded
        }
        return ByteArrayOutputStream().use { out ->
            sized.compress(Bitmap.CompressFormat.JPEG, 90, out)
            out.toByteArray()
        }
    }

    private fun removeAvatar() {
        setEnabled(false)
        HoldMyTrackApi.deleteAvatar { result ->
            setEnabled(true)
            result.onSuccess { updated ->
                profile = updated
                Session.update(updated)
                showAvatar(updated.avatarUrl)
                showNotice(R.string.settings_avatar_removed)
            }.onFailure { show(it.message.orEmpty(), failed = true) }
        }
    }

    /** The avatar at [url], or the placeholder person with none; Remove only with one. */
    private fun showAvatar(url: String) {
        avatarRemove.visibility = if (url.isEmpty()) View.GONE else View.VISIBLE
        if (url.isEmpty()) {
            showPlaceholder()
            return
        }
        HoldMyTrackApi.avatar(url) { result ->
            result.onSuccess { bitmap ->
                avatar.setPadding(0, 0, 0, 0)
                avatar.imageTintList = null
                avatar.setImageBitmap(bitmap)
            }.onFailure { showPlaceholder() }
        }
    }

    private fun showPlaceholder() {
        val padding = resources.getDimensionPixelSize(R.dimen.hmt_space_16)
        avatar.setPadding(padding, padding, padding, padding)
        avatar.imageTintList = getColorStateList(R.color.hmt_ink_meta)
        avatar.setImageResource(R.drawable.ic_user)
    }

    /** The fields and buttons, all at once — off while loading or saving, and for good on the
     *  demo account, whose settings the server won't change. */
    private fun setEnabled(enabled: Boolean, clearMessages: Boolean = !enabled) {
        val editable = enabled && !Session.isDemo
        listOf(avatarChoose, avatarRemove, nameField, countryField, timezoneField, languageField, save, delete)
            .forEach { it.isEnabled = editable }
        listOf(countryLayout, timezoneLayout, findViewById<View>(R.id.settings_name_layout), findViewById<View>(R.id.settings_language_layout))
            .forEach { it.isEnabled = editable }
        if (clearMessages) {
            notice.visibility = View.GONE
            error.visibility = View.GONE
        }
    }

    private fun showNotice(res: Int) {
        noticeRes = res
        show(getString(res), failed = false)
    }

    private fun show(text: String, failed: Boolean) {
        val (shown, hidden) = if (failed) error to notice else notice to error
        shown.text = text
        shown.visibility = View.VISIBLE
        hidden.visibility = View.GONE
    }

    companion object {
        const val EXTRA_ONBOARDING = "onboarding"
        private const val STATE_NOTICE = "notice"
        private const val AVATAR_MAX_PX = 512
        private val io = Executors.newSingleThreadExecutor()

        fun open(context: Context) {
            context.startActivity(Intent(context, SettingsActivity::class.java))
        }

        /** The first run: this screen as the root of a new task, in welcome mode. */
        fun openOnboarding(context: Context) {
            context.startActivity(
                Intent(context, SettingsActivity::class.java)
                    .putExtra(EXTRA_ONBOARDING, true)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK),
            )
        }
    }
}
