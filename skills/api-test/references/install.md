# Locate Or Build The CurlGo CLI

Run `command -v curlgo`. Use a trusted executable consistently. Use `curlgo help` to check for `curlgo skill [topic]` in `help.usage` before running it. An older executable without that advertised command needs an upgrade; updating this bootstrap skill alone does not upgrade it.

Standalone CLI binaries are published at [CLI v0.1.0](https://github.com/echoVic/curlgo/releases/tag/cli-v0.1.0). Select the archive for your OS and CPU architecture, verify it against the release's `SHA256SUMS`, and extract it. Put `curlgo` (`curlgo.exe` on Windows) in a user-managed directory on PATH, or use its absolute path. Use `curlgo help` to confirm skill support, then `curlgo skill` to read the bundled guide. Upgrades replace this executable; updating the bootstrap or desktop application does not replace a separately installed CLI.

If a trusted source checkout is available, confirm that its `go.mod` declares `module github.com/echoVic/curlgo`, and build the standalone CLI from that checkout's root with Go 1.27.3:

```sh
CGO_ENABLED=0 go build -o /tmp/curlgo-api-test ./cmd/curlgo
/tmp/curlgo-api-test help
/tmp/curlgo-api-test skill
```

Use `/tmp/curlgo-api-test` in place of `curlgo` for every subsequent command. Stop on build failure. If the checkout does not contain `cmd/curlgo`, it predates this distribution model; locate a newer trusted checkout. If neither a suitable executable nor source is available, ask for its location. Do not build an unrelated API project as CurlGo.

This builds the version in that checkout, not necessarily the latest release. The CLI and its embedded guide upgrade together when the executable is replaced. This skill does not automatically download binaries, install the desktop app, or alter agent configuration. When multiple executables exist, run `skill` using the exact path you intend to use for requests.
