package dev.fitmap.poc.healthconnect

import android.os.Bundle
import android.util.Log
import android.view.ViewGroup.LayoutParams.MATCH_PARENT
import android.view.ViewGroup.LayoutParams.WRAP_CONTENT
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContract
import androidx.appcompat.app.AppCompatActivity
import androidx.health.connect.client.HealthConnectClient
import androidx.health.connect.client.PermissionController
import androidx.health.connect.client.permission.HealthPermission
import androidx.health.connect.client.records.ExerciseRouteResult
import androidx.health.connect.client.records.ExerciseSessionRecord
import androidx.health.connect.client.request.ReadRecordsRequest
import androidx.health.connect.client.time.TimeRangeFilter
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.temporal.ChronoUnit

/**
 * Throwaway diagnostic for apps/android/docs/ROADMAP.md Phase 1. It exists to answer three
 * questions with observed behaviour rather than documentation, and then be deleted:
 *
 *  1. Can READ_EXERCISE_ROUTES be requested programmatically? (Expected: no — the permission
 *     dialog should simply not offer it, and it must be granted in Health Connect settings.)
 *  2. Do foreground route reads succeed where background ones return ConsentRequired, even
 *     with READ_HEALTH_DATA_IN_BACKGROUND granted? (Expected: yes — this is the constraint
 *     that makes Android sync foreground-only.)
 *  3. Which *sources* actually expose route geometry? Samsung Health is documented not to,
 *     but this reports per-package so any other writer that degrades the same way shows up.
 *
 * Everything is mirrored to logcat under TAG so results can be read over adb without
 * squinting at a phone screen.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var output: TextView

    // Deliberately outlives the Activity: the background test has to keep running after the
    // app is no longer foregrounded, which is the entire condition under test.
    private val backgroundScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    private val client: HealthConnectClient? by lazy {
        runCatching { HealthConnectClient.getOrCreate(this) }.getOrNull()
    }

    /**
     * Requested as raw strings rather than typed constants because the point is to ask for
     * exactly what the manifest declares and see what the platform hands back. The routes
     * permission in particular is expected to be silently absent from the result.
     */
    private val requested = setOf(
        HealthPermission.getReadPermission(ExerciseSessionRecord::class),
        "android.permission.health.READ_EXERCISE_ROUTES",
        "android.permission.health.READ_HEALTH_DATA_IN_BACKGROUND",
    )

    private val permissionLauncher =
        registerForActivityResult(
            PermissionController.createRequestPermissionResultContract()
                    as ActivityResultContract<Set<String>, Set<String>>,
        ) { granted ->
            log("--- permission result ---")
            for (p in requested) {
                log("${if (p in granted) "GRANTED" else "DENIED "}  ${p.substringAfterLast('.')}")
            }
            log(
                "\nQ1: READ_EXERCISE_ROUTES programmatically requestable? " +
                    if ("android.permission.health.READ_EXERCISE_ROUTES" in granted) {
                        "APPARENTLY YES — contradicts the documented constraint, re-check by hand"
                    } else {
                        "NO (as documented) — grant it in Health Connect settings, then re-run"
                    },
            )
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        output = TextView(this).apply {
            setPadding(24, 24, 24, 24)
            textSize = 11f
            setTextIsSelectable(true)
        }
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            addView(button("1. Request permissions") { requestPermissions() })
            addView(button("2. Read sessions + routes (foreground)") { readForeground() })
            addView(button("3. Background read in 20s — press Home now") { readAfterDelay() })
            addView(button("Show granted permissions") { showGranted() })
            addView(ScrollView(this@MainActivity).apply { addView(output) })
        }
        setContentView(root, LinearLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))

        reportAvailability()
    }

    private fun button(label: String, onClick: () -> Unit) =
        Button(this).apply {
            text = label
            layoutParams = LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT)
            setOnClickListener { onClick() }
        }

    private fun reportAvailability() {
        val status = HealthConnectClient.getSdkStatus(this)
        log("Health Connect SDK status: " + when (status) {
            HealthConnectClient.SDK_AVAILABLE -> "AVAILABLE"
            HealthConnectClient.SDK_UNAVAILABLE -> "UNAVAILABLE"
            HealthConnectClient.SDK_UNAVAILABLE_PROVIDER_UPDATE_REQUIRED -> "PROVIDER UPDATE REQUIRED"
            else -> "unknown ($status)"
        })
        if (client == null) log("getOrCreate() failed — nothing below will work.")
    }

    private fun requestPermissions() {
        output.text = ""
        permissionLauncher.launch(requested)
    }

    private fun showGranted() {
        val c = client ?: return
        lifecycleScope.launch {
            output.text = ""
            val granted = c.permissionController.getGrantedPermissions()
            log("--- currently granted (${granted.size}) ---")
            granted.sorted().forEach { log("  ${it.substringAfterLast('.')}") }
            if (granted.none { it.endsWith("READ_EXERCISE_ROUTES") }) {
                log("\nREAD_EXERCISE_ROUTES is NOT granted. Grant it by hand:")
                log("Settings > Security & privacy > Health Connect > App permissions >")
                log("FitMap HC PoC > 'Exercise routes' > Allow all")
            }
        }
    }

    private fun readForeground() {
        lifecycleScope.launch {
            output.text = ""
            log("--- foreground read ---")
            runRead(inBackground = false)
        }
    }

    private fun readAfterDelay() {
        log("\nBackground read scheduled for 20s. Press Home NOW and leave the app.")
        backgroundScope.launch {
            delay(20_000)
            Log.i(TAG, "--- background read (app should not be foregrounded) ---")
            runRead(inBackground = true)
        }
    }

    /**
     * One read path used by both tests on purpose: any difference in the results is then
     * attributable to foreground vs background, not to two different queries.
     */
    private suspend fun runRead(inBackground: Boolean) {
        val c = client ?: return
        val label = if (inBackground) "BACKGROUND" else "FOREGROUND"
        // Checked before concluding anything, because without this permission every route
        // comes back ConsentRequired in the foreground too — which makes the foreground and
        // background runs identical and the comparison meaningless. An earlier version of
        // this PoC skipped the check and reported "foreground-only confirmed" off a run that
        // had not established it.
        val routesGranted = runCatching {
            c.permissionController.getGrantedPermissions()
                .any { it.endsWith("READ_EXERCISE_ROUTES") }
        }.getOrDefault(false)
        try {
            val sessions = c.readRecords(
                ReadRecordsRequest(
                    ExerciseSessionRecord::class,
                    timeRangeFilter = TimeRangeFilter.between(
                        Instant.now().minus(365, ChronoUnit.DAYS),
                        Instant.now(),
                    ),
                ),
            ).records

            log("[$label] ${sessions.size} exercise sessions in the last 365 days")
            if (sessions.isEmpty()) {
                log("No sessions — nothing to say about routes. Record one, or sync an app.")
                return
            }

            // Per-source tallies are the answer to Q3: "which writers give us geometry".
            val tally = linkedMapOf<String, IntArray>() // [data, noData, consentRequired]
            for (s in sessions) {
                val pkg = s.metadata.dataOrigin.packageName.ifEmpty { "(unknown)" }
                val slot = tally.getOrPut(pkg) { IntArray(3) }
                when (val r = s.exerciseRouteResult) {
                    is ExerciseRouteResult.Data -> {
                        slot[0]++
                        if (slot[0] == 1) {
                            log("[$label] $pkg first route: ${r.exerciseRoute.route.size} points")
                        }
                    }
                    is ExerciseRouteResult.NoData -> slot[1]++
                    is ExerciseRouteResult.ConsentRequired -> slot[2]++
                    else -> Unit
                }
            }

            log("\n[$label] per source — route / no-route / consent-required")
            for ((pkg, s) in tally) log("  $pkg: ${s[0]} / ${s[1]} / ${s[2]}")

            val consent = tally.values.sumOf { it[2] }
            val withRoute = tally.values.sumOf { it[0] }
            log("[$label] READ_EXERCISE_ROUTES granted: $routesGranted")
            if (!routesGranted) {
                log(
                    "\n[$label] INCONCLUSIVE for Q2. Without READ_EXERCISE_ROUTES every route " +
                        "reads as ConsentRequired regardless of foreground/background, so the two " +
                        "runs cannot be told apart. Grant it in Health Connect settings and re-run " +
                        "both reads.",
                )
            } else if (inBackground) {
                log(
                    "\nQ2: background route read (permission granted) => " + when {
                        consent > 0 && withRoute == 0 ->
                            "ConsentRequired for all $consent — foreground-only confirmed, but only " +
                                "if the foreground run with the same permission returned geometry"
                        withRoute > 0 ->
                            "returned geometry for $withRoute session(s) — CONTRADICTS the documented " +
                                "constraint; verify the app was really backgrounded before trusting this"
                        else -> "no routes either way; needs a source that actually writes routes"
                    },
                )
            }
        } catch (e: Exception) {
            log("[$label] read failed: ${e.javaClass.simpleName}: ${e.message}")
        }
    }

    private fun log(line: String) {
        Log.i(TAG, line)
        runOnUiThread { output.append(line + "\n") }
    }

    private companion object {
        const val TAG = "FitMapPoC"
    }
}
