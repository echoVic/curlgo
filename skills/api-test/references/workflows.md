# CurlGo Request Workflows

Use these recipes with an already selected CurlGo executable and authorized endpoints. Python serializes inputs and reads results; all HTTP requests go through CurlGo. Run Python from a private script or stdin, without interpolating credentials into shell commands.

## Checks From Changes

For a supplied diff, MR, or commit, trace the changed behavior to its endpoint and relevant callers. Use the API contract or existing tests to choose expectations; a changed internal function suggests an affected interface but does not prove its behavior. Briefly state the focused checks and execute within the user's authorization. If the request supplies a goal rather than a diff, start from that goal. No planning artifact is required.

For example, a change to task completion suggests: create a fresh task, complete its returned ID, then query that same ID for the expected terminal state and preserved message. If the change also touches repeated completion, invalid state, or authorization, add those cases with their documented error/status assertions. Flag missing expectations or data rather than inventing them. Broaden checks when shared middleware or persistence changes affect other endpoints.

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

## Business Chains

Use a business goal as the scenario: create an object, change it, then verify its persisted identity, state, and relevant fields. The agent or Python orchestrates serial single-request CLI calls. Steps and the scenario summary are orchestration data, not fields in CurlGo's request schema.

This recipe reuses `private_json`, `invoke`, `response_json`, and the imports from the JSON example. It assumes an authorized test API: `POST /tasks` accepts `{message: ...}` and returns HTTP 201 with `{ok: true, data: {id: <generated string>, state: "new"}}`; `POST /tasks/{id}/complete` returns HTTP 200 with `{ok: true}`; `GET /tasks/{id}` returns HTTP 200 with the same ID, `state: "done"`, and the original message. Adapt these contracts to the actual API.

```python
def run_business_chain(binary, base_url, work, message):
    from urllib.parse import quote

    # work is an owned, empty 0700 directory retained until evidence is reviewed.
    summary = {"status": "blocked", "steps": [
        {"name": name, "status": "not_run", "reason": "not_reached"}
        for name in ("create", "complete", "query")
    ]}
    current = None

    def equal(pointer, value):
        # Assertion expectations are JSON literals, not variable templates.
        return {"source": "json", "operator": "equals", "selector": pointer,
                "expected": json.dumps(value, ensure_ascii=False)}

    def step(index, method, path, status, variables, checks=(), **extra):
        nonlocal current
        current = summary["steps"][index]
        current.update(status="failed", reason="incomplete_evidence")
        directory = work / current["name"]
        directory.mkdir(mode=0o700)
        request = {"schemaVersion": 1, "request": {
            "method": method, "url": base_url.rstrip("/") + path,
            "followRedirects": False, "timeoutSeconds": 15,
            "assertions": [
                {"source": "status", "operator": "equals", "expected": str(status)},
                equal("/ok", True), *checks,
            ], **extra,
        }, "settings": {"environment": "chain", "environments": {"chain": variables}}}
        path = directory / "request.json"
        private_json(path, request)
        current["phase"] = "preflight"
        code, value = invoke(binary, path, directory, "--dry-run")
        current["exit"] = code
        if code != 0 or value.get("validation", {}).get("dryRun") is not True:
            current["reason"] = "preparation_failed"
            return None
        current["phase"] = "run"
        code, value = invoke(binary, path, directory)
        result = value.get("result", {})
        current.update(exit=code, httpStatus=result.get("statusCode"),
                       errorCode=value.get("error", {}).get("code"))
        if (code != 0 or value.get("ok") is not True
                or result.get("statusCode") != status
                or result.get("assertions", {}).get("passed") is not True
                or result.get("body", {}).get("truncated") is not False):
            summary["status"] = "failed" if code == 4 else "blocked"
            current["reason"] = "assertion_failed" if code == 4 else "incomplete_evidence"
            return None
        response_json(value)  # Reject incomplete or invalid JSON before propagation.
        current.update(status="passed")
        current.pop("reason", None)
        return value

    try:
        created = step(0, "POST", "/tasks", 201,
                       {"payload_json": json.dumps({"message": message}, ensure_ascii=False)},
                       checks=[equal("/data/state", "new")],
                       headers=[{"key": "Content-Type", "value": "application/json"}],
                       body="{{payload_json}}",
                       extractors=[{"variable": "task_id", "source": "json",
                                    "selector": "/data/id"}])
        if created is None:
            return summary
        task_id = created["result"]["extractedVariables"]["task_id"]
        if not task_id or response_json(created)["data"]["id"] != task_id:
            raise ValueError("Invalid generated ID")
        # Ordinary extraction does not mutate input settings. Encode the path
        # segment separately, then explicitly supply it to subsequent calls.
        variables = {"task_path": quote(task_id, safe="")}
        if step(1, "POST", "/tasks/{{task_path}}/complete", 200, variables) is None:
            return summary
        if step(2, "GET", "/tasks/{{task_path}}", 200, variables, checks=[
            equal("/data/id", task_id), equal("/data/state", "done"),
            equal("/data/message", message),
        ]) is not None:
            summary["status"] = "passed"
        return summary
    except (OSError, ValueError, KeyError, TypeError, RuntimeError,
            subprocess.SubprocessError):
        # Raw exception text may contain secrets. An output/read/transport
        # problem leaves business outcome uncertain; never retry a write here.
        summary["status"] = "blocked"
        if current is not None:
            current.update(status="failed", reason="incomplete_evidence")
        return summary
    finally:
        for entry in summary["steps"]:
            if entry["status"] == "not_run" and current is not None:
                entry["reason"] = "dependency_failed:" + current["name"]
        private_json(work / "summary.json", summary)
```

Call this function with the selected binary, authorized base URL, a fresh directory from `Path(tempfile.mkdtemp(prefix="curlgo-chain-"))`, and the desired message. Inspect `summary["status"]`; a script wrapper must exit nonzero for both `failed` and `blocked`. Each step directory retains its private input and preflight/execution envelopes, and `summary.json` contains only fixed labels, exit/error codes, and HTTP status. These files remain available after failure. After inspection, retain only a sanitized summary in a shared report and remove the recorded owned directory. Use `TemporaryDirectory` instead when evidence should be consumed and automatically cleaned within the run.

Step statuses are `passed`, `failed`, or `not_run`; dependent steps are never counted as passed. Overall `passed` requires the final identity/state/field assertions, `failed` records an observed assertion mismatch, and `blocked` records preparation, transport, extraction, or evidence problems. These outcomes do not establish the service's root cause. A timed-out write may have taken effect; do not automatically repeat creation or completion. Reuse an authorized private secrets file or Cookie jar serially when authentication is required.

For asynchronous APIs, wait for the real event and poll only an authorized read with a finite attempt/time budget. Simulate a callback only against an explicitly designated test fixture. Local fixtures prove CLI orchestration over real HTTP; they do not establish acceptance of the real business system. Remove remote test objects only when the task authorizes cleanup and the API contract defines it.

## Public Endpoints And Captured Exports

For a public read, choose stable expectations from the requested resource: HTTP status, resource ID/name, and selected business fields. GitHub may reject anonymous calls with HTTP 403 and `X-Ratelimit-Remaining: ["0"]`; report the rate limit instead of classifying it as a connection failure. Do not add a real token just to make a smoke test pass.

Large JSON and binary responses use `result.body.path`. Read the path before cleanup; `encoding: "binary"` describes preserved bytes, not a base64 string or proof of non-JSON content. Configure status/business assertions on the request: CurlGo evaluates them from captured bytes even when the body goes to a file. A truncated response cannot prove a full export.
