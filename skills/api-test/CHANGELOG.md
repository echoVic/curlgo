# API Test bootstrap changelog

This file tracks the installed bootstrap, not the CLI or its runtime guide.

## 0.1.0 — 2026-11-06

- Keep only the stable `curlgo skill` entry point and installation/older-version guidance in the installed skill.
- Probe existing JSON help before invoking `skill`, so older desktop executables do not fall through to GUI startup.
- Load complete instructions and references from the actual executable, with an explicit completeness marker.
- Re-read the guide after switching or upgrading the executable.

The CLI and full guide are released together. Their version lives in `internal/cli/VERSION`; changing this bootstrap does not update an installed binary.
