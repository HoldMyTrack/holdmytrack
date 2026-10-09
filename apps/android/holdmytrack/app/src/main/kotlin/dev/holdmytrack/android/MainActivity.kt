package dev.holdmytrack.android

import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.view.View
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity
import androidx.core.graphics.Insets
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.fragment.app.Fragment
import androidx.lifecycle.lifecycleScope
import com.google.android.material.bottomnavigation.BottomNavigationView
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.panel.PanelTab
import dev.holdmytrack.android.recording.RecordingService
import dev.holdmytrack.android.recording.db.RecordedActivityStore
import kotlinx.coroutines.launch

/**
 * The app's main window: the bottom bar — Map, Stories, Record, Sync, You — and the tab above it
 * ([ADR-0033](../../../docs/adr/0033-android-bottom-navigation-single-activity.md)). Map, Stories
 * and Sync are all the map ([MapFragment]): Stories and Sync are its panel's Stories and Sync
 * tabs, Sync's list drawn on the map it covers. You is [YouFragment]. Tabs are shown and hidden
 * rather than replaced, so the map — its camera, its layers, a recording on it — is just as it
 * was on coming back to it. The record button over the bar's middle slot is the map's to drive
 * (`MapFragment.onRecordTap`). Sync carries a badge with the number of recordings waiting to be sent.
 *
 * Never shows the map without a session: a signed-out visitor is handed straight to
 * `SignInActivity`, mirroring web's `AuthGate` (`docs/IMPLEMENTATION.md` §4.13), before any layout
 * or `MapView` exists. A bare basemap with none of the three user layers — all behind
 * `requireAuth` server-side — would be a weak first impression next to the Demo account that
 * screen offers one tap away.
 */
class MainActivity : AppCompatActivity() {

    /** The bottom bar's tabs, Record aside — a button rather than a tab. */
    enum class Tab { MAP, STORIES, SYNC, YOU }

    private lateinit var nav: BottomNavigationView
    private var tab = Tab.MAP

    /** Set while the bar's selection is moved in code, so its listener doesn't act on it. */
    private var selectingInCode = false

    /** Back from any tab but Map goes to Map, and only then leaves the app. */
    private val backToMap = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() = showTab(Tab.MAP)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (!Session.isSignedIn) {
            SignInActivity.open(this)
            finish()
            return
        }
        // Likewise an account whose email isn't confirmed: the server refuses it every tile.
        if (!Session.emailVerified) {
            VerifyEmailActivity.open(this)
            finish()
            return
        }
        setContentView(R.layout.activity_main)
        nav = findViewById(R.id.bottom_nav)
        // The middle slot is where the record button sits: no tab, and nothing for TalkBack to
        // stop on under the button.
        nav.findViewById<View>(R.id.nav_record)?.importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS
        nav.setOnItemSelectedListener { item ->
            if (!selectingInCode) tabOf(item.itemId)?.let(::showTab)
            true
        }
        // The bar pads itself clear of the gesture bar, so the tabs above it get no bottom inset
        // — the map's own chrome adds the one it's given (`MapFragment.insetSystemBars`). At the
        // sides — a landscape phone's navigation bar or its camera cutout — the tabs are padded
        // in here, once for all of them, and see no side inset of their own.
        ViewCompat.setOnApplyWindowInsetsListener(findViewById(R.id.main_content)) { content, insets ->
            val sideTypes = WindowInsetsCompat.Type.navigationBars() or WindowInsetsCompat.Type.displayCutout()
            val sides = insets.getInsets(sideTypes)
            content.setPadding(sides.left, 0, sides.right, 0)
            val navigation = insets.getInsets(WindowInsetsCompat.Type.navigationBars())
            val cutout = insets.getInsets(WindowInsetsCompat.Type.displayCutout())
            WindowInsetsCompat.Builder(insets)
                .setInsets(WindowInsetsCompat.Type.navigationBars(), Insets.of(0, navigation.top, 0, 0))
                .setInsets(WindowInsetsCompat.Type.displayCutout(), Insets.of(0, cutout.top, 0, cutout.bottom))
                .build()
        }
        onBackPressedDispatcher.addCallback(this, backToMap)

        // A recreation (rotation, theme, language) restores the tabs' fragments, which tab was
        // showing and each one's own state.
        if (savedInstanceState == null) {
            showTab(Tab.MAP)
            tabFor(intent)?.let(::showTab)
            editPrivateLocation(intent)
        } else {
            tab = savedInstanceState.getString(STATE_TAB)?.let(Tab::valueOf) ?: Tab.MAP
            markTab(tab)
            // The map's panel comes back on its Activities tab; Stories and Sync put it back on
            // their own once the map's view is up.
            if (tab == Tab.STORIES) nav.post { map()?.showStories() }
            if (tab == Tab.SYNC) nav.post { map()?.showSync() }
        }
    }

    override fun onResume() {
        super.onResume()
        // Back from a recording's Save screen, or anywhere a recording was added or sent.
        if (::nav.isInitialized) {
            refreshSyncBadge()
            refreshStoriesBadge()
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        // The notification's Stop and a View on map are both about the map.
        val forMap = intent.action == RecordingService.ACTION_STOP || intent.hasExtra(EXTRA_VIEW_ACTIVITY)
        if (forMap) showMap()
        tabFor(intent)?.let(::showTab)
        map()?.onNewIntent(intent)
        editPrivateLocation(intent)
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.putString(STATE_TAB, tab.name)
    }

    /**
     * Shows [next]'s fragment and hides the others, adding it the first time. Map, Stories and
     * Sync are the same fragment: moving between them moves its panel between Activities, Stories
     * and Sync, and Map takes it off Private locations too.
     */
    fun showTab(next: Tab) {
        val tag = tagOf(next)
        val fragments = supportFragmentManager
        val transaction = fragments.beginTransaction().setReorderingAllowed(true)
        for (other in TAGS) {
            if (other == tag) continue
            fragments.findFragmentByTag(other)?.takeIf { !it.isHidden }?.let(transaction::hide)
        }
        val target = fragments.findFragmentByTag(tag)
        when {
            target == null -> transaction.add(R.id.main_content, newFragment(tag), tag)
            target.isHidden -> transaction.show(target)
        }
        transaction.commitNow()
        tab = next
        when (next) {
            Tab.STORIES -> map()?.showStories()
            Tab.SYNC -> map()?.showSync()
            Tab.MAP -> if (map()?.panelTab.let { it != null && it != PanelTab.ACTIVITIES }) map()?.showActivities()
            else -> Unit
        }
        markTab(next)
    }

    /** The map, when a start of a recording, a View on map or the notification's Stop needs it
     *  — left on Stories if that's where it is, since that's the map too. */
    fun showMap() {
        if (tab == Tab.SYNC || tab == Tab.YOU) showTab(Tab.MAP)
    }

    /** The Sync tab's badge: the recordings on this phone waiting to be sent — none for the
     *  demo account, which can't send them. */
    fun setSyncWaiting(count: Int) {
        if (count <= 0 || Session.isDemo) {
            nav.removeBadge(R.id.nav_sync)
            return
        }
        nav.getOrCreateBadge(R.id.nav_sync).apply {
            backgroundColor = getColor(R.color.hmt_danger)
            badgeTextColor = getColor(R.color.hmt_surface)
            number = count
            setContentDescriptionQuantityStringsResource(R.plurals.sync_waiting)
        }
    }

    private fun refreshSyncBadge() {
        lifecycleScope.launch { setSyncWaiting(RecordedActivityStore(this@MainActivity).count()) }
    }

    /** The Stories tab's badge: the copies of Stories others sent, waiting to be accepted or
     *  declined (`docs/SPEC.md` FR-14.7) — in the accent, an invitation rather than a chore. */
    fun setStoriesWaiting(count: Int) {
        if (count <= 0 || Session.isDemo) {
            nav.removeBadge(R.id.nav_stories)
            return
        }
        nav.getOrCreateBadge(R.id.nav_stories).apply {
            backgroundColor = getColor(R.color.hmt_accent)
            badgeTextColor = getColor(R.color.hmt_surface)
            number = count
            setContentDescriptionQuantityStringsResource(R.plurals.stories_waiting)
        }
    }

    /** The inbox read on launch and on every return to the app; the Stories tab reads it again
     *  as it opens and after every answer. A failed read leaves the badge as it was. */
    private fun refreshStoriesBadge() {
        if (Session.isDemo) return
        HoldMyTrackApi.storySends { result -> result.onSuccess { setStoriesWaiting(it.sends.size) } }
    }

    /** The bottom bar, and the record button over it — gone while the map's Edit window or
     *  Private location editor has the screen. */
    fun setBottomBarShown(shown: Boolean) {
        findViewById<View>(R.id.bottom_bar).visibility = if (shown) View.VISIBLE else View.GONE
    }

    /** The Privacy screen's Add a location on the map, or a row: the map, the editor on it. */
    private fun editPrivateLocation(intent: Intent?) {
        val id = intent?.getStringExtra(EXTRA_PRIVATE_LOCATION) ?: return
        showTab(Tab.MAP)
        map()?.editPrivateLocation(id)
    }

    /** The map's panel moved between its tabs: the bar marks the one it shows. */
    fun onPanelTabChanged(panelTab: PanelTab) {
        if (tab == Tab.YOU) return
        markTab(
            when (panelTab) {
                PanelTab.STORIES -> Tab.STORIES
                PanelTab.SYNC -> Tab.SYNC
                PanelTab.ACTIVITIES -> Tab.MAP
            },
        )
    }

    private fun markTab(shown: Tab) {
        tab = shown
        selectingInCode = true
        nav.selectedItemId = itemOf(shown)
        selectingInCode = false
        backToMap.isEnabled = shown != Tab.MAP
    }

    private fun map() = supportFragmentManager.findFragmentByTag(TAG_MAP) as? MapFragment

    private fun tagOf(tab: Tab) = when (tab) {
        Tab.MAP, Tab.STORIES, Tab.SYNC -> TAG_MAP
        Tab.YOU -> TAG_YOU
    }

    private fun newFragment(tag: String): Fragment = when (tag) {
        TAG_YOU -> YouFragment()
        else -> MapFragment()
    }

    private fun itemOf(tab: Tab) = when (tab) {
        Tab.MAP -> R.id.nav_map
        Tab.STORIES -> R.id.nav_stories
        Tab.SYNC -> R.id.nav_sync
        Tab.YOU -> R.id.nav_you
    }

    private fun tabOf(itemId: Int) = when (itemId) {
        R.id.nav_map -> Tab.MAP
        R.id.nav_stories -> Tab.STORIES
        R.id.nav_sync -> Tab.SYNC
        R.id.nav_you -> Tab.YOU
        else -> null
    }

    private fun tabFor(intent: Intent?) = intent?.getStringExtra(EXTRA_TAB)?.let { name -> Tab.entries.firstOrNull { it.name == name } }

    companion object {
        private const val TAG_MAP = "map"
        private const val TAG_YOU = "you"
        private val TAGS = listOf(TAG_MAP, TAG_YOU)
        private const val STATE_TAB = "tab"
        private const val EXTRA_TAB = "dev.holdmytrack.android.TAB"
        internal const val EXTRA_VIEW_ACTIVITY = "dev.holdmytrack.android.VIEW_ACTIVITY"
        internal const val EXTRA_VIEW_DAY = "dev.holdmytrack.android.VIEW_DAY"
        private const val EXTRA_PRIVATE_LOCATION = "dev.holdmytrack.android.PRIVATE_LOCATION"

        /** Opens the main window on [tab] — Upload's "See Sync". Clears whatever is over an
         *  existing one. */
        fun openTab(context: Context, tab: Tab) {
            context.startActivity(
                Intent(context, MainActivity::class.java)
                    .putExtra(EXTRA_TAB, tab.name)
                    .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP),
            )
        }

        /** Opens the map with the Private location editor on [id], or on a new one with
         *  `PrivateLocationEditor.NEW` — the Privacy screen's. Clears whatever is over the map. */
        fun editPrivateLocation(context: Context, id: String) {
            context.startActivity(
                Intent(context, MainActivity::class.java)
                    .putExtra(EXTRA_PRIVATE_LOCATION, id)
                    .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP),
            )
        }

        /** Opens the map on [activityId], on [day] (`YYYY-MM-DD`, its local date where it was
         *  recorded) — the sync history's View on map. Clears whatever is over an existing map,
         *  which then selects it. */
        fun viewOnMap(context: Context, activityId: String, day: String) {
            context.startActivity(
                Intent(context, MainActivity::class.java)
                    .putExtra(EXTRA_VIEW_ACTIVITY, activityId)
                    .putExtra(EXTRA_VIEW_DAY, day)
                    .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP),
            )
        }
    }
}
