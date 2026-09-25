// AGP 9 has built-in Kotlin support; the separate org.jetbrains.kotlin.android plugin is
// not applied here and applying it is an error (see kotl.in/gradle/agp-built-in-kotlin).
plugins {
    alias(libs.plugins.android.application)
}

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
        versionCode = 1
        versionName = "0.1"

        // The API origin is a build input, not a constant: the same source builds against a
        // dev stack on the host and against a deployed server. See gradle.properties.
        buildConfigField(
            "String",
            "API_BASE_URL",
            "\"${providers.gradleProperty("holdmytrack.apiBaseUrl").get().trimEnd('/')}\"",
        )
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
        release {
            isMinifyEnabled = false
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    sourceSets["main"].java.srcDirs("src/main/kotlin")
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
}
