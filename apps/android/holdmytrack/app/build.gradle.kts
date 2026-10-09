// AGP 9 has built-in Kotlin support; the separate org.jetbrains.kotlin.android plugin is
// not applied here and applying it is an error (see kotl.in/gradle/agp-built-in-kotlin).
plugins {
    alias(libs.plugins.android.application)
}

/** `git <args>`'s output, trimmed — or null outside a git checkout (a source archive), where
 *  the version falls back to code 1 and SHA "dev". */
fun gitOutput(vararg args: String): String? = runCatching {
    providers.exec {
        commandLine("git", *args)
        isIgnoreExitValue = true
    }.standardOutput.asText.get().trim().takeIf { it.isNotEmpty() }
}.getOrNull()

/** The Gradle property [name]'s origin as a quoted Java string, for a BuildConfig field. */
fun apiBaseUrl(name: String): String = "\"${providers.gradleProperty(name).get().trimEnd('/')}\""

android {
    namespace = "dev.holdmytrack.android"
    compileSdk = 37

    defaultConfig {
        applicationId = "dev.holdmytrack.android"
        // 34 (Android 14) is where Health Connect became part of the platform. Below it,
        // Health Connect is a Play-installed APK the app has to detect, route the user into
        // installing, and then re-check — a whole second provider state machine, on a
        // configuration Phase 1's route findings were never measured against. HoldMyTrack ingests
        // GPS sessions from a watch, so the devices it loses are not the ones it serves.
        minSdk = 34
        // Play's requirement is currently API 36 and rises annually; 37 is the newest
        // platform and matches compileSdk, so no behaviour changes are being opted out of.
        // Re-check this against Play's current requirement at each release, not this comment.
        targetSdk = 37
        // versionCode is the commit count, so it rises with every commit to main and never
        // needs remembering — Play refuses an upload whose code isn't higher than the last.
        // versionName is the release number, bumped by hand when a release means something;
        // GIT_SHA is the exact commit, the same short SHA the server's /healthz reports.
        versionCode = gitOutput("rev-list", "--count", "HEAD")?.toIntOrNull() ?: 1
        versionName = "0.11"
        buildConfigField("String", "GIT_SHA", "\"${gitOutput("rev-parse", "--short", "HEAD") ?: "dev"}\"")
    }

    // The release build is the Play build, signed with the upload key (Play App Signing re-signs
    // it with Google's). The keystore and its passwords stay out of the repository: these four
    // properties live in ~/.gradle/gradle.properties. Without them, bundleRelease stops at
    // validateSigningRelease, naming what's missing; debug builds don't need them.
    signingConfigs {
        create("release") {
            providers.gradleProperty("holdmytrack.uploadStoreFile").orNull?.let { storeFile = file(it) }
            storePassword = providers.gradleProperty("holdmytrack.uploadStorePassword").orNull
            keyAlias = providers.gradleProperty("holdmytrack.uploadKeyAlias").orNull
            keyPassword = providers.gradleProperty("holdmytrack.uploadKeyPassword").orNull
        }
    }

    // The languages the app ships (res/values-ru/, res/xml/locales_config.xml): without this
    // the APK also carries every library's own translations into languages the app itself
    // doesn't have, and a phone set to one of those would get a half-translated screen.
    androidResources {
        localeFilters += listOf("en", "ru")
    }

    buildFeatures {
        buildConfig = true
    }

    buildTypes {
        // AGP's built-in JaCoCo: `createDebugUnitTestCoverageReport` runs the unit tests and
        // writes the XML report CI uploads to Codecov (.github/workflows/ci.yml).
        // The burger menu's Donate (MainActivity.showMenu). The release build is the Play build,
        // and Play's Payments policy doesn't allow pointing users at a payment method outside
        // Play's billing (apps/android/docs/ROADMAP.md Phase 6); the debug build is the APK the
        // website offers (docs/DEPLOY.md), which keeps it.
        // The API origin is a build input, not a constant: the same source builds against a
        // dev stack on the host and against a deployed server. Each build type reads its own
        // property, so a release never picks up the dev stack's address. See gradle.properties.
        debug {
            enableUnitTestCoverage = true
            buildConfigField("boolean", "DONATE_LINK", "true")
            buildConfigField("String", "API_BASE_URL", apiBaseUrl("holdmytrack.apiBaseUrl"))
        }
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.getByName("release")
            buildConfigField("boolean", "DONATE_LINK", "false")
            buildConfigField("String", "API_BASE_URL", apiBaseUrl("holdmytrack.releaseApiBaseUrl"))
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    sourceSets["main"].java.srcDirs("src/main/kotlin")
    sourceSets["test"].java.srcDirs("src/test/kotlin")
}

dependencies {
    implementation(libs.maplibre)
    implementation(libs.appcompat)
    implementation(libs.material)
    implementation(libs.okhttp)
    implementation(libs.health.connect)
    // Coroutines are not a style preference here: every HealthConnectClient read is a suspend
    // function, so there is no callback API to use instead. lifecycle-runtime brings the
    // lifecycleScope the sync run is tied to, which is what keeps it foreground-only.
    implementation(libs.lifecycle.runtime)
    implementation(libs.coroutines.android)
    implementation(libs.credentials)
    implementation(libs.credentials.play.services)
    implementation(libs.google.id)
    implementation(libs.browser)
    implementation(libs.recyclerview)
    implementation(libs.fragment)
    testImplementation(libs.junit)
}
