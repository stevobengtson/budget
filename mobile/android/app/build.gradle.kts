import java.util.Properties

plugins {
    id("com.android.application")
    // Kotlin is compiled by AGP 9's built-in support; only the Compose compiler
    // plugin is applied on top.
    id("org.jetbrains.kotlin.plugin.compose")
}

// Release signing is read from an untracked keystore.properties (see
// keystore.properties.example). Absent it, release builds are left unsigned so
// the build still works for anyone without the upload key.
val keystorePropsFile = rootProject.file("keystore.properties")
val keystoreProps = Properties().apply {
    if (keystorePropsFile.exists()) keystorePropsFile.inputStream().use { load(it) }
}

android {
    namespace = "ca.pigglet.budget"
    compileSdk = 37

    defaultConfig {
        applicationId = "ca.pigglet.budget"
        minSdk = 26
        targetSdk = 37
        versionCode = 3
        versionName = "1.1.0"
    }

    signingConfigs {
        if (keystorePropsFile.exists()) {
            create("release") {
                storeFile = rootProject.file(keystoreProps["storeFile"] as String)
                storePassword = keystoreProps["storePassword"] as String
                keyAlias = keystoreProps["keyAlias"] as String
                keyPassword = keystoreProps["keyPassword"] as String
            }
        }
    }

    buildTypes {
        debug {
            // Local dev server (emulator/USB device reach it via `adb reverse`).
            buildConfigField("String", "API_BASE_URL", "\"http://localhost:8080\"")
            manifestPlaceholders["usesCleartextTraffic"] = "true"
        }
        release {
            isMinifyEnabled = false
            buildConfigField("String", "API_BASE_URL", "\"https://pigglet.ca\"")
            manifestPlaceholders["usesCleartextTraffic"] = "false"
            if (keystorePropsFile.exists()) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation("androidx.activity:activity-compose:1.10.1")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.9.4")
    implementation(platform("androidx.compose:compose-bom:2026.06.01"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-core")
    implementation("androidx.compose.ui:ui-tooling-preview")
    debugImplementation("androidx.compose.ui:ui-tooling")
}
