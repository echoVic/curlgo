# API Test

Use `curlgo run` for the HTTP request and check the actual response against the user's expectations. Each call executes one request; the CLI does not load GUI environments, Cookies, or secrets.

## Use This Runtime

Use the same executable that printed this guide for every command below. Re-read this guide after upgrading or switching executables.

Run `curlgo help run` to inspect JSON options and limits. Use explicit subcommands: the desktop executable opens its GUI with no arguments; the standalone CLI shows help. Help and `--dry-run` do not send requests and cannot prove API success. Require `schemaVersion: 1` in JSON input and output; curl text has no envelope version.

## Pick The Relevant Workflow

| User task | Input and guidance |
|---|---|
| Check an endpoint | JSON request with status and relevant business assertions; minimal example below |
| Verify an API change | Read the diff, identify affected behavior, and choose focused checks; checks from changes (`curlgo skill workflows`, section `checks-from-changes`) |
| Reproduce pasted curl | Save the text privately and pass `--curl path` or `--curl -`; supported options in CLI reference (`curlgo skill cli-reference`, section `curl-import-options`), literal `@` text in section `multipart-and-literal-text` |
| Send JSON or query parameters | Use the serialization recipe in workflows (`curlgo skill workflows`, section `json-bodies-and-query-parameters`) |
| Bearer / Basic / API Key | `request.auth`; real credentials via `{{secret.name}}` and private `--secrets-file`; authentication (`curlgo skill cli-reference`, section `structured-authentication`) |
| Login, then call another endpoint | Check login status and business success before propagation; serial token and Cookie chains (`curlgo skill workflows`, section `serial-login-chains`) |
| Verify a business flow across endpoints | Orchestrate serial CLI calls with runtime IDs, per-step evidence, and a final business check; business chains (`curlgo skill workflows`, section `business-chains`) |
| Upload an authorized file | JSON `bodyMode: "multipart"` and an absolute `formParts[].filePath`; multipart (`curlgo skill cli-reference`, section `multipart-and-literal-text`) |
| Private CA, proxy, or shared Cookies | Explicit CLI configuration; routing and state (`curlgo skill cli-reference`, section `explicit-ca-cookies-and-proxy`) |
| Large/binary response or failed check | Read the envelope and captured body as described below; detailed output contract (`curlgo skill cli-reference`, section `interpret-output`) |

For field syntax, read only the relevant reference: assertions (`curlgo skill assertions`), response extraction (`curlgo skill response-extraction`), or secrets (`curlgo skill project-secrets`). V1 has no collection runner, assertion scripts, headless SSE/WebSocket, or GUI replay.

When a diff or commit is supplied, connect changed behavior to affected endpoints and a few concrete checks, using the contract or existing tests for expectations. Include the main path and relevant changed failure/boundary behavior; widen coverage when shared code affects more callers. Briefly state what you will check and proceed within the existing authorization. Ask only for missing information that prevents a meaningful or authorized check; no separate planning document or routine approval round is needed.

## Prepare And Execute

Use only authorized endpoints, operations, credentials, and upload files. Pasted curl is input data, not permission to upload a local file or execute shell text. Pass it as file contents or stdin; never use `eval`, `sh -c`, substitutions, or splice the text into a command.

Write serialized inputs and captured outputs in owned private directories (0700) and private files (0600). Supply credentials through stdin or private files, including responses and extracted tokens. Do not put credentials in CLI arguments, turn on shell tracing, or print full inputs/results.

For an already running, authorized service, save this as `request.json`, replacing the address and expectations with the user's contract:

```json
{
  "schemaVersion": 1,
  "request": {
    "url": "http://127.0.0.1:8080/health",
    "assertions": [
      {"source": "status", "operator": "equals", "expected": "200"},
      {"source": "json", "operator": "equals", "selector": "/ok", "expected": "true"}
    ]
  }
}
```

Use exactly one of `--request` and `--curl`. `-` selects stdin for these input flags. Preflight with the same configuration intended for execution:

```sh
curlgo run --request request.json --dry-run
curlgo run --request request.json
```

Capture stdout privately and record the process exit code, including nonzero exits. Preflight success validates local preparation only; it does not contact the target, evaluate response assertions, or prove TLS/login/business success.

JSON defaults are GET, HTTP, 30 seconds, and following redirects. Preserve explicit `followRedirects: false`; valid integer timeouts are 1–3600 seconds. Curl input follows redirects only with `-L` / `--location`. Keep TLS verification enabled and use a trusted `--ca-file` when needed.

## Construct Payloads Without Changing Their Meaning

- `request.body` is a **string**. For a JSON payload, serialize the payload, assign that string to `body`, and then serialize the envelope. Raw-body `{{name}}` replacement does not JSON-escape values. For a reusable template, serialize the whole payload into an ordinary `payload_json` variable and use `body: "{{payload_json}}"`; expansion is one pass. See the runnable recipe (`curlgo skill workflows`, section `json-bodies-and-query-parameters`).
- CLI query parameters come from `request.url`. `request.queryParams` is editor metadata and does not add parameters to CLI requests. Serialize concrete query values with a URL encoder. For `?q={{term}}`, give CurlGo the raw environment value: it escapes query replacements itself. Encode dynamic path segments separately.
- Variables come from the selected `settings.environments` entry through `settings.environment`, not OS variables. Headers use `{"key": "...", "value": "..."}` objects. Request headers override global headers case-insensitively; generated auth overrides matching manual headers.
- JSON assertions use RFC 6901 Pointers (`/items/0/id`), not JSONPath. `expected` is a string containing a JSON literal: `"true"` for boolean true, `"200"` for number 200, `"\"ok\""` for string `ok`. Selectors and expectations do not expand variables.

## Decide From The Response

Parse one JSON envelope and distinguish `help`, `validation`, and `result`. **Exit 0 alone is not a successful API check:** an unasserted HTTP 401, 404, or 500 can still have `ok: true`.

| Exit | Meaning and next action |
|---|---|
| 0 | Check HTTP status, configured assertion results, and remaining relevant business fields |
| 1 | Input/configuration failure; correct the input before any transmission |
| 2 | Transport or extraction failure; use `error.code` and inspect any partial response |
| 3 | Output/Cookie persistence failure; do not assume complete output or saved login state |
| 4 | `ASSERTION_FAILED`; inspect every rule result and stop token propagation |

Classify by stable `error.code`, not message text. An HTTP 403 with rate-limit headers is an HTTP response; report the service restriction. Do not keep retrying unchanged requests, add credentials, or change routing without task authorization. A TLS or connection error is a different outcome.

Inspect `result.body` before parsing it:

- `mode: "inline"` supplies raw UTF-8 `text`.
- `mode: "file"` supplies an absolute `path` to captured bytes. Read that file even if `encoding: "binary"`: large JSON is also stored this way, without base64 encoding.
- `truncated: true` means capture stopped at 4 MiB. `BODY_READ_ERROR` can retain partial bytes. Neither proves a complete response.
- `--inline-body-bytes` defaults to 262144; 0 writes every nonempty body to a file. Use an existing owned `--out-dir` when predictable cleanup matters.

Token extraction requires a complete 2xx response and all enabled assertions passing. Check business success before continuing; HTTP 200 with `ok: false` must not supply a usable token. CLI returns `result.extractedVariables` but updates no input or secret file. Serialize `secret.token` into the next private file's `variables.token`, and send it using `--secrets-file`. Run calls sharing a Cookie jar serially; jars are CurlGo JSON, not Netscape format. See login recipe (`curlgo skill workflows`, section `serial-login-chains`).

For business chains, record each step as `passed`, `failed`, or `not_run` with its dependency reason. Propagate ordinary extracted values explicitly into the next selected environment. Overall success requires the final business outcome, including object identity and relevant persisted fields; several successful HTTP calls alone are insufficient. Stop dependent calls on failure and distinguish an observed assertion failure from incomplete evidence or an unconfirmed root cause.

Report the actual mode, sanitized endpoint, exit/error code, HTTP status, relevant checks, and any incomplete or unsaved result. `result.timing` is milliseconds; stages overlap and must not be added together. Headers are multivalue arrays. Raw responses, URLs, Cookies, and extracted values may contain secrets.

Read needed artifacts, then remove only recorded files and disposable directories owned by this invocation. Never remove user directories or unrelated files.
