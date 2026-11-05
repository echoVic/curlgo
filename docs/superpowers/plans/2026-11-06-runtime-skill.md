# Runtime-owned API Test Guide Implementation Plan

> Execute inline using test-driven-development, then request an independent code review.

**Goal:** Follow the approved ego-lite model: a stable installed skill bootstraps a guide embedded in the installed CLI, with no stale local reference copies.

**Architecture:** `internal/cli` owns the runtime guide and references, embedded using Go embed. `curlgo skill [topic]` writes Markdown with an explicit end marker. A standalone `cmd/curlgo` entry point shares the CLI/core but does not import the GUI. Its runtime version and guide ship together, separately from the desktop version in `mygo.json`.

**Tech Stack:** Existing Go toolchain and standard library only.

**Spec:** User approval in this chat of the ego-lite bootstrap model and separate desktop / CLI-with-guide delivery.

## Constraints

- Preserve the existing run/help JSON v1 contract and GUI startup.
- Skill guide commands do not read stdin, contact HTTP services, load the GUI workspace, or write files.
- Default guide and named references are available without the repository or skill reference files.
- Unknown topics/arguments return INVALID_USAGE and exit 1 without echoing user input.
- Detect truncated output with `[curlgo:skill] end of guide`; output failures exit 3.
- No automatic downloading, agent configuration changes, or publishing.
- Keep ongoing changes from the earlier version discussion and revise their documentation to this model.

## Review Focus

- Named reference access must use a fixed allowlist, including traversal and absolute-path inputs.
- Output writes may fail or short-write; never claim a complete guide in that case.
- Standalone stdin must remain cancellable on SIGINT.
- A guide consumed outside the repository cannot depend on relative Markdown links.
- Root desktop CLI commands and standalone commands must use the same embedded guide.

## Task 1: Runtime-owned guide and bootstrap

Files: `internal/cli/skill.go`, `internal/cli/skill_test.go`, `internal/cli/guide/`, `internal/cli/help.go`, `skills/api-test/SKILL.md`, `skills/api-test/references/install.md`.

- [x] Write failing Dispatch tests for the default guide, all topics, invalid usage, and output failures; run and observe failure.
- [x] Move current guide/references into the embedded runtime tree and update cross references to `curlgo skill <topic>`.
- [x] Implement Markdown guide output, fixed topics, version header, completeness marker, and root help discovery.
- [x] Replace installed skill with stable bootstrap and an installation/old CLI reference.
- [x] Run focused tests.

## Task 2: Standalone CLI and documentation

Files: `cmd/curlgo/`, `internal/cli/entry.go`, `internal/cli/input_reader.go`, `main_input.go`, `main.go`, `main_cli_test.go`, `scripts/test_api_skill.py`, README and release/skill docs.

- [x] Test standalone no-argument help, unknown commands, embedded guides from another directory, and stdin cancellation; observe failure before implementing.
- [x] Share cancellation-aware stdin reading; add CLI-only entry point and versioned runtime/guide build instructions.
- [x] Make recipe acceptance tests consume the guide emitted by the actual binary.
- [x] Update install, release, version, and changelog docs to distinguish bootstrap from embedded runtime guide.
- [x] Run full Go tests/vet, standalone and desktop builds, and existing local recipe acceptance.
- [x] Request independent review and resolve material findings.

## Delivery

Leave reviewable changes in this checkout. Do not commit, tag, publish, replace installed binaries, or modify globally installed agent skills.

## Verification evidence

- RED: Dispatch skill tests failed because `skill` was unhandled; standalone acceptance failed because `cmd/curlgo` did not exist.
- GREEN: shared/desktop and standalone acceptance pass, including guide output from outside the repository, safe usage errors, request behavior, blocked-stdin SIGINT, and active-request cancellation. Desktop light/dark render acceptance remains covered.
- `go test ./...` and `go vet ./...` passed.
- Standalone `-trimpath` build passed all 19 local recipe scenarios. Recipes now come from that binary's `skill workflows`, including final business-state and identity failure cases.
- Active document links and embedded reference destinations checked; standalone dependency graph contains no MyGo packages.
- Independent review found an old executable compatibility issue: unknown `skill` can fall through to GUI. Bootstrap now probes existing JSON help first; unsupported installs follow installation guidance. Root help discovery has a regression test.
- No release, tag, or installed application was changed.
- Review follow-up verified the compatibility fix; no remaining actionable findings.
