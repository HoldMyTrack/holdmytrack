package dev.holdmytrack.android.privacy

import android.content.Intent
import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.browser.customtabs.CustomTabsIntent
import dev.holdmytrack.android.MainActivity
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.PrivateLocation
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.panel.PrivateLocationEditor

/**
 * Privacy, from the You tab: the account's Private locations (`docs/SPEC.md` FR-8.1), each a
 * row with its circle drawn small ([CirclePreviewView]) that opens the editor on the map
 * ([PrivateLocationEditor], through `MainActivity.editPrivateLocation`), and Add a location on
 * the map; then what HoldMyTrack keeps, three facts; then Download your data
 * ([DataExportActivity]) and the privacy policy. The list is read each time the screen shows.
 *
 * The demo account sees its locations but can't change them: no Add, no chevrons, rows that
 * don't open, and no download — its data is deleted within the day.
 */
class PrivacyActivity : AppCompatActivity() {

    private lateinit var note: TextView
    private lateinit var rows: LinearLayout

    private val readOnly get() = Session.isDemo

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (!Session.isSignedIn) {
            finish()
            return
        }
        setContentView(R.layout.activity_privacy)
        note = findViewById(R.id.privacy_note)
        rows = findViewById(R.id.privacy_rows)
        findViewById<View>(R.id.privacy_add).apply {
            visibility = if (readOnly) View.GONE else View.VISIBLE
            setOnClickListener { edit(PrivateLocationEditor.NEW) }
        }
        findViewById<View>(R.id.privacy_reprocess).visibility = if (readOnly) View.GONE else View.VISIBLE
        findViewById<View>(R.id.privacy_download).apply {
            visibility = if (readOnly) View.GONE else View.VISIBLE
            setOnClickListener { startActivity(Intent(this@PrivacyActivity, DataExportActivity::class.java)) }
        }
        findViewById<View>(R.id.privacy_policy).setOnClickListener {
            CustomTabsIntent.Builder().build().launchUrl(this, HoldMyTrackApi.webPageUri("/privacy"))
        }
    }

    override fun onResume() {
        super.onResume()
        if (rows.childCount == 0) showNote(getString(R.string.panel_loading))
        HoldMyTrackApi.privateLocations { result ->
            if (isDestroyed) return@privateLocations
            result.onSuccess(::render).onFailure {
                showNote(it.message?.takeIf { m -> m.isNotBlank() } ?: getString(R.string.map_unreachable), failed = true)
            }
        }
    }

    private fun render(locations: List<PrivateLocation>) {
        rows.removeAllViews()
        rows.visibility = if (locations.isEmpty()) View.GONE else View.VISIBLE
        if (locations.isEmpty()) showNote(getString(R.string.private_none)) else note.visibility = View.GONE
        val inflater = LayoutInflater.from(this)
        for (location in locations) {
            val row = inflater.inflate(R.layout.item_private_location, rows, false)
            row.findViewById<CirclePreviewView>(R.id.private_preview).radiusM = location.radiusM
            row.findViewById<TextView>(R.id.private_title).text = location.name.ifEmpty { getString(R.string.private_unnamed) }
            row.findViewById<TextView>(R.id.private_meta).text =
                getString(R.string.private_row_meta, PrivateLocationEditor.radius(resources, location.radiusM))
            if (readOnly) {
                row.findViewById<View>(R.id.private_chevron).visibility = View.GONE
                row.isClickable = false
                row.background = null
            } else {
                row.setOnClickListener { edit(location.id) }
            }
            rows.addView(row)
        }
    }

    private fun showNote(text: String, failed: Boolean = false) {
        note.text = text
        note.setTextColor(getColor(if (failed) R.color.hmt_danger else R.color.hmt_ink_secondary))
        note.visibility = View.VISIBLE
    }

    /** The map, with the editor on [id] — this screen gives way to it. */
    private fun edit(id: String) {
        MainActivity.editPrivateLocation(this, id)
        finish()
    }
}
