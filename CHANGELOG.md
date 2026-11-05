# Changelog

## CLI 0.1.0 - 2026-11-06

- Add `curlgo skill [topic]` to read the complete API Test guide and references embedded in the executable.
- Replace the separately installed Agent skill with a stable bootstrap that reads instructions from the actual CLI.
- Add a standalone `./cmd/curlgo` build, with its CLI/guide version tracked independently from desktop packaging.

## 0.1.1 - 2026-11-04

- Add an application settings page with the current version, appearance, a manual update check, and preferences for background checks and automatic downloads.
- Add native update notifications with release notes and install/relaunch actions. Updates are verified with a dedicated Ed25519 public key before installation. Background checks run every four hours; automatic downloads are off by default.
- Introduce the original double-loop CurlGo logo and desktop icon.
- Refresh the product walkthrough with Chinese narration and an original soundtrack.

Users of 0.1.0 must install 0.1.1 from the DMG once: 0.1.0 did not include an updater. Future signed releases can be installed through the new updater. The desktop app does not install agent skills or modify coding-agent configuration.

## 0.1.0 - 2026-11-04

First public release of CurlGo, a local native API client.

- Send HTTP/HTTPS requests, receive SSE events, and exchange WebSocket messages.
- Import curl text, Postman Collection v2.1, Bruno collections, and CurlGo collections; preview imports before saving. Collection scripts are not executed.
- Configure environments, Bearer/Basic/API Key authentication, explicit proxies, private CAs, and multipart uploads.
- Check declarative response assertions and extract values for subsequent requests.
- Keep drafts, favorites, history, and Cookies locally. Store explicitly configured secret variables in an encrypted vault with its master key in the macOS Keychain.
- Run individual HTTP requests through the JSON CLI, with help and a local dry-run mode. Install the optional `api-test` agent skill yourself.
- Distribute a Developer ID signed and notarized macOS Apple Silicon DMG.

The first binary release targets macOS Apple Silicon. It does not include team collaboration, collection script execution, a collection runner, or automatic updates. Ordinary workspace values remain plaintext; the encrypted vault only protects values explicitly saved as secrets.
