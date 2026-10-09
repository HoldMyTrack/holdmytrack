package dev.holdmytrack.android

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.text.TextUtils
import android.view.View
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.browser.customtabs.CustomTabsIntent
import dev.holdmytrack.android.imports.FileImports
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session

/**
 * Upload: files made somewhere else — a watch's or an app's export, a Google Takeout archive —
 * brought into the account (`apps/android/docs/SPEC.md` FR-3.6).
 * The web's Upload menu as a screen, from the You tab, kept apart from the Sync tab as Upload
 * sits apart from Sync in the web's header: Sync is for what lives on the phone and keeps coming, this is for a file,
 * usually once. The uploads themselves are [FileImports]'s, so they carry on when this screen
 * goes; once one has gone, its activities are followed in Sync's history.
 *
 * Another app's `.gpx`, `.tcx` or `.zip` handed to this app to open or share lands here, and is
 * uploaded as if picked.
 */
class UploadActivity : AppCompatActivity() {

    private lateinit var accountNotice: TextView
    private lateinit var section: View
    private lateinit var choose: Button
    private lateinit var transfers: LinearLayout
    private lateinit var notes: TextView
    private lateinit var errors: TextView
    private lateinit var seeSync: Button

    private val listener: () -> Unit = { render() }

    // Any type: providers label .gpx and .fit all sorts of ways, and FileImports says which it won't send.
    private val picker = registerForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { uris ->
        if (uris.isNotEmpty()) importFiles(uris)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_upload)

        accountNotice = findViewById(R.id.upload_account_notice)
        section = findViewById(R.id.upload_section)
        choose = findViewById(R.id.upload_choose)
        transfers = findViewById(R.id.upload_transfers)
        notes = findViewById(R.id.upload_notes)
        errors = findViewById(R.id.upload_errors)
        seeSync = findViewById(R.id.upload_see_sync)

        choose.setOnClickListener { picker.launch(arrayOf("*/*")) }
        findViewById<Button>(R.id.upload_guide_google_health).setOnClickListener { openPage(GOOGLE_HEALTH_GUIDE_PATH) }
        seeSync.setOnClickListener {
            MainActivity.openTab(this, MainActivity.Tab.SYNC)
            finish()
        }

        if (savedInstanceState == null) importShared(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        importShared(intent)
    }

    override fun onStart() {
        super.onStart()
        FileImports.addListener(listener)
        render()
    }

    override fun onStop() {
        FileImports.removeListener(listener)
        super.onStop()
    }

    /** The server refuses a demo account's uploads (`requireNotDemo`) and an unconfirmed one's. */
    private fun canUpload() = Session.isSignedIn && Session.emailVerified && !Session.isDemo

    /** Files another app handed this one to open or share: uploaded as if picked here, when
     *  this account can (the notice says why not otherwise). */
    private fun importShared(intent: Intent?) {
        val uris = when (intent?.action) {
            Intent.ACTION_VIEW -> listOfNotNull(intent.data)
            Intent.ACTION_SEND -> listOfNotNull(intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java))
            Intent.ACTION_SEND_MULTIPLE -> intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java).orEmpty()
            else -> emptyList()
        }
        if (uris.isNotEmpty() && canUpload()) importFiles(uris)
    }

    /** Picked or shared files to [FileImports]. */
    private fun importFiles(uris: List<Uri>) {
        FileImports.enqueue(this, uris)
    }

    private fun openPage(path: String) {
        CustomTabsIntent.Builder().build().launchUrl(this, HoldMyTrackApi.webPageUri(path))
    }

    /** The account gate, then each file queued or uploading, the uploads' notes and errors, and
     *  the way to Sync once anything has gone. */
    private fun render() {
        if (!canUpload()) {
            accountNotice.setText(
                when {
                    !Session.isSignedIn -> R.string.sync_needs_account
                    !Session.emailVerified -> R.string.sync_needs_verified_email
                    else -> R.string.upload_demo
                },
            )
            accountNotice.visibility = View.VISIBLE
            for (view in listOf(section, choose, transfers, notes, errors, seeSync)) view.visibility = View.GONE
            return
        }
        accountNotice.visibility = View.GONE

        transfers.removeAllViews()
        for (transfer in FileImports.transfers) {
            val row = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
            val name = TextView(this).apply {
                text = transfer.name
                maxLines = 1
                ellipsize = TextUtils.TruncateAt.MIDDLE
            }
            val status = TextView(this).apply {
                setTextColor(getColor(R.color.hmt_ink_meta))
                text = when {
                    transfer.sent < 0 -> getString(R.string.upload_queued)
                    transfer.size > 0 -> getString(R.string.upload_uploading, (transfer.sent * 100 / transfer.size).toInt())
                    else -> getString(R.string.upload_uploading_mb, (transfer.sent shr 20).toInt())
                }
            }
            row.addView(name, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
            row.addView(status)
            transfers.addView(row)
        }
        val (bad, good) = FileImports.notes.partition { it.error }
        notes.text = good.joinToString("\n") { it.text }
        notes.visibility = if (good.isEmpty()) View.GONE else View.VISIBLE
        errors.text = bad.joinToString("\n") { it.text }
        errors.visibility = if (bad.isEmpty()) View.GONE else View.VISIBLE
        seeSync.visibility = if (FileImports.sentCount > 0) View.VISIBLE else View.GONE
    }

    private companion object {
        /** The step-by-step Google Health (Takeout) export guide (root `docs/SPEC.md` FR-10.5). */
        const val GOOGLE_HEALTH_GUIDE_PATH = "/help/google-health-export"
    }
}
