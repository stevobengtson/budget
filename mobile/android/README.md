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
| JDK | 17 or 21 (26+ breaks release builds — see [Release to Play](#release-to-play)) |

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

## Release to Play

Play requires a **signed** Android App Bundle (`.aab`). `applicationId` is
`ca.pigglet.budget` and is **permanent once published** — it can't be changed
later.

> **JDK gotcha:** release builds run AGP's `JdkImageTransform` (jlink), which
> **fails on JDK 26+**. Build with JDK 17 or 21. Android Studio's bundled JBR 21
> works — either launch Gradle with it explicitly (shown below) or set
> `org.gradle.java.home` in `~/.gradle/gradle.properties`. Debug builds don't hit
> this, so the failure only shows up at release time.

**1. One-time: create the upload key.** Run yourself so the password stays with
you; back up the `.jks` + password (a lost key with no Play App Signing enrolled
means you can't ship updates):

```bash
keytool -genkeypair -v \
  -keystore mobile/android/upload-keystore.jks \
  -alias upload -keyalg RSA -keysize 2048 -validity 10000 \
  -dname "CN=Pigglet, O=Plainly Software, C=CA"

cp mobile/android/keystore.properties.example mobile/android/keystore.properties
# edit keystore.properties: storePassword / keyPassword = what you chose, keyAlias=upload
```

`keystore.properties`, `*.jks`, and `*.keystore` are gitignored — never commit
them. `build.gradle.kts` applies the release `signingConfig` only when
`keystore.properties` is present; without it, release builds are left unsigned.

**2. Bump the version** in `app/build.gradle.kts` (`versionCode` must increase for
every Play upload; `versionName` is the user-facing string).

**3. Build the signed bundle** (from `mobile/android`):

```bash
JBR="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
./gradlew clean bundleRelease -Dorg.gradle.java.home="$JBR"
# -> app/build/outputs/bundle/release/app-release.aab
```

**4. Verify** it's signed and carries the expected version before uploading:

```bash
"$JBR/bin/jarsigner" -verify -certs \
  app/build/outputs/bundle/release/app-release.aab | head -3
# "jar verified." (an unsigned bundle prints "jar is unsigned")
```

**5. Upload** `app-release.aab` in the [Play Console](https://play.google.com/console)
→ your app → Testing (internal/closed) or Production → *Create release*. On the
first upload, accept **Play App Signing** — the key above becomes your *upload*
key.

## Notes

- Only `androidx.activity:activity-compose` is pinned by hand; all Compose
  libraries are versioned by the BOM. Android Studio may suggest newer versions —
  safe to accept.
- Command-line builds work too: `./gradlew :app:assembleDebug` (needs
  `ANDROID_HOME` or a `local.properties` pointing at your SDK). Android Studio
  sets this up automatically.
