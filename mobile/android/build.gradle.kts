// Top-level build file — plugin versions declared once here, applied in :app.
plugins {
    id("com.android.application") version "9.1.1" apply false
    // AGP 9 has built-in Kotlin — the kotlin.android plugin is no longer applied.
    id("org.jetbrains.kotlin.plugin.compose") version "2.2.10" apply false
}
