# Budget — Android

Kotlin + Jetpack Compose app, built with Gradle.

## Versions

| | Version |
|---|---|
| Android Gradle Plugin | 9.1.1 |
| Gradle (wrapper) | 9.6.1 |
| Kotlin | built into AGP 9 (2.2.10) |
| Compose BOM | 2026.06.01 → Compose 1.11.4 / material3 1.4.0 |
| compileSdk / targetSdk | 37 |
| minSdk | 26 |
| JDK | 17+ |

> **AGP 9 compiles Kotlin itself** — the `org.jetbrains.kotlin.android` plugin is
> deliberately *not* applied (doing so fails with "extension 'kotlin' already
> registered"). Only the Compose compiler plugin is applied on top. See
> [Migrate to built-in Kotlin](https://developer.android.com/build/migrate-to-built-in-kotlin).

## Build & run

The Gradle wrapper is committed, so no `gradle wrapper` bootstrap is needed.
Open `mobile/android/` in **Android Studio** (Quail / 2026.1.2 or newer — it must
support AGP 9). On first sync it downloads Gradle 9.6.1, SDK 37, and build tools,
then pick an emulator and **Run**.

### Connect the emulator to the local server

The app targets `http://localhost:8080`, reached over an **adb reverse tunnel**
(the emulator's own `localhost` forwarded to your Mac). With `task web` running:

```bash
~/Library/Android/sdk/platform-tools/adb reverse tcp:8080 tcp:8080
```

Then tap **Check server** — it should show `HTTP 200 {"status":"ok"}`.

- Re-run the command after an emulator **cold boot** (the tunnel doesn't persist).
- It also works for a **physical device over USB** — no LAN/IP setup needed.
- Why not `10.0.2.2` (the emulator's host alias)? It works only if the **macOS
  firewall** allows the server's incoming connections; the adb tunnel avoids that.
- Cleartext HTTP is enabled via `usesCleartextTraffic` for dev only.

## Notes

- Only `androidx.activity:activity-compose` is pinned by hand; all Compose
  libraries are versioned by the BOM. Android Studio may suggest newer versions —
  safe to accept.
- Command-line builds work too: `./gradlew :app:assembleDebug` (needs
  `ANDROID_HOME` or a `local.properties` pointing at your SDK). Android Studio
  sets this up automatically.
