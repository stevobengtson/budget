# Budget — iOS

SwiftUI app. The Xcode project is **generated** from `project.yml` by
[XcodeGen](https://github.com/yonsson/XcodeGen), so it isn't committed — the spec
is the source of truth.

## One-time setup

```bash
# Point the toolchain at full Xcode (you have both Xcode.app and CLT):
sudo xcode-select -s /Applications/Xcode.app

# Install XcodeGen:
brew install xcodegen
```

## Build & run

```bash
cd mobile/ios
xcodegen generate        # writes Budget.xcodeproj from project.yml
open Budget.xcodeproj    # then Cmd-R on an iPhone Simulator
```

With `task web` running from the repo root, tap **Check server** — it should show
`HTTP 200 {"status":"ok"}`.

## Release to TestFlight

All commands run from `mobile/ios`. Signing is automatic against team
`NTQ6QQEU5A`; `-allowProvisioningUpdates` lets Xcode fetch/create the
distribution profile.

**1. Bump the version** in `project.yml`, then regenerate:

```bash
# CFBundleShortVersionString (user-facing) and CFBundleVersion (build number).
# The build number MUST increase for every upload under a given marketing version.
#   MARKETING_VERSION:       e.g. 1.1.0
#   CURRENT_PROJECT_VERSION: e.g. 2
xcodegen generate
```

**2. Archive** (device build, distribution-signed):

```bash
xcodebuild archive \
  -project Budget.xcodeproj -scheme Budget -configuration Release \
  -destination 'generic/platform=iOS' \
  -archivePath build/Budget.xcarchive \
  -allowProvisioningUpdates
```

**3. Export the `.ipa`** using the committed `ExportOptions.plist`:

```bash
xcodebuild -exportArchive \
  -archivePath build/Budget.xcarchive \
  -exportOptionsPlist ExportOptions.plist \
  -exportPath build/export \
  -allowProvisioningUpdates
# -> build/export/Budget.ipa
```

> **Gotcha:** `ExportOptions.plist` sets `manageAppVersionAndBuildNumber = false`
> on purpose. Left at Xcode's default (`true`), the export contacts App Store
> Connect and silently **increments** the build number, so the `.ipa` no longer
> matches `project.yml` — and the *next* archive then collides. Keep it false so
> the build number is exactly what you set in step 1.

**4. Verify** the `.ipa` carries the version + bundle id you expect before uploading:

```bash
cd build && rm -rf _v && mkdir _v && cd _v && unzip -q ../export/Budget.ipa
/usr/libexec/PlistBuddy \
  -c "Print :CFBundleShortVersionString" \
  -c "Print :CFBundleVersion" \
  -c "Print :CFBundleIdentifier" \
  Payload/Budget.app/Info.plist
cd ../.. && rm -rf build/_v
# expect e.g.  1.1.0 / 2 / ca.pigglet.budget
```

**5. Upload.** Drag `build/export/Budget.ipa` into the **Transporter** app and
Deliver, or from the CLI with an App Store Connect API key (`.p8`):

```bash
xcrun altool --upload-app -f build/export/Budget.ipa -t ios \
  --apiKey <KEY_ID> --apiIssuer <ISSUER_ID>
```

The App Store Connect app record for `ca.pigglet.budget` must exist first, or the
upload is rejected. After upload, the build shows as *Processing* for a few
minutes before it's assignable to TestFlight testers.

## Notes

- Re-run `xcodegen generate` whenever you add/rename source files or edit
  `project.yml`.
- `Info.plist` allows cleartext to localhost (`NSAllowsLocalNetworking`) for dev
  only; production traffic is HTTPS.
- Bundle id: `ca.pigglet.budget`. Deployment target: iOS 16.
