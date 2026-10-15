package dev.holdmytrack.android

import android.content.ActivityNotFoundException
import android.content.Intent
import android.os.Bundle
import android.widget.Button
import android.widget.TextView
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity
import androidx.core.net.toUri
import dev.holdmytrack.android.net.AppBuild
import dev.holdmytrack.android.net.AppOutdated
import dev.holdmytrack.android.net.HoldMyTrackApi

/**
 * "Update HoldMyTrack" — what covers the app's screens once the server refuses this build as
 * older than its minimum ([AppOutdated]). Back closes the app rather than uncovering screens
 * the server won't answer; opening it again asks the server again, so the screen comes back for
 * as long as the build is refused. Nothing on the phone is touched: recordings and the Sync
 * tab's list wait for the updated app.
 */
class UpdateAppActivity : AppCompatActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_update_app)
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() = finishAffinity()
        })
        findViewById<TextView>(R.id.update_version).text = getString(R.string.menu_version, HoldMyTrackApi.appVersion)
        val installer = runCatching { packageManager.getInstallSourceInfo(packageName).installingPackageName }.getOrNull()
        val link = AppBuild.updateLink(installer, BuildConfig.API_BASE_URL, packageName)
        findViewById<Button>(R.id.update_open).setOnClickListener {
            try {
                startActivity(Intent(Intent.ACTION_VIEW, link.toUri()))
            } catch (_: ActivityNotFoundException) {
                // No store or browser at all: nothing to open it in.
            }
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        if (isFinishing) AppOutdated.dismissed()
    }
}
