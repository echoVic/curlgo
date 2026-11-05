---
name: api-test
description: Use when testing or debugging HTTP/HTTPS APIs with CurlGo, including checks for changed API behavior, serial business chains, pasted curl, JSON payloads, authentication, uploads, and response assertions. Not for GUI replay, collection runners, SSE, or WebSocket.
metadata:
  version: "0.1.0"
  bootstrap: "true"
---

# API Test

Before any CurlGo API testing task, check the installed executable safely:

```sh
curlgo help
```

Require a successful JSON help response whose `help.usage` includes `curlgo skill [topic]`. If the executable is missing, help fails, or that usage is absent, read [installation and older versions](references/install.md) before proceeding. Do not probe `skill` on an unsupported executable: older desktop versions can open their GUI for unknown subcommands.

Once support is confirmed, run:

```sh
curlgo skill
```

Follow the guide printed by that executable. It ships with the CLI and matches its supported behavior. Use that same executable for every subsequent command, including `curlgo skill <topic>` to read references. Read the guide again after upgrading or switching executables.

The guide must end with `[curlgo:skill] end of guide`. If the marker is missing, treat it as incomplete: save the output to a file in an owned private directory and read it in parts. Do not send requests based on a truncated guide.

Do not use cached instructions from another CLI version or silently switch HTTP clients.
