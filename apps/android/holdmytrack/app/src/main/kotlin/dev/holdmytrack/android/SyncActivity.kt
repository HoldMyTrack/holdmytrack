package dev.holdmytrack.android

import android.os.Bundle
import androidx.appcompat.app.AppCompatActivity

/**
 * The Sync screen on its own ([SyncFragment]), for Health Connect: the screen it opens as this
 * app's permission rationale, and from "see how this app used your data". It opens even
 * signed out, which the app's own Sync tab (`MainActivity`) never is.
 */
class SyncActivity : AppCompatActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_sync)
    }
}
