// AGP 9 has built-in Kotlin support; the separate org.jetbrains.kotlin.android plugin is
// not applied here and applying it is an error (see kotl.in/gradle/agp-built-in-kotlin).
plugins {
    alias(libs.plugins.android.application)
}

android {
    namespace = "dev.holdmytrack.poc.healthconnect"
    compileSdk = 37

    defaultConfig {
        applicationId = "dev.holdmytrack.poc.healthconnect"
        // 34 is where Health Connect became part of the platform. The PoC pins it there on
        // purpose: below 34 Health Connect is a separately-installed APK the app has to
        // detect and route around, which is a different question from the one this PoC
        // exists to answer. The real app's minSdk is its own decision (ROADMAP.md, Phase 2).
        minSdk = 34
        targetSdk = 37
        versionCode = 1
        versionName = "0.1"
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
    implementation(libs.health.connect)
    implementation(libs.core.ktx)
    implementation(libs.appcompat)
    implementation(libs.lifecycle.runtime)
}
