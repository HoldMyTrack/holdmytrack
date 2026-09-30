pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositories {
        google()
        mavenCentral()
    }
}

// The Android app (apps/android/docs/ROADMAP.md, Phase 2).
rootProject.name = "holdmytrack-android"
include(":app")
