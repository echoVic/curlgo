# CurlGo CLI and runtime guide changelog

The CLI and its embedded guide share `VERSION` in this directory. Desktop packaging uses `mygo.json`; the installed Agent bootstrap has its own metadata.

## Unreleased

- An explicitly empty `--out-dir ""` now fails a normal run with `OUTPUT_ERROR` and exit 3 before any request is sent, as `--dry-run` already did. Omitting the flag still uses a private temporary directory.
- Add the list of curl options that `--curl` imports, ignores, and rejects to the CLI reference (`curlgo skill cli-reference`, section `curl-import-options`).

## 0.1.0 — 2026-11-06

- Introduce a standalone `./cmd/curlgo` build using the existing HTTP execution core.
- Embed the API Test guide and five reference topics in the executable, available through `curlgo skill [topic]` without source or installed reference copies.
- Identify the runtime/guide version and end each guide with a completeness marker.
- Preserve existing JSON v1 help, request, validation, and cancellation behavior.

The request capabilities and workflow recipes predate this independent CLI version. This release distributes standalone binaries; upgrades replace the executable and its embedded guide together. An automatic updater is not included.
