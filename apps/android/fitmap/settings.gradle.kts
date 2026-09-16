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

// The real Android app (apps/android/docs/ROADMAP.md, Phase 2). Its own build rather than a
// module alongside poc-healthconnect: that PoC is throwaway and gets deleted, this does not.
rootProject.name = "fitmap-android"
include(":app")
