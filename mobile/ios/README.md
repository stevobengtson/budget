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

## Notes

- Re-run `xcodegen generate` whenever you add/rename source files or edit
  `project.yml`.
- `Info.plist` allows cleartext to localhost (`NSAllowsLocalNetworking`) for dev
  only; production traffic is HTTPS.
- Bundle id: `ca.pigglet.budget`. Deployment target: iOS 16.
