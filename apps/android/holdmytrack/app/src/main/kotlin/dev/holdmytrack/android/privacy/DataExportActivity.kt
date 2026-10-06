package dev.holdmytrack.android.privacy

import android.app.DownloadManager
import android.os.Bundle
import android.os.Environment
import android.text.format.Formatter
import android.view.View
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import dev.holdmytrack.android.R
import dev.holdmytrack.android.net.ExportPart
import dev.holdmytrack.android.net.ExportStatus
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/**
 * Download your data (root `docs/SPEC.md` FR-1.12), from the Privacy screen: what the copy
 * holds, the request, where it stands, and each finished archive saved to Downloads by
 * Android's DownloadManager. The Privacy screen offers it to a real account only.
 */
class DataExportActivity : AppCompatActivity() {

    private lateinit var status: TextView
    private lateinit var parts: LinearLayout
    private lateinit var request: Button
    private lateinit var notice: TextView
    private lateinit var error: TextView

    /** Re-reads the export's state while one is being prepared and the screen is showing. */
    private val poll = Runnable { load() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (!Session.isSignedIn || Session.isDemo) {
            finish()
            return
        }
        setContentView(R.layout.activity_data_export)
        status = findViewById(R.id.export_status)
        parts = findViewById(R.id.export_parts)
        request = findViewById(R.id.export_request)
        notice = findViewById(R.id.export_notice)
        error = findViewById(R.id.export_error)
        request.setOnClickListener { requestExport() }
    }

    override fun onResume() {
        super.onResume()
        load()
    }

    override fun onPause() {
        status.removeCallbacks(poll)
        super.onPause()
    }

    private fun load() {
        status.removeCallbacks(poll)
        HoldMyTrackApi.exportStatus { result -> result.onSuccess { show(it) } }
    }

    private fun requestExport() {
        request.isEnabled = false
        error.visibility = View.GONE
        HoldMyTrackApi.requestExport { result ->
            request.isEnabled = true
            result.onSuccess { show(it) }.onFailure {
                error.text = it.message.orEmpty()
                error.visibility = View.VISIBLE
            }
        }
    }

    /** The screen for [export]: preparing (checked again every [POLL_MS] while it shows), ready
     *  with a Download button per archive, failed, or nothing yet. */
    private fun show(export: ExportStatus) {
        parts.removeAllViews()
        status.visibility = View.VISIBLE
        status.setTextColor(getColor(if (export.state == "failed") R.color.hmt_danger else R.color.hmt_ink))
        request.visibility = View.VISIBLE
        request.setText(if (export.state == "ready") R.string.settings_export_again else R.string.settings_export_request)
        when (export.state) {
            "preparing" -> {
                status.text = getString(R.string.settings_export_preparing, Session.email)
                request.visibility = View.GONE
                status.postDelayed(poll, POLL_MS)
            }
            "ready" -> {
                val until = export.expiresAt?.atZone(ZoneId.systemDefault())
                    ?.format(DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM)).orEmpty()
                status.text = getString(R.string.settings_export_ready, Formatter.formatShortFileSize(this, export.totalSize), until)
                for (part in export.parts) {
                    val button = layoutInflater.inflate(R.layout.item_export_part, parts, false) as Button
                    button.text = getString(R.string.settings_export_download, part.n, export.parts.size, Formatter.formatShortFileSize(this, part.size))
                    button.setOnClickListener { download(part) }
                    parts.addView(button)
                }
            }
            "failed" -> status.setText(R.string.settings_export_failed)
            else -> status.visibility = View.GONE
        }
    }

    /** One archive, to Downloads, by DownloadManager: it shows its own progress notification,
     *  keeps going if this screen closes, and resumes a dropped connection. The session's
     *  token goes as the header, as with every other API request. */
    private fun download(part: ExportPart) {
        val request = DownloadManager.Request(HoldMyTrackApi.exportPartUri(part))
            .addRequestHeader("Authorization", "Bearer ${Session.token}")
            .setTitle(part.name)
            .setMimeType("application/zip")
            .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
            .setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, part.name)
        getSystemService(DownloadManager::class.java).enqueue(request)
        notice.setText(R.string.settings_export_downloading)
        notice.visibility = View.VISIBLE
    }

    private companion object {
        const val POLL_MS = 10_000L
    }
}
