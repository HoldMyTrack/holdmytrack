package dev.holdmytrack.android.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AppBuildTest {

    private val api = "https://holdmytrack.com"

    @Test
    fun `a 426 from the API's own v1 is the server turning this build away`() {
        assertTrue(AppBuild.isRefusal("$api/v1/auth/me", api, 426))
    }

    @Test
    fun `anything else is not`() {
        assertFalse(AppBuild.isRefusal("$api/v1/auth/me", api, 401))
        assertFalse(AppBuild.isRefusal("$api/tiles/v1/fog-vector/3/4/5.pbf", api, 426))
        assertFalse(AppBuild.isRefusal("https://api.maptiler.com/v1/tiles", api, 426))
    }

    @Test
    fun `a Play install updates from its listing, any other from the deployment's APK`() {
        assertEquals(
            "https://play.google.com/store/apps/details?id=dev.holdmytrack.android",
            AppBuild.updateLink("com.android.vending", api, "dev.holdmytrack.android"),
        )
        assertEquals("$api/download/holdmytrack.apk", AppBuild.updateLink("com.google.android.packageinstaller", api, "dev.holdmytrack.android"))
        assertEquals("$api/download/holdmytrack.apk", AppBuild.updateLink(null, api, "dev.holdmytrack.android"))
    }

    @Test
    fun `the User-Agent carries the versionCode the server compares`() {
        assertEquals("HoldMyTrack-Android/0.14 (62da50b; 812)", AppBuild.userAgent("0.14", "62da50b", 812))
    }
}
