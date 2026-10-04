package dev.holdmytrack.android

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.view.Gravity
import android.view.View
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.browser.customtabs.CustomTabsIntent
import com.google.android.material.checkbox.MaterialCheckBox
import com.google.android.material.datepicker.CalendarConstraints
import com.google.android.material.datepicker.CompositeDateValidator
import com.google.android.material.datepicker.DateValidatorPointBackward
import com.google.android.material.datepicker.DateValidatorPointForward
import com.google.android.material.datepicker.MaterialDatePicker
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.panel.PanelFormat
import dev.holdmytrack.android.recording.RecordingTypes
import dev.holdmytrack.android.timeline.TimelineImport
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/**
 * The Google Maps Timeline import (`apps/android/docs/SPEC.md` FR-3.6, root `docs/SPEC.md`
 * FR-3.10): the web's import window as a screen, without its map preview. Opened from Upload — its
 * guide link, or a `.json` picked there — or by another app handing it a `Timeline.json` to view
 * or share. The state is [TimelineImport]'s; this only draws it.
 */
class TimelineImportActivity : AppCompatActivity() {

    private lateinit var accountNotice: TextView
    private lateinit var intro: View
    private lateinit var file: TextView
    private lateinit var choose: Button
    private lateinit var fields: View
    private lateinit var fromButton: Button
    private lateinit var toButton: Button
    private lateinit var modes: LinearLayout
    private lateinit var skipped: TextView
    private lateinit var selection: TextView
    private lateinit var importButton: Button
    private lateinit var progress: TextView
    private lateinit var error: TextView
    private lateinit var close: Button

    private val listener: () -> Unit = { render() }

    private val picker = registerForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri != null) TimelineImport.open(this, uri)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_timeline_import)

        accountNotice = findViewById(R.id.timeline_account_notice)
        intro = findViewById(R.id.timeline_intro)
        file = findViewById(R.id.timeline_file)
        choose = findViewById(R.id.timeline_choose)
        fields = findViewById(R.id.timeline_fields)
        fromButton = findViewById(R.id.timeline_from)
        toButton = findViewById(R.id.timeline_to)
        modes = findViewById(R.id.timeline_modes)
        skipped = findViewById(R.id.timeline_skipped)
        selection = findViewById(R.id.timeline_selection)
        importButton = findViewById(R.id.timeline_import)
        progress = findViewById(R.id.timeline_progress)
        error = findViewById(R.id.timeline_error)
        close = findViewById(R.id.timeline_close)

        findViewById<Button>(R.id.timeline_guide).setOnClickListener {
            CustomTabsIntent.Builder().build().launchUrl(this, HoldMyTrackApi.webPageUri(GUIDE_PATH))
        }
        // Any type: providers label a .json all sorts of ways, and the reader says when it isn't one.
        choose.setOnClickListener { picker.launch(arrayOf("*/*")) }
        fromButton.setOnClickListener { pickDay(isFrom = true) }
        toButton.setOnClickListener { pickDay(isFrom = false) }
        importButton.setOnClickListener { TimelineImport.send(this) }
        close.setOnClickListener { finish() }

        if (savedInstanceState == null && canImport()) incomingUri(intent)?.let { TimelineImport.open(this, it) }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        if (canImport()) incomingUri(intent)?.let { TimelineImport.open(this, it) }
    }

    override fun onStart() {
        super.onStart()
        TimelineImport.addListener(listener)
        render()
    }

    override fun onStop() {
        TimelineImport.removeListener(listener)
        super.onStop()
    }

    override fun onDestroy() {
        // Leaving the screen ends the import, unless it's still reading or sending, which carry on
        // and are there to come back to.
        if (isFinishing) TimelineImport.reset()
        super.onDestroy()
    }

    /** The server refuses a demo account's sync and an unconfirmed one's, so neither gets this far. */
    private fun canImport() = Session.isSignedIn && Session.emailVerified && !Session.isDemo

    private fun render() {
        if (!canImport()) {
            accountNotice.setText(
                when {
                    !Session.isSignedIn -> R.string.sync_needs_account
                    !Session.emailVerified -> R.string.sync_needs_verified_email
                    else -> R.string.upload_demo
                },
            )
            accountNotice.visibility = View.VISIBLE
            for (view in listOf(intro, file, choose, fields, importButton, progress, error, close)) view.visibility = View.GONE
            return
        }
        accountNotice.visibility = View.GONE

        val state = TimelineImport
        val loaded = state.loaded
        val finished = state.finished

        intro.visibility = if (loaded == null) View.VISIBLE else View.GONE
        file.visibility = if (loaded == null) View.GONE else View.VISIBLE
        loaded?.let { file.text = getString(R.string.timeline_mode_row, it.file, getString(R.string.timeline_span, dayLabel(it.first), dayLabel(it.last))) }

        choose.visibility = if (state.sending || finished) View.GONE else View.VISIBLE
        choose.isEnabled = state.reading == null
        choose.text = state.reading?.let { getString(R.string.timeline_reading, it) }
            ?: getString(if (loaded != null) R.string.timeline_choose_other else R.string.timeline_choose)

        fields.visibility = if (loaded != null && !finished) View.VISIBLE else View.GONE
        importButton.visibility = fields.visibility
        if (loaded != null && !finished) {
            fromButton.text = dayLabel(state.from)
            toButton.text = dayLabel(state.to)
            fromButton.isEnabled = !state.sending
            toButton.isEnabled = !state.sending
            renderModes()
            skipped.visibility = if (loaded.read.skipped > 0) View.VISIBLE else View.GONE
            skipped.text = resources.getQuantityString(R.plurals.timeline_skipped, loaded.read.skipped, loaded.read.skipped)
            val selected = state.selected
            selection.text = if (selected.isEmpty()) {
                getString(R.string.timeline_none_selected)
            } else {
                resources.getQuantityString(
                    R.plurals.timeline_selected,
                    selected.size,
                    selected.size,
                    PanelFormat.totalDistance(resources, selected.sumOf { it.distanceM }),
                )
            }
            importButton.isEnabled = !state.sending && selected.isNotEmpty()
        }

        val sent = state.sent
        progress.visibility = if (sent == null) View.GONE else View.VISIBLE
        if (sent != null) {
            progress.text = if (finished) {
                getString(R.string.timeline_done, sent.total, sent.enqueued, sent.already, sent.rejected)
            } else {
                getString(R.string.timeline_sending, sent.done, sent.total)
            }
        }
        error.visibility = if (state.error == null) View.GONE else View.VISIBLE
        error.text = state.error
        close.visibility = if (finished) View.VISIBLE else View.GONE
    }

    /** One checkbox per mode, most trips first, with its count and distance at the other end. */
    private fun renderModes() {
        val loaded = TimelineImport.loaded ?: return
        modes.removeAllViews()
        for (mode in loaded.modes) {
            val row = LinearLayout(this).apply {
                orientation = LinearLayout.HORIZONTAL
                gravity = Gravity.CENTER_VERTICAL
            }
            val box = MaterialCheckBox(this).apply {
                text = RecordingTypes.format(resources, mode.type)
                isChecked = mode.mode in TimelineImport.modes
                isEnabled = !TimelineImport.sending
                setOnCheckedChangeListener { _, _ -> TimelineImport.toggleMode(mode.mode) }
            }
            val meta = TextView(this).apply {
                text = getString(R.string.timeline_mode_row, PanelFormat.count(resources, mode.count), PanelFormat.totalDistance(resources, mode.distanceM))
                setTextColor(getColor(R.color.hmt_ink_meta))
            }
            row.addView(box, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
            row.addView(meta)
            modes.addView(row)
        }
    }

    /** From or To, on a calendar bounded by the file's first and last day and the other end of the range. */
    private fun pickDay(isFrom: Boolean) {
        val loaded = TimelineImport.loaded ?: return
        val min = if (isFrom) loaded.first else TimelineImport.from
        val max = if (isFrom) TimelineImport.to else loaded.last
        val current = if (isFrom) TimelineImport.from else TimelineImport.to
        val constraints = CalendarConstraints.Builder()
            .setStart(utcMillis(min))
            .setEnd(utcMillis(max))
            .setOpenAt(utcMillis(current))
            .setValidator(
                CompositeDateValidator.allOf(
                    listOf(DateValidatorPointForward.from(utcMillis(min)), DateValidatorPointBackward.before(utcMillis(max))),
                ),
            )
            .build()
        val picker = MaterialDatePicker.Builder.datePicker()
            .setTitleText(if (isFrom) R.string.timeline_from else R.string.timeline_to)
            .setSelection(utcMillis(current))
            .setCalendarConstraints(constraints)
            .build()
        picker.addOnPositiveButtonClickListener { millis ->
            val day = Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate().toString()
            if (isFrom) TimelineImport.setRange(day, TimelineImport.to) else TimelineImport.setRange(TimelineImport.from, day)
        }
        picker.show(supportFragmentManager, "timeline_day")
    }

    /** A YYYY-MM-DD day as the date picker counts days: midnight UTC. */
    private fun utcMillis(day: String): Long = LocalDate.parse(day).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()

    private fun dayLabel(day: String): String =
        runCatching {
            DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM)
                .withLocale(resources.configuration.locales[0])
                .format(LocalDate.parse(day))
        }.getOrDefault(day)

    companion object {
        /** The step-by-step export guide (root `docs/SPEC.md` FR-10.5). */
        const val GUIDE_PATH = "/help/timeline-export"

        /** The file another app handed this screen to view or share, or the Upload screen's. */
        fun incomingUri(intent: Intent?): Uri? = when (intent?.action) {
            Intent.ACTION_SEND -> intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
            else -> intent?.data
        }

        fun open(activity: AppCompatActivity, uri: Uri) {
            activity.startActivity(
                Intent(activity, TimelineImportActivity::class.java)
                    .setData(uri)
                    .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION),
            )
        }
    }
}
