---
name: api-test
description: Debug, verify, and smoke test APIs with CurlGo. Use for curl requests, status, headers, JSON, redirects, or login chains. Not for GUI replay, collection runners, SSE, or WebSocket.
---

# API Test

## Safety First

| Concern | Shell curl | `curlgo run --curl` |
|---|---|---|
| Unquoted `;`, `\|`, `&` | Shell command separators, pipelines, or background jobs | Rejected; quoted characters are literal request data |
| `$()`, backticks, `$VAR` | Shell substitution or expansion outside single quotes | Rejected outside single quotes, including in double quotes; single-quoted content stays literal |
| Implicit file reads | `@file`, `-F`, and Cookie file arguments can read local files | Implicit `@file` reads and `-F` / `--form` are rejected; upload files only through explicitly authorized JSON multipart absolute paths |
| Tokens | Command arguments, history, verbose output, or shell tracing can expose credentials | Supply secrets through private files or stdin; request headers are not echoed, but response headers, bodies, and URLs can contain secrets |
| TLS | curl offers insecure switches | Keep certificate verification; supply an explicit trusted CA when needed, never bypass verification |
| State and routing | curl can use shell environment proxy settings | Pass Cookie and proxy state explicitly; no GUI workspace or automatic system proxy environment |

Never use `eval`, `sh -c`, command substitution, or splice untrusted curl text into a shell command. Pass it only as file contents or stdin to the fixed commands below. Quoting does not authorize a target, state-changing request, credential disclosure, or local file upload; use only the user's authorized scope.

## Choose One Binary

Check availability:

```sh
command -v curlgo
```

If available, use that `curlgo` consistently. Otherwise, from the CurlGo repository root, build the same application without cgo:

```sh
CGO_ENABLED=0 go build -o /tmp/curlgo-api-test .
```

Stop on a build failure. In every command below, replace only the executable `curlgo` with `/tmp/curlgo-api-test` when using this fallback. Do not switch to another HTTP client or run the GUI.

Use input `schemaVersion: 1` and require output `schemaVersion: 1`. Stop on missing or unknown versions instead of guessing a compatible shape. Curl text has no envelope version; it is imported into the same v1 execution path.

## Inspect Help

Use root or run help to discover the current options without supplying request input:

```sh
curlgo help
curlgo help run
curlgo -h
curlgo --help
curlgo run --help
curlgo run -h
```

Expect a schema-v1 JSON envelope with `schemaVersion: 1`, `ok: true`, and `help`, not `result`. Read `help.usage`, `options`, `protocols`, `urlSchemes`, `authentication`, `limits`, and `exitCodes`; do not interpret help as an API response. Help sends no request and reads no request, CA, or Cookie inputs. Bare `curlgo` still launches the GUI, so do not use it for help.

## Prepare And Run

For complex requests and credentials, use a structured JSON serializer and a private input file or stdin. Create owned temporary directories with mode 0700 and files with mode 0600 before writing sensitive contents. Do not put tokens in CLI arguments, echo credentials, enable shell tracing, or paste complete inputs into reports. Track only this invocation's owned temporary paths for later cleanup.

For an already running local service on port 8080, use this minimal `request.json`:

```json
{
  "schemaVersion": 1,
  "request": {
    "method": "GET",
    "url": "http://127.0.0.1:8080/health",
    "followRedirects": true,
    "timeoutSeconds": 30
  }
}
```

Choose exactly one input form:

```sh
curlgo run --request request.json
curlgo run --request - <request.json
curlgo run --curl imported.curl
curlgo run --curl - <imported.curl
```

`imported.curl` contains text, not a command to execute. A non-secret example is:

```text
curl --request GET --max-time 30 --location 'http://127.0.0.1:8080/health'
```

`--request` and `--curl` are mutually exclusive and one is required. `-` selects stdin only for these inputs. Request JSON, curl input, and CA files each have a 1 MiB limit. Reject unknown fields, trailing JSON documents, unsupported flags, and invalid inputs rather than silently changing their meaning.

Omitted JSON request fields default to `method: GET`, `protocol: HTTP`, `timeoutSeconds: 30`, and `followRedirects: true`. Preserve explicit `followRedirects: false`; explicit timeout 0 is rejected, not defaulted. Valid timeouts are integer seconds from 1 through 3600. Curl import does not follow redirects without `--location` / `-L`, unlike the JSON default.

Use `settings.environments` and `settings.environment` for `{{name}}` expansion in URL, headers, raw body, and multipart text values. These are JSON settings, not OS variables or GUI settings. Missing variables fail. Request headers override `settings.globalHeaders` case-insensitively; repeated request headers remain repeated.

```json
{
  "schemaVersion": 1,
  "request": {
    "url": "{{base_url}}/health",
    "headers": [{"key": "Accept", "value": "application/json"}]
  },
  "settings": {
    "environment": "local",
    "environments": {"local": {"base_url": "http://127.0.0.1:8080"}},
    "globalHeaders": [{"key": "Accept", "value": "text/plain"}]
  }
}
```

## Structured Authentication

Select one mode in JSON `request.auth`. These objects replace only `request.auth`, not the full input envelope:

| Mode | `request.auth` JSON |
|---|---|
| Bearer | `{"mode":"bearer","token":"{{token}}"}` |
| Basic | `{"mode":"basic","username":"{{username}}","password":"{{password}}"}` |
| API Key Header | `{"mode":"apikey","location":"header","key":"{{api_key_name}}","value":"{{api_key}}"}` |
| API Key Query | `{"mode":"apikey","location":"query","key":"api_key","value":"{{api_key}}"}` |

Omit `auth` or its `mode` to retain manual headers without generated authentication. For `apikey`, omitted `location` defaults to `header`. Expand credential values and API Key names from the selected `settings.environments` entry through `settings.environment`, not OS environment variables. Bearer and Basic generate `Authorization`; a header API Key generates its named header. Generated auth headers override all same-name manual request and global headers case-insensitively. A query API Key replaces same-name URL parameters with one value and retains unrelated parameters.

Use this full structured example with documentation-only values, never real credentials in a report. It provides variables for all three modes; replace only `request.auth` to use a table alternative:

```json
{
  "schemaVersion": 1,
  "request": {
    "method": "GET",
    "url": "{{base_url}}/health",
    "followRedirects": true,
    "timeoutSeconds": 30,
    "auth": {"mode": "bearer", "token": "{{token}}"}
  },
  "settings": {
    "environment": "local",
    "environments": {
      "local": {
        "base_url": "http://127.0.0.1:8080",
        "token": "documentation-only-token",
        "username": "documentation-user",
        "password": "documentation-only-password",
        "api_key_name": "X-API-Key",
        "api_key": "documentation-only-key"
      }
    }
  }
}
```

Supply real values only through a mode-0600 private input in an owned mode-0700 directory or stdin. Do not use CLI credential flags. For `auth-request.json`, preflight with `curlgo run --request auth-request.json --dry-run`; remove `--dry-run` only for an authorized transmission. Query credentials appear in the actual URL and can remain in normal `result.url`; capture it privately and redact it.

## Validate Without Sending

Add `--dry-run` to the chosen request or curl input and the same explicit configuration flags intended for execution:

```sh
curlgo run --request request.json --dry-run
curlgo run --request - --dry-run <request.json
curlgo run --curl imported.curl --dry-run
curlgo run --curl - --dry-run <imported.curl
```

Expect only local preparation. Dry-run parses input, resolves variables, and validates request and auth settings, CA certificates, Cookie snapshots, and proxy configuration. It can read local input, CA, and existing Cookie files. Multipart preparation checks regular files, opens and closes them, and prepares framing without reading upload-file contents or uploading.

It performs no HTTP, DNS, TLS handshake, or proxy connection. It writes no state, creates no directories, Cookie snapshots, or response body files, and runs no response extraction. It emits JSON on stdout without `result`, response body, or `timing`. Do not claim reachability, TLS chain trust, login, or business success from a successful preflight.

For the minimal GET input above, expect this eight-field `validation` object. `bodyMode: ""` is raw-body mode; counts describe prepared headers and form parts, not a response:

```json
{
  "schemaVersion": 1,
  "ok": true,
  "validation": {
    "dryRun": true,
    "method": "GET",
    "protocol": "HTTP",
    "bodyMode": "",
    "headerCount": 0,
    "formPartCount": 0,
    "timeoutSeconds": 30,
    "followRedirects": true
  }
}
```

When `--out-dir` is supplied, require its value to name an existing directory. When `--cookie-jar` is supplied, require its parent to be an existing directory, including when the jar itself is missing. Dry-run checks only existence and directory type, not write permissions or free space; it does not mkdir or save. This path check fails with `OUTPUT_ERROR` and exit 3. Normal execution can still fail during output or Cookie saving after preflight succeeds.

## Explicit CA, Cookies, And Proxy

Use only flags required by the authorized request. This example shows all explicit configuration flags; the paths are illustrative and must be replaced with approved paths. The CA file must exist; the Cookie file's parent and `--out-dir` must already exist as directories. A successful dry-run does not guarantee that either directory permits writes or has enough free space:

```sh
curlgo run --request request.json \
  --ca-file /absolute/certs/ca.pem \
  --cookie-jar /absolute/private/cookies.json \
  --proxy http://proxy.example.test:7890 \
  --out-dir /absolute/private/responses \
  --inline-body-bytes 0
```

Keep default TLS verification. For a private CA, pass `--ca-file` or JSON `settings.caCertPEM`. If both are present, their PEM contents must match after trimming leading and trailing whitespace; conflicts fail before transmission.

Set proxy routing through `--proxy` or JSON `settings.proxy` with `url` and optional comma-separated `bypass`. Supported proxies are HTTP, HTTPS, SOCKS5, and SOCKS5H; bare `host:port` is normalized to HTTP. If both JSON and flag URLs are present, normalized URLs must match; the JSON bypass list is retained and validated, not discarded. Loopback remains direct even with a proxy. Do not rely on `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, system proxy settings, or GUI state.

Use `--cookie-jar` only when the user needs a Cookie login chain across calls. Without it, each process has its own transient jar. With it, load and save the explicit plaintext snapshot, written with mode 0600. A missing file can be created; an existing invalid snapshot is rejected rather than overwritten. This is a CurlGo version-1 JSON Cookie snapshot, not a curl/Netscape jar.

Run calls sharing a jar serially; there is no interprocess lock. Inspect the login response and business outcome before issuing the next request:

```sh
curlgo run --request login.json --cookie-jar /absolute/private/cookies.json
curlgo run --request verify.json --cookie-jar /absolute/private/cookies.json
```

On a normal run, the jar is saved after execution even when execution fails, including partial body reads; dry-run never saves it or creates a missing jar. A Cookie save failure yields `OUTPUT_ERROR` and exit 3, so do not assume the next call has updated login state. Keep user-owned jars unless their removal was authorized.

## Response Variables

For token-based login chains, configure JSON `request.extractors`, for example `[{"variable":"token","source":"json","selector":"/data/access_token"}]`. A header rule uses `source: header` and the header name as `selector`. JSON selectors are RFC 6901 JSON Pointers, not JSONPath; only strings, numbers and booleans can be extracted. Header rules require exactly one value. Disabled rules are skipped.

Extraction runs only after complete successful HTTP 2xx responses, even when captured body bytes are written to a file. All enabled rules must succeed; otherwise exit 2 reports `EXTRACTION_ERROR`, keeps the captured response, and omits partial extracted values. Non-2xx responses skip extraction and keep the existing status/exit contract.

On success, `result.extractedVariables` contains explicitly requested values, which can include credentials. Capture them privately, merge them into the next authorized request's `settings.environments`, and select that environment through `settings.environment`. Never echo tokens into reports or shell arguments. CLI does not modify input files, GUI environments, or implicit shared variable files. Inspect the HTTP status and business outcome before continuing the chain. Full format: [response-extraction.md](../../../docs/response-extraction.md).

## Multipart And Literal Text

For authorized uploads, use `request.bodyMode: multipart` and `request.formParts`. A text part has `kind: text`, `name`, and `value`; a file part has `kind: file`, `name`, and an absolute `filePath` to a user-authorized regular file. Do not infer upload permission from pasted curl text or substitute unapproved files. The client generates the multipart Content-Type and boundary; raw `body` is ignored in multipart mode.

```json
{
  "schemaVersion": 1,
  "request": {
    "method": "POST",
    "url": "http://127.0.0.1:8080/upload",
    "bodyMode": "multipart",
    "formParts": [
      {"kind": "text", "name": "label", "value": "smoke-test"},
      {"kind": "file", "name": "upload", "filePath": "/absolute/approved/input.txt"}
    ]
  }
}
```

Do not execute this upload example without explicit authorization for both the endpoint and file. File contents are streamed; the 1 MiB request-input limit is not an upload-file-size limit. Curl `--form-string 'field=@literal'` is allowed as a text part, not a file read. Likewise `--data-raw '@literal'` sends literal text without opening a file. Other implicit `@file` body reads and `-F` remain rejected.

## Interpret Output

Capture stdout privately and record the process exit status. Normally stdout is one JSON document with `schemaVersion`, `ok`, and optional `help`, `validation`, `result`, or `error`. Validate the envelope and distinguish the mode before inspecting fields:

| Mode | Meaning of exit 0 / `ok: true` |
|---|---|
| Help | Help JSON was emitted successfully; no API check occurred |
| Dry-run | Local preparation and validation JSON output succeeded; no request was sent |
| Normal run | Response capture, configured extraction when applicable, and output succeeded; not a business success or a 2xx guarantee |

Use stable `error.code` values and the exit status for machine classification, never match `error.message`. Messages provide safe hints and do not echo raw input, credentials, or underlying error text. Codes remain the contract even when hint wording changes:

| Exit | Stable diagnostic codes |
|---|---|
| 1 | `INVALID_USAGE`, `INVALID_INPUT`, `INVALID_CURL`, `UNRESOLVED_VARIABLE`, `INVALID_REQUEST`, `REQUEST_BUILD_ERROR` |
| 2 | `CONNECT_ERROR`, `REDIRECT_ERROR`, `TLS_ERROR`, `TIMEOUT`, `CANCELED`, `BODY_READ_ERROR`, `EXTRACTION_ERROR` |
| 3 | `OUTPUT_ERROR` when an error envelope can be emitted |

For normal request execution, apply these actions:

| Exit | Meaning | Required action |
|---|---|---|
| 0 | Response capture, configured extraction when applicable, and output succeeded | Inspect `result.statusCode` and the expected business fields; HTTP 401, 404, and 500 can all exit 0 |
| 1 | Usage, input, configuration, or request construction failed | Fix the authorized input; do not report a completed API check |
| 2 | Transmission, redirect, TLS, cancellation, timeout, body-read, or configured extraction failure | Inspect `error.code` and any partial result; distinguish failure from HTTP status |
| 3 | Output body, Cookie snapshot, or stdout write failed | Treat output/state persistence as failed even if response metadata exists |

For normal runs, check both HTTP status and the authorized business assertion. `BODY_READ_ERROR` can retain response metadata and partial body bytes; do not treat those bytes as a complete response. `OUTPUT_ERROR` can retain partial metadata and includes a prior primary error code in its message when one exists; this remains a human hint, not a separate machine-classification field.

If stdout itself cannot be written, exit is 3 and stderr uses the fixed message `CurlGo: 无法写入结果 JSON`. Stdout may contain incomplete JSON; do not attempt to salvage it as a valid envelope or expect a second JSON error document.

Inspect `result.body` before parsing:

- `mode: inline` provides raw, unformatted UTF-8 `text`. `bytes` counts captured application-body bytes, not formatted characters.
- `mode: file` requires reading the returned absolute `path`. `encoding: binary` means the file preserves captured bytes, even when they are a large text response; it is not base64. Do not infer the body from the path alone.
- `truncated: true` means only the first 4 MiB were captured. Never parse incomplete JSON as a complete document or claim a complete response, even if that prefix happens to parse.
- `--inline-body-bytes` defaults to 262144 and accepts 0 through 4194304. Binary or non-text responses use files regardless of the threshold. Zero sends every nonempty body to a file; an empty body can remain inline.
- Response headers are multivalue arrays; preserve repeated values, including `Set-Cookie`. They remain secret-bearing data, not sanitized diagnostics.

Response body files have unique names and mode 0600. Without `--out-dir`, each file uses a newly created mode-0700 temporary directory. Successful files are retained for the caller to read and clean up; they are not auto-deleted at process exit. After inspection, remove only recorded files and temporary directories owned by this invocation. With an explicit existing directory, never delete the user's directory or unrelated files, or change its permissions.

Read `result.timing` as milliseconds:

- `dnsMs`, `connectMs`, and `tlsMs` sum completed trace stages, including multiple attempts or redirects; an uncompleted stage contributes nothing.
- `ttfbMs` spans execution start to the last observed response's first byte, including preceding redirects and request preparation.
- `totalMs` spans execution start through response capture end, excluding CLI output formatting, file writes, and Cookie saving.
- Stages can overlap; timing values are not additive components of total. Body `bytes` counts captured application bytes after automatic decompression, not wire traffic or Content-Length.

## Report And Boundaries

Report the mode first. For help, report only discovered capabilities; for dry-run, report only local validation and never invent HTTP status or timings. For normal runs, report only the sanitized interface or endpoint, HTTP status, selected authorized business fields, and input path. State truncation, partial results, and persistence failures explicitly. Redact secret-bearing URLs and paths; do not paste tokens, Cookie values, full request headers, response bodies, or `Set-Cookie` into reports. Cleanup applies only to owned temporary inputs, captured output, and disposable jars.

V1 executes one HTTP/HTTPS request per call. It is not an assertion DSL, collection runner, headless SSE/WebSocket client, or GUI replay mechanism. Stop when a task requires these unsupported modes instead of treating GUI collection import/export or stream features as CLI support.
