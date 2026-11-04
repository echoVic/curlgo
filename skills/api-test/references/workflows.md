# CurlGo Request Workflows

Use these recipes with an already selected CurlGo executable and authorized endpoints. Python serializes inputs and reads results; all HTTP requests go through CurlGo. Run Python from a private script or stdin, without interpolating credentials into shell commands.

## JSON Bodies And Query Parameters

`request.body` contains serialized text, even for JSON. Serialize twice: the payload once, then the envelope containing it. Environment substitution inside a quoted JSON field can break when the value contains quotes, backslashes, or newlines.

This complete example exercises those characters. Replace the documentation endpoint with the authorized API and adjust assertions to its response contract. The example expects `{ok: true, received: {message: ...}}`.

```python
import json
import os
import subprocess
import tempfile
from pathlib import Path
from urllib.parse import urlencode

binary = "/tmp/curlgo-api-test"  # Or the executable selected on PATH.
base_url = "http://127.0.0.1:8080"
message = 'say "hello" \\path\n中文'
payload = {"message": message}
payload_text = json.dumps(payload, ensure_ascii=False)
envelope = {
    "schemaVersion": 1,
    "request": {
        "method": "POST",
        "url": base_url + "/echo?" + urlencode({"q": "a & b+中文"}),
        "headers": [{"key": "Content-Type", "value": "application/json"}],
        "body": "{{payload_json}}",
        "assertions": [
            {"source": "status", "operator": "equals", "expected": "200"},
            {"source": "json", "operator": "equals",
             "selector": "/ok", "expected": "true"},
            {"source": "json", "operator": "equals",
             "selector": "/received/message",
             "expected": json.dumps(message, ensure_ascii=False)},
        ],
    },
    "settings": {
        "environment": "local",
        "environments": {"local": {"payload_json": payload_text}},
    },
}

def private_json(path, value):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600),
                   "w", encoding="utf-8") as stream:
        json.dump(value, stream, ensure_ascii=False)

def invoke(binary, request_path, work, *flags):
    completed = subprocess.run(
        [binary, "run", "--request", str(request_path),
         "--out-dir", str(work), *flags],
        capture_output=True, check=False,
    )
    output = Path(work) / ("result-" + os.urandom(8).hex() + ".json")
    with os.fdopen(os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600),
                   "wb") as stream:
        stream.write(completed.stdout)
    result = json.loads(completed.stdout)  # Reject incomplete/invalid stdout.
    if result.get("schemaVersion") != 1:
        raise RuntimeError("Unsupported result schema")
    return completed.returncode, result

def response_json(result):
    body = result["result"]["body"]
    if body["truncated"]:
        raise RuntimeError("Incomplete response")
    raw = (body["text"].encode("utf-8") if body["mode"] == "inline"
           else Path(body["path"]).read_bytes())
    return json.loads(raw)

with tempfile.TemporaryDirectory(prefix="curlgo-check-") as directory:
    work = Path(directory)  # TemporaryDirectory creates an owned 0700 directory.
    request_path = work / "request.json"
    private_json(request_path, envelope)
    code, preflight = invoke(binary, request_path, work, "--dry-run")
    if code != 0 or preflight.get("validation", {}).get("dryRun") is not True:
        raise RuntimeError("Local preparation failed")
    code, result = invoke(binary, request_path, work)
    if code != 0 or not result["ok"]:
        raise RuntimeError("API check failed: " + result.get("error", {}).get("code", "unknown"))
    received = response_json(result)
    assert received["received"]["message"] == message
    print("POST /echo: HTTP 200, status/business/payload checks passed")
```

For a one-off payload without literal `{{name}}` text, set `body` directly to `payload_text` and omit the environment settings. For a query template, use `"url": "{{base_url}}/search?q={{term}}"` and raw `base_url` / `term` environment values. CurlGo escapes query replacements; do not pre-encode `term`. `request.queryParams` does not add CLI query parameters.

Variables introduced inside a serialized replacement remain literal because expansion is one pass. If user text itself contains `{{name}}`, the whole-payload replacement above preserves that text instead of resolving it as a second variable.

## Serial Login Chains

Adapt `private_json`, `invoke`, and `response_json` from the preceding example. Use the actual login payload and business contract; the following example assumes login returns `{ok: true, data: {access_token: "..."}}`, then `/me` returns `{ok: true}`. It sends only to the authorized `base_url`.

```python
with tempfile.TemporaryDirectory(prefix="curlgo-login-") as directory:
    work = Path(directory)
    login_path, verify_path = work / "login.json", work / "verify.json"
    secrets_path = work / "secrets.json"
    checks = [
        {"source": "status", "operator": "equals", "expected": "200"},
        {"source": "json", "operator": "equals", "selector": "/ok", "expected": "true"},
    ]
    private_json(login_path, {
        "schemaVersion": 1,
        "request": {
            "method": "POST", "url": base_url + "/login",
            # Set body to json.dumps(the_authorized_login_payload) if required.
            "assertions": checks,
            "extractors": [
                {"variable": "secret.token", "source": "json",
                 "selector": "/data/access_token"},
            ],
        },
    })
    code, _ = invoke(binary, login_path, work, "--dry-run")
    if code != 0:
        raise RuntimeError("Login preparation failed")
    code, login = invoke(binary, login_path, work)
    if (code != 0 or not login["ok"] or login["result"]["statusCode"] != 200
            or login["result"].get("assertions", {}).get("passed") is not True
            or response_json(login).get("ok") is not True):
        raise RuntimeError("Login failed; stop the chain")
    token = login["result"]["extractedVariables"]["secret.token"]
    private_json(secrets_path, {"schemaVersion": 1, "variables": {"token": token}})
    private_json(verify_path, {
        "schemaVersion": 1,
        "request": {
            "url": base_url + "/me",
            "auth": {"mode": "bearer", "token": "{{secret.token}}"},
            "assertions": checks,
        },
    })
    code, _ = invoke(binary, verify_path, work, "--secrets-file", str(secrets_path), "--dry-run")
    if code != 0:
        raise RuntimeError("Authenticated request preparation failed")
    code, verified = invoke(binary, verify_path, work, "--secrets-file", str(secrets_path))
    if code != 0 or not verified["ok"]:
        raise RuntimeError("Authenticated request failed")
    print("Login and authenticated request passed")
```

For a Cookie-only service, omit the token extractor and private token file. Pass the same `--cookie-jar` to login and each subsequent request, and run them serially. The login outcome still needs checking; Cookies can be saved even on assertion or body-read failure. Do not infer success from jar existence.

An HTTP 200 with a failed business assertion exits 4 and has no `extractedVariables`. A non-2xx response skips extraction and can still exit zero without assertions. Stop propagation on any failure and retain only the sanitized outcome. An `OUTPUT_ERROR` means captured bodies or Cookie state may not have been saved.

## Public Endpoints And Captured Exports

For a public read, choose stable expectations from the requested resource: HTTP status, resource ID/name, and selected business fields. GitHub may reject anonymous calls with HTTP 403 and `X-Ratelimit-Remaining: ["0"]`; report the rate limit instead of classifying it as a connection failure. Do not add a real token just to make a smoke test pass.

Large JSON and binary responses use `result.body.path`. Read the path before cleanup; `encoding: "binary"` describes preserved bytes, not a base64 string or proof of non-JSON content. Configure status/business assertions on the request: CurlGo evaluates them from captured bytes even when the body goes to a file. A truncated response cannot prove a full export.
