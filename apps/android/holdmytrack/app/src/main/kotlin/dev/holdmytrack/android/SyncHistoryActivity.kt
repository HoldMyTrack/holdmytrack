package dev.holdmytrack.android

import android.os.Bundle
import androidx.appcompat.app.AppCompatActivity
import dev.holdmytrack.android.sync.ImportHistory

/**
 * The whole sync history, from the Sync tab's See all: every import a page at a time
 * ([ImportHistory]) — the web's `/sync` list. Read while it shows, and kept
 * current while anything is still processing.
 */
class SyncHistoryActivity : AppCompatActivity() {

    private lateinit var history: ImportHistory

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_sync_history)
        history = ImportHistory(findViewById(R.id.sync_history_root), preview = false, onViewOnMap = { activityId, day ->
            MainActivity.viewOnMap(this, activityId, day)
            finish()
        })
    }

    override fun onStart() {
        super.onStart()
        history.start()
    }

    override fun onStop() {
        history.stop()
        super.onStop()
    }
}
