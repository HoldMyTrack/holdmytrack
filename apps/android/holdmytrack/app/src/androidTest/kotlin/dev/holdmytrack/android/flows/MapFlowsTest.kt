package dev.holdmytrack.android.flows

import android.Manifest
import android.content.Context
import android.content.Intent
import android.location.Location
import android.location.LocationManager
import android.location.provider.ProviderProperties
import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.Configurator
import androidx.test.uiautomator.BySelector
import androidx.test.uiautomator.Direction
import androidx.test.uiautomator.StaleObjectException
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.UiObject2
import androidx.test.uiautomator.Until
import dev.holdmytrack.android.MainActivity
import dev.holdmytrack.android.net.Session
import org.junit.AfterClass
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.BeforeClass
import org.junit.FixMethodOrder
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.runners.MethodSorters
import kotlin.concurrent.thread

/**
 * End-to-end flows over the map's state rules, on an emulator against the dev stack the app was
 * built for (`docs/DEVELOPMENT.md`, "Android verification"): a new date range clearing the
 * selection, a Story opened from the Stories tab and closed by Back, and a recording reaching the
 * Sync sheet. One fresh account ([TestAccount]) serves the class, in method-name order.
 */
@RunWith(AndroidJUnit4::class)
@FixMethodOrder(MethodSorters.NAME_ASCENDING)
class MapFlowsTest {

    @Test
    fun a_newRangeClearsTheCheckedGroupAndHiddenTracks() {
        signIn()
        expandSheet()
        val tiles = atLeast(id("activity_tile_target"), 2)
        tiles[0].click()
        device.waitForIdle()
        device.findObjects(id("activity_tile_target"))[1].click()
        need(id("selection_bar"))
        need(id("panel_visibility")).click()
        assertTrue("two rows hidden", device.wait(Until.findObjects(id("activity_hidden")), TIMEOUT_MS).orEmpty().size == 2)

        need(id("date_range_shift_earlier")).click()
        assertTrue("selection bar gone", device.wait(Until.gone(id("selection_bar")), TIMEOUT_MS))
        assertFalse("no row hidden", device.hasObject(id("activity_hidden")))
        assertFalse("no row checked", device.hasObject(By.descStartsWith("Remove ")))
    }

    @Test
    fun b_theStoriesTabOpensTheNewestStoryAndBackReturnsToTheRange() {
        signIn()
        val members = listOf(account.idsByDay.getValue("2026-10-01"), account.idsByDay.getValue("2026-10-06"))
        account.createStory(STORY, members)
        need(id("nav_stories")).click()
        expandSheet()
        need(By.text(STORY))
        assertTrue("the Story's two rows", device.wait(Until.findObjects(id("activity_text")), TIMEOUT_MS).orEmpty().size == 2)

        device.pressBack()
        assertTrue("Story closed", device.wait(Until.gone(By.text(STORY)), TIMEOUT_MS))
        need(id("date_range_label"))
        expandSheet()
        assertTrue("the range's rows", device.wait(Until.hasObject(id("activity_text")), TIMEOUT_MS))
    }

    @Test
    fun c_aStoppedRecordingReachesTheSyncSheet() {
        signIn()
        val locations = context.getSystemService(LocationManager::class.java)
        locations.addTestProvider(
            LocationManager.GPS_PROVIDER, false, false, false, false, true, true, true,
            ProviderProperties.POWER_USAGE_LOW, ProviderProperties.ACCURACY_FINE,
        )
        locations.setTestProviderEnabled(LocationManager.GPS_PROVIDER, true)
        var walking = true
        // A walk north from Cleveland's Public Square, a fix a second, ~11 m apart.
        val walker = thread {
            var step = 0
            while (walking) {
                val fix = Location(LocationManager.GPS_PROVIDER).apply {
                    latitude = 41.4993 + step * 0.0001
                    longitude = -81.6944
                    accuracy = 5f
                    speed = 1.4f
                    time = System.currentTimeMillis()
                    elapsedRealtimeNanos = SystemClock.elapsedRealtimeNanos()
                }
                runCatching { locations.setTestProviderLocation(LocationManager.GPS_PROVIDER, fix) }
                step++
                Thread.sleep(1_000)
            }
        }
        try {
            need(id("record_button")).click()
            need(id("recording_status"))
            // A recording keeps only past a minute of moving time (`RecordingService.MIN_DURATION_MS`).
            Thread.sleep(RECORDING_MS)
            need(id("recording_status_stop")).click()
            need(By.res("android", "button1")).click()
            // The Save screen fills its fields once the recording loads, replacing the views.
            need(id("recording_save"))
            device.waitForIdle()
            retrying { need(id("recording_name")).text = WALK }
            retrying { need(id("recording_save")).click() }
            assertTrue("the Save screen closed", device.wait(Until.gone(id("recording_save")), TIMEOUT_MS))
        } finally {
            walking = false
            walker.join()
            runCatching { locations.removeTestProvider(LocationManager.GPS_PROVIDER) }
        }
        need(id("nav_sync")).click()
        expandSheet()
        val row = device.wait(Until.findObject(By.textContains(WALK)), SHORT_MS)
            ?: device.findObjects(By.scrollable(true)).firstNotNullOfOrNull { it.scrollUntil(Direction.DOWN, Until.findObject(By.textContains(WALK))) }
        if (row == null) device.dumpWindowHierarchy(java.io.File(context.getExternalFilesDir(null), "sync-sheet.xml"))
        assertNotNull("the recording on the Sync sheet", row)
    }

    private fun need(selector: BySelector): UiObject2 =
        device.wait(Until.findObject(selector), TIMEOUT_MS) ?: run {
            // What was on screen instead, for whoever reads the failure (adb pull it).
            val dir = context.getExternalFilesDir(null)
            device.takeScreenshot(java.io.File(dir, "failure.png"))
            device.dumpWindowHierarchy(java.io.File(dir, "failure.xml"))
            throw AssertionError("not on screen: $selector")
        }

    /** At least [count] of [selector] on screen. */
    private fun atLeast(selector: BySelector, count: Int): List<UiObject2> {
        val deadline = SystemClock.uptimeMillis() + TIMEOUT_MS
        while (true) {
            val found = device.findObjects(selector)
            if (found.size >= count) return found
            if (SystemClock.uptimeMillis() > deadline) throw AssertionError("fewer than $count on screen: $selector")
            Thread.sleep(250)
        }
    }

    /** The Activities sheet open full height, so its rows are on screen. */
    private fun expandSheet() {
        val handle = need(id("panel_handle"))
        if (handle.contentDescription?.startsWith("Expand") == true) handle.click()
        device.waitForIdle()
    }

    /** Runs [action] again if the view it found was replaced meanwhile. */
    private fun retrying(action: () -> Unit) {
        repeat(3) {
            try {
                return action()
            } catch (_: StaleObjectException) {
                device.waitForIdle()
            }
        }
        action()
    }

    /** Opens the app signed in to the test account, on the map, whoever it was left signed in as. */
    private fun signIn() {
        Session.clear()
        context.startActivity(
            Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK),
        )
        need(id("email").enabled(true)).text = account.email
        need(id("password")).text = account.password
        // A tap that lands before the screen settles is lost; tap until the sign-in screen goes.
        for (attempt in 1..3) {
            retrying { device.findObject(id("submit"))?.click() }
            if (device.wait(Until.gone(id("submit")), SIGN_IN_MS)) break
        }
        need(id("date_range_label"))
    }

    private companion object {
        const val PACKAGE = "dev.holdmytrack.android"
        // Generous: an emulator on a busy machine takes seconds to draw a screen.
        const val TIMEOUT_MS = 60_000L
        const val SHORT_MS = 5_000L
        const val SIGN_IN_MS = 15_000L
        const val RECORDING_MS = 70_000L
        const val STORY = "Flows trip"
        const val WALK = "Flows walk"

        val instrumentation get() = InstrumentationRegistry.getInstrumentation()
        val context: Context get() = instrumentation.targetContext
        val device: UiDevice get() = UiDevice.getInstance(instrumentation)
        lateinit var account: TestAccount

        fun id(name: String): BySelector = By.res(PACKAGE, name)

        @BeforeClass
        @JvmStatic
        fun createAccount() {
            // The map animates while recording (the camera follows, the button pulses), so the app
            // never goes idle; waiting for it before each query would time every query out.
            Configurator.getInstance().waitForIdleTimeout = 0
            val automation = instrumentation.uiAutomation
            listOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION, Manifest.permission.POST_NOTIFICATIONS)
                .forEach { automation.grantRuntimePermission(PACKAGE, it) }
            automation.executeShellCommand("appops set $PACKAGE android:mock_location allow").close()
            // A loaded emulator's own apps (the launcher) can stop responding; their dialog would cover the app.
            automation.executeShellCommand("settings put global hide_error_dialogs 1").close()
            account = TestAccount.create()
        }

        @AfterClass
        @JvmStatic
        fun deleteAccount() {
            account.delete()
        }
    }
}
