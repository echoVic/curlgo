# Building and publishing CurlGo

The desktop version is set in `mygo.json`. Release notes are the matching version section of `CHANGELOG.md`. Build from a clean commit and annotated tag; keep the source revision with the release evidence.

## Signing updates

The public update key is committed in `mygo.json`. Its dedicated private key is outside the repository, under `~/Library/Application Support/CurlGo/update-keys/mygo-update.key` on the release machine. Keep an encrypted backup: future versions must use the same key so installed apps can verify them. Do not generate a replacement for routine releases.

On another release machine, provide this same key through `MYGO_UPDATER_PRIVATE_KEY` or place it at the configured private-key path. Apple signing uses the Developer ID identity and notarization keychain profile in `mygo.json`.

```sh
go test ./...
go vet ./...
go tool mygo build -platform darwin/arm64 -o build/release-0.1.1 .
```

The build produces the DMG, a signed update archive, `update-darwin-arm64.json`, and checksums. Publish all of them to the same GitHub release. The updater reads `https://github.com/echoVic/curlgo/releases/latest/download/update-darwin-arm64.json`; the archive URL points at the immutable version tag.

Before publishing:

```sh
codesign --verify --deep --strict build/release-0.1.1/darwin-arm64/CurlGo.app
xcrun stapler validate build/release-0.1.1/darwin-arm64/CurlGo.app
spctl --assess --type execute build/release-0.1.1/darwin-arm64/CurlGo.app
xcrun stapler validate build/release-0.1.1/darwin-arm64/curlgo-0.1.1-arm64.dmg
hdiutil verify build/release-0.1.1/darwin-arm64/curlgo-0.1.1-arm64.dmg
go run scripts/verify_update.go mygo.json build/release-0.1.1/darwin-arm64/update-darwin-arm64.json build/release-0.1.1/darwin-arm64/curlgo-0.1.1-darwin-arm64.tar.gz
```

Verify the downloaded public assets again after publishing. The signature covers the SHA-256 digest of the update archive; release-note text in the manifest is not signed. The signed `.app` archive contains the stapled app. The DMG itself is stapled; its embedded app is copied before app stapling.

## User controls

Settings shows the version and exposes manual checks, background checks and automatic downloads. Background checks default to every four hours. Automatic downloading and installing defaults off. If enabled, a verified update is prepared for the next launch; the app does not force a restart during an active request. Preferences are kept by the native MyGo updater.

Version 0.1.0 did not include an updater. Those users need to install 0.1.1 manually once. Development builds and installations without write permission cannot update in place; the settings page explains their status. CLI help, dry-run and request execution do not start the updater.
