# Building and publishing CurlGo

The desktop version is set in `mygo.json`. Release notes are the matching version section of `CHANGELOG.md`. Build from a clean commit and annotated tag; keep the source revision with the release evidence.

## Windows builds

Windows builds use the same MyGo build as the macOS app. The release machine needs a working NSIS `makensis` on `PATH`; with the macOS Homebrew bottle, also export a UTF-8 locale, because NSIS 3.13 aborts with `std::bad_alloc` when `LANG`/`LC_*` are unset ([NSIS bug #1165](https://sourceforge.net/p/nsis/bugs/1165/); the Homebrew formula test sets the same workaround):

```sh
PATH=/opt/homebrew/bin:$PATH LANG=en_GB.UTF-8 LC_ALL=en_GB.UTF-8 \
  go tool mygo build -platform windows/amd64,windows/arm64 -o build/release-<version>-windows .
```

Each target directory contains:

- `CurlGo.exe` with icon and manifest (the portable executable);
- `curlgo-setup-<version>-<arch>.exe`, the NSIS installer;
- `install.ps1`, a one-line installer for the latest release;
- `curlgo-<version>-windows-<arch>.tar.gz` and `update-windows-<arch>.json`, the signed update archive and manifest.

Publish the installers, the portable ZIPs (`curlgo-<version>-windows-<arch>.zip`, made from the executable plus the license and third-party notices), the update archives/manifests and install.ps1 to the same GitHub release. Windows builds are unsigned: state the SmartScreen warning in release notes and on the website. Combine macOS and Windows assets in the release `SHA256SUMS`.

## Standalone CLI and embedded guide

The standalone CLI and complete Agent guide share the version in `internal/cli/VERSION` and the [CLI changelog](../internal/cli/CHANGELOG.md). The guide and all references in `internal/cli/guide/` are embedded in the binary at build time. `curlgo skill [topic]` reads those resources, never local copies installed by a skills tool. Bump the CLI version when releasing changes to its behavior or full guide; desktop-only changes do not require a CLI version bump.

Build the CLI independently from the desktop app:

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o build/agent-cli/curlgo ./cmd/curlgo
build/agent-cli/curlgo skill
build/agent-cli/curlgo skill workflows
python3 scripts/test_api_skill.py --binary build/agent-cli/curlgo
```

The CLI has no GUI dependency. With no arguments it displays JSON help; the desktop executable still opens its GUI with no arguments. Both share CLI/core code and embed the same guide from their source revision. Replacing the standalone binary upgrades its full guide at the same time, without a separate reference-file installation step. The `skill` header identifies the embedded runtime/guide version, which can differ from the desktop package version.

Use `v<version>` for desktop releases and `cli-v<version>` for independent CLI releases. CLI releases include platform-specific standalone binaries and checksums, plus `LICENSE` and `THIRD_PARTY_NOTICES.txt`. The first release targets macOS, Linux, and Windows on arm64 and amd64; only the local macOS arm64 binary is exercised on this release machine. macOS executables are Developer ID signed and submitted for notarization using the existing `clipnest-notary` keychain profile. Before signing or publishing, authenticate with `xcrun notarytool history --keychain-profile clipnest-notary`; a missing profile name does not establish that the machine has no notarization credentials. For `cli-v0.1.0`, notarization was completed after initial publication on the original, unchanged macOS archives; see the release's `NOTARIZATION.json` for archive hashes and Apple submission results. The initial `BUILD-INFO.json` and archive README describe the pre-notarization packaging state and are superseded by this notarization record. Keep published binaries and checksums unchanged when completing notarization after publication. Keep CLI releases off the repository's latest-release channel: the desktop updater downloads its manifest through `releases/latest`. Do not publish a CLI-only or bootstrap-only release as latest. The standalone CLI has no automatic updater; users replace its executable using a verified release archive.

The separately installed `skills/api-test/` directory contains only a stable bootstrap, installation guidance, and its own [bootstrap changelog](../skills/api-test/CHANGELOG.md). Its `metadata.version` tracks changes to that entry point, not CLI features. Updating it does not install or upgrade a binary. A guide-only update ships with the CLI; users re-read `curlgo skill` after upgrading or switching executables. See [installation and upgrades](agent-skill.md).

## Signing updates

The public update key is committed in `mygo.json`. Its dedicated private key is outside the repository, under `~/Library/Application Support/CurlGo/update-keys/mygo-update.key` on the release machine. Keep an encrypted backup: future versions must use the same key so installed apps can verify them. Do not generate a replacement for routine releases.

On another release machine, provide this same key through `MYGO_UPDATER_PRIVATE_KEY` or place it at the configured private-key path. Apple signing uses the Developer ID identity and existing `clipnest-notary` notarization keychain profile in `mygo.json`. Keep the existing credential item; do not generate a replacement for a profile-name mismatch.

```sh
go test ./...
go vet ./...
go tool mygo build -platform darwin/arm64 -o build/release-0.2.1 .
```

The build produces the DMG, a signed update archive, `update-darwin-arm64.json`, and checksums. Publish all of them to the same GitHub release. The updater reads `https://github.com/echoVic/curlgo/releases/latest/download/update-darwin-arm64.json`; the archive URL points at the immutable version tag.

Before publishing:

```sh
codesign --verify --deep --strict build/release-0.2.1/darwin-arm64/CurlGo.app
xcrun stapler validate build/release-0.2.1/darwin-arm64/CurlGo.app
spctl --assess --type execute build/release-0.2.1/darwin-arm64/CurlGo.app
xcrun stapler validate build/release-0.2.1/darwin-arm64/curlgo-0.2.0-arm64.dmg
hdiutil verify build/release-0.2.1/darwin-arm64/curlgo-0.2.0-arm64.dmg
go run scripts/verify_update.go mygo.json build/release-0.2.1/darwin-arm64/update-darwin-arm64.json build/release-0.2.1/darwin-arm64/curlgo-0.2.0-darwin-arm64.tar.gz
```

Verify the downloaded public assets again after publishing. The signature covers the SHA-256 digest of the update archive; release-note text in the manifest is not signed. The signed `.app` archive contains the stapled app. The DMG itself is stapled; its embedded app is copied before app stapling.

## User controls

Settings shows the version and exposes manual checks, background checks and automatic downloads. Background checks default to every four hours. Automatic downloading and installing defaults off. If enabled, a verified update is prepared for the next launch; the app does not force a restart during an active request. Preferences are kept by the native MyGo updater.

Version 0.1.0 did not include an updater. Those users need to install 0.1.1 manually once. Development builds and installations without write permission cannot update in place; the settings page explains their status. CLI help, dry-run and request execution do not start the updater.
