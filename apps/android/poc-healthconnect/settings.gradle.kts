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

// Throwaway, and deliberately its own build rather than a module of the real app: this exists
// to answer apps/android/docs/ROADMAP.md's Phase 1 questions about what Health Connect will
// actually let us read, and should be deleted once those answers are written down.
rootProject.name = "fitmap-poc-healthconnect"
include(":app")
