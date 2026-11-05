"""Exercise api-test recipes through the real CLI and a loopback HTTP service.

No third-party Python packages. Public, unauthenticated GETs require --public.
Temporary inputs, outputs, credentials, and uploads are owned and auto-cleaned.
"""

import argparse
import ast
import hashlib
import json
import os
import re
import shutil
import ssl
import subprocess
import tempfile
import threading
import time
import unittest
from email.parser import BytesParser
from email.policy import default
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlencode, urlsplit


ROOT = Path(__file__).resolve().parents[1]
TOKEN = 'fixture-"quoted"\\token'
MESSAGE = 'say "hello" \\path\n中文 {{literal}}'
STATUS = {"source": "status", "operator": "equals", "expected": "200"}
BUSINESS = {"source": "json", "operator": "equals", "selector": "/ok", "expected": "true"}


def private_write(path, data):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb") as f:
        f.write(data)


class Service(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, value, status=200, headers=()):
        self.raw(json.dumps(value, ensure_ascii=False).encode(), "application/json", status, headers)

    def raw(self, data, content_type, status=200, headers=()):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        for key, value in headers:
            self.send_header(key, value)
        self.end_headers()
        self.wfile.write(data)

    def task_route(self, raw=None):
        path = urlsplit(self.path).path
        if not path.startswith('/tasks') or not hasattr(self.server, 'task_mode'):
            return False
        self.server.task_requests.append((self.command, path))
        if self.command == 'POST' and path == '/tasks':
            if self.server.task_mode == 'create_failure':
                self.reply({'ok': False, 'data': {'id': self.server.task_id}})
            else:
                self.server.task = {'id': self.server.task_id, 'state': 'new',
                                    'message': json.loads(raw)['message']}
                self.reply({'ok': True, 'data': dict(self.server.task)}, 201)
        elif self.command == 'POST' and path == '/tasks/' + self.server.task_id + '/complete':
            self.server.task['state'] = 'done'
            self.reply({'ok': True})
        elif self.command == 'GET' and path == '/tasks/' + self.server.task_id:
            data = dict(self.server.task)
            if self.server.task_mode == 'query_pending':
                data['state'] = 'pending'
            elif self.server.task_mode == 'query_wrong_id':
                data['id'] = 'another-task'
            elif self.server.task_mode == 'query_wrong_message':
                data['message'] = 'another-message'
            self.reply({'ok': True, 'data': data})
        else:
            self.reply({'ok': False}, 404)
        return True

    def do_GET(self):
        path = urlsplit(self.path).path
        with self.server.counter_lock:
            self.server.calls += 1
        if self.task_route():
            return
        if path == "/health":
            self.reply({"ok": True})
        elif path == "/echo":
            self.reply({"ok": True, "query": parse_qs(urlsplit(self.path).query)})
        elif path == "/me":
            ok = self.headers.get("Authorization") == "Bearer " + TOKEN
            self.reply({"ok": ok, "user": {"id": 9007199254740993}}, 200 if ok else 401)
        elif path == "/session":
            ok = "session=fixture" in self.headers.get("Cookie", "")
            self.reply({"ok": ok}, 200 if ok else 401)
        elif path == "/business-failure":
            self.reply({"ok": False, "data": {"access_token": TOKEN}})
        elif path == "/export":
            self.reply({"ok": True, "rows": [{"id": 9007199254740993}], "padding": "x" * 300000})
        elif path == "/binary":
            self.raw(bytes(range(256)), "application/octet-stream")
        elif path == "/truncated":
            self.raw(b"x" * ((4 << 20) + 1), "text/plain")
        elif path == "/partial":
            self.send_response(200)
            self.send_header("Content-Length", "100")
            self.end_headers()
            self.wfile.write(b"partial")
            self.close_connection = True
        elif path == "/redirect":
            self.reply({}, 302, [("Location", "/health")])
        elif path == "/slow":
            time.sleep(1.2)
            try:
                self.reply({"ok": True})
            except BrokenPipeError:
                pass
        else:
            self.reply({"ok": False}, 404)

    def do_POST(self):
        path = urlsplit(self.path).path
        with self.server.counter_lock:
            self.server.calls += 1
        raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        if self.task_route(raw):
            return
        if path == "/login":
            self.reply({"ok": True, "data": {"access_token": TOKEN}},
                       headers=[("Set-Cookie", "session=fixture; Path=/; HttpOnly")])
        elif path == "/echo":
            try:
                payload = json.loads(raw)
            except ValueError:
                self.reply({"ok": False}, 400)
                return
            self.reply({"ok": True, "received": payload, "query": parse_qs(urlsplit(self.path).query)})
        elif path == "/upload":
            message = BytesParser(policy=default).parsebytes(
                b"Content-Type: " + self.headers["Content-Type"].encode()
                + b"\r\nMIME-Version: 1.0\r\n\r\n" + raw)
            parts = {}
            for part in message.iter_parts():
                data = part.get_payload(decode=True)
                parts[part.get_param("name", header="content-disposition")] = {
                    "sha256": hashlib.sha256(data).hexdigest(),
                    "bytes": len(data), "filename": part.get_filename(),
                }
            self.reply({"ok": True, "parts": parts})
        else:
            self.reply({"ok": False}, 404)


def serve():
    server = ThreadingHTTPServer(("127.0.0.1", 0), Service)
    server.calls = 0
    server.counter_lock = threading.Lock()
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server


class Scenarios(unittest.TestCase):
    binary = None

    @classmethod
    def setUpClass(cls):
        cls.server = serve()
        cls.base = "http://127.0.0.1:" + str(cls.server.server_port)

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="curlgo-skill-test-")
        self.addCleanup(self.temporary.cleanup)
        self.work = Path(self.temporary.name)
        self.sequence = 0

    def write_json(self, name, value):
        path = self.work / name
        private_write(path, json.dumps(value, ensure_ascii=False).encode())
        return path

    def call(self, request, *flags, settings=None):
        self.sequence += 1
        path = self.write_json(f"request-{self.sequence}.json", {
            "schemaVersion": 1, "request": request, **({"settings": settings} if settings else {}),
        })
        return self.execute(["--request", str(path), *flags])

    def execute(self, args):
        proc = subprocess.run([self.binary, "run", *args, "--out-dir", str(self.work)],
                              capture_output=True, timeout=15)
        private_write(self.work / ("output-" + os.urandom(8).hex() + ".json"), proc.stdout)
        self.assertEqual(proc.stderr, b"", "Unexpected CLI stderr")
        result = json.loads(proc.stdout)
        self.assertEqual(result["schemaVersion"], 1)
        return proc.returncode, result

    def request(self, path, **extra):
        return {"url": self.base + path, "assertions": [STATUS, BUSINESS], **extra}

    def successful(self, code, result):
        self.assertEqual(code, 0, "CLI failed; see private output")
        self.assertTrue(result["ok"])
        self.assertEqual(result["result"]["statusCode"], 200)

    def body(self, result):
        body = result["result"]["body"]
        self.assertFalse(body["truncated"])
        if body["mode"] == "inline":
            return body["text"].encode()
        path = Path(body["path"])
        self.assertEqual(path.parent, self.work)
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        return path.read_bytes()

    def test_dry_run_sends_nothing_and_creates_no_cookie(self):
        before = self.server.calls
        jar = self.work / "cookies.json"
        code, result = self.call(self.request("/health"), "--cookie-jar", str(jar), "--dry-run")
        self.assertEqual(code, 0)
        self.assertTrue(result["validation"]["dryRun"])
        self.assertNotIn("result", result)
        self.assertEqual(self.server.calls, before)
        self.assertFalse(jar.exists())

    def test_json_payload_preserves_quotes_newlines_and_literal_variables(self):
        settings = {"environment": "local", "environments": {
            "local": {"payload_json": json.dumps({"message": MESSAGE}, ensure_ascii=False)},
        }}
        code, result = self.call(self.request("/echo", method="POST", body="{{payload_json}}",
                                             headers=[{"key": "Content-Type", "value": "application/json"}]),
                                 settings=settings)
        self.successful(code, result)
        self.assertEqual(json.loads(self.body(result))["received"]["message"], MESSAGE)

    def test_literal_json_field_interpolation_is_not_json_escaping(self):
        code, result = self.call(self.request("/echo", method="POST", body='{"message":"{{message}}"}'),
                                 settings={"environment": "local", "environments": {"local": {"message": MESSAGE}}})
        self.assertEqual(code, 4)
        self.assertEqual(result["result"]["statusCode"], 400)

    def test_query_values_are_encoded_once(self):
        value = "a & b+中文"
        for address, settings in [
            (self.base + "/echo?" + urlencode({"q": value}), None),
            (self.base + "/echo?q={{term}}",
             {"environment": "local", "environments": {"local": {"term": value}}}),
        ]:
            code, result = self.call({"url": address, "assertions": [STATUS, BUSINESS]}, settings=settings)
            self.successful(code, result)
            self.assertEqual(json.loads(self.body(result))["query"], {"q": [value]})

    def test_query_metadata_does_not_change_cli_url(self):
        code, result = self.call(self.request("/echo", queryParams=[{"key": "q", "value": "ignored"}]))
        self.successful(code, result)
        self.assertEqual(json.loads(self.body(result))["query"], {})

    def test_private_token_chain(self):
        code, login = self.call(self.request("/login", method="POST", extractors=[
            {"variable": "secret.token", "source": "json", "selector": "/data/access_token"},
        ]))
        self.successful(code, login)
        secrets = self.write_json("secrets.json", {"schemaVersion": 1, "variables": {
            "token": login["result"]["extractedVariables"]["secret.token"],
        }})
        before = secrets.read_bytes()
        code, result = self.call(self.request("/me", auth={"mode": "bearer", "token": "{{secret.token}}"},
                                             assertions=[STATUS, BUSINESS, {
                                                 "source": "json", "operator": "equals",
                                                 "selector": "/user/id", "expected": "9007199254740993",
                                             }]), "--secrets-file", str(secrets))
        self.successful(code, result)
        self.assertEqual(secrets.read_bytes(), before)

    def test_cookie_chain_is_explicit_and_serial(self):
        jar = self.work / "cookies.json"
        self.successful(*self.call(self.request("/login", method="POST"), "--cookie-jar", str(jar)))
        self.successful(*self.call(self.request("/session"), "--cookie-jar", str(jar)))
        code, result = self.call(self.request("/session"))
        self.assertEqual(code, 4)
        self.assertEqual(result["result"]["statusCode"], 401)
        self.assertEqual(jar.stat().st_mode & 0o777, 0o600)

    def test_business_failure_blocks_extraction_even_with_http_200(self):
        code, result = self.call(self.request("/business-failure", extractors=[
            {"variable": "secret.token", "source": "json", "selector": "/data/access_token"},
        ]))
        self.assertEqual(code, 4)
        self.assertEqual(result["error"]["code"], "ASSERTION_FAILED")
        self.assertEqual(result["result"]["statusCode"], 200)
        self.assertNotIn("extractedVariables", result["result"])

    def test_http_404_without_assertions_can_exit_zero(self):
        code, result = self.call({"url": self.base + "/missing"})
        self.assertEqual(code, 0)
        self.assertTrue(result["ok"])
        self.assertEqual(result["result"]["statusCode"], 404)

    def test_large_json_and_binary_are_read_from_files(self):
        for path in ["/export", "/binary"]:
            code, result = self.call({"url": self.base + path, "assertions": [STATUS]})
            self.successful(code, result)
            self.assertEqual(result["result"]["body"]["mode"], "file")
            self.assertEqual(result["result"]["body"]["encoding"], "binary")
            data = self.body(result)
            if path == "/export":
                self.assertEqual(json.loads(data)["rows"][0]["id"], 9007199254740993)
            else:
                self.assertEqual(data, bytes(range(256)))

    def test_truncated_and_partial_bodies_are_incomplete(self):
        code, result = self.call({"url": self.base + "/truncated", "assertions": [STATUS]})
        self.assertEqual(code, 4)
        self.assertTrue(result["result"]["body"]["truncated"])
        self.assertEqual(result["result"]["assertions"]["results"][0]["code"], "TRUNCATED_RESPONSE")
        code, result = self.call({"url": self.base + "/partial"})
        self.assertEqual(code, 2)
        self.assertEqual(result["error"]["code"], "BODY_READ_ERROR")
        self.assertEqual(self.body(result), b"partial")

    def test_redirect_defaults_differ_for_json_and_curl(self):
        self.successful(*self.call(self.request("/redirect")))
        for follow in [False, True]:
            path = self.work / ("redirect-" + str(follow) + ".curl")
            private_write(path, ("curl " + ("-L " if follow else "") + "'" + self.base + "/redirect'").encode())
            code, result = self.execute(["--curl", str(path)])
            self.assertEqual(code, 0)
            self.assertEqual(result["result"]["statusCode"], 200 if follow else 302)
        code, result = self.call({"url": self.base + "/redirect", "followRedirects": False})
        self.assertEqual(code, 0)
        self.assertEqual(result["result"]["statusCode"], 302)

    def test_uploaded_bytes_and_literal_at_text(self):
        data = b"owned upload\n" + "中文".encode()
        path = self.work / "upload.txt"
        private_write(path, data)
        code, result = self.call(self.request("/upload", method="POST", bodyMode="multipart", formParts=[
            {"kind": "text", "name": "label", "value": "@literal"},
            {"kind": "file", "name": "upload", "filePath": str(path)},
        ]))
        self.successful(code, result)
        parts = json.loads(self.body(result))["parts"]
        self.assertEqual(parts["upload"]["sha256"], hashlib.sha256(data).hexdigest())
        self.assertEqual(parts["label"]["sha256"], hashlib.sha256(b"@literal").hexdigest())

    def test_timeout_is_execution_failure(self):
        code, result = self.call(self.request("/slow", timeoutSeconds=1))
        self.assertEqual(code, 2)
        self.assertEqual(result["error"]["code"], "TIMEOUT")

    def test_private_ca_keeps_certificate_verification(self):
        openssl = shutil.which("openssl")
        if not openssl:
            self.skipTest("openssl unavailable for local certificate fixture")
        cert, key = self.work / "cert.pem", self.work / "key.pem"
        config = self.work / "openssl.cnf"
        private_write(config, b"[req]\ndistinguished_name=dn\nx509_extensions=ext\nprompt=no\n"
                      b"[dn]\nCN=127.0.0.1\n[ext]\nsubjectAltName=IP:127.0.0.1\n"
                      b"basicConstraints=critical,CA:TRUE\n")
        proc = subprocess.run([openssl, "req", "-x509", "-newkey", "rsa:2048", "-nodes",
                               "-keyout", str(key), "-out", str(cert), "-days", "1",
                               "-config", str(config)], capture_output=True, timeout=15)
        self.assertEqual(proc.returncode, 0, "Certificate fixture generation failed")
        key.chmod(0o600)
        server = ThreadingHTTPServer(("127.0.0.1", 0), Service)
        server.calls, server.counter_lock = 0, threading.Lock()
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(cert, key)
        server.socket = context.wrap_socket(server.socket, server_side=True)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        request = {"url": f"https://127.0.0.1:{server.server_port}/health", "assertions": [STATUS, BUSINESS]}
        code, result = self.call(request)
        self.assertEqual(code, 2)
        self.assertEqual(result["error"]["code"], "TLS_ERROR")
        self.successful(*self.call(request, "--ca-file", str(cert)))

    def test_documented_python_recipes_execute(self):
        namespace = {}
        recipe = self.recipe('JSON Bodies And Query Parameters')
        recipe = recipe.replace('binary = "/tmp/curlgo-api-test"', "binary = " + repr(self.binary))
        recipe = recipe.replace('base_url = "http://127.0.0.1:8080"', "base_url = " + repr(self.base))
        exec(compile(recipe, "workflows.md:json", "exec"), namespace)
        exec(compile(self.recipe('Serial Login Chains'), "workflows.md:login", "exec"), namespace)

    def recipe(self, heading):
        text = (ROOT / 'skills/api-test/references/workflows.md').read_text()
        sections = re.split(r'^## ', text, flags=re.M)
        section = next((s for s in sections if s.splitlines()[0] == heading), None)
        self.assertIsNotNone(section, 'Missing runnable workflow: ' + heading)
        blocks = re.findall(r'```python\n(.*?)\n```', section, re.S)
        self.assertTrue(blocks, 'Missing Python recipe: ' + heading)
        return blocks[0]

    def business_chain(self, mode):
        server = serve()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        server.task_mode, server.task_id = mode, 'task-' + os.urandom(6).hex()
        server.task_requests = []
        namespace = {}
        # Reuse documented helpers without sending the unrelated echo example.
        setup = ast.parse(self.recipe('JSON Bodies And Query Parameters'))
        setup.body = [node for node in setup.body
                      if isinstance(node, (ast.Import, ast.ImportFrom, ast.FunctionDef))]
        exec(compile(setup, 'workflows.md:helpers', 'exec'), namespace)
        exec(compile(self.recipe('Business Chains'), 'workflows.md:chain', 'exec'), namespace)
        work = self.work / mode
        work.mkdir(mode=0o700)
        summary = namespace['run_business_chain'](
            self.binary, f'http://127.0.0.1:{server.server_port}', work, MESSAGE)
        self.assertEqual(summary, json.loads((work / 'summary.json').read_bytes()))
        self.assertNotIn(MESSAGE, json.dumps(summary, ensure_ascii=False))
        for path in work.rglob('*'):
            self.assertEqual(path.stat().st_mode & 0o777, 0o700 if path.is_dir() else 0o600)
        return summary, server, work

    def test_business_chain_passes_and_retains_step_evidence(self):
        summary, server, work = self.business_chain('success')
        self.assertEqual(summary['status'], 'passed')
        self.assertEqual([s['status'] for s in summary['steps']], ['passed'] * 3)
        self.assertEqual(server.task['message'], MESSAGE)
        self.assertEqual(server.task['state'], 'done')
        self.assertEqual(server.task_requests, [
            ('POST', '/tasks'), ('POST', '/tasks/' + server.task_id + '/complete'),
            ('GET', '/tasks/' + server.task_id)])
        for step in summary['steps']:
            evidence = work / step['name']
            self.assertTrue((evidence / 'request.json').is_file())
            self.assertTrue(list(evidence.glob('result-*.json')))

    def test_business_chain_stops_after_upstream_business_failure(self):
        summary, server, work = self.business_chain('create_failure')
        self.assertEqual(summary['status'], 'failed')
        self.assertEqual([s['status'] for s in summary['steps']], ['failed', 'not_run', 'not_run'])
        self.assertEqual(server.task_requests, [('POST', '/tasks')])
        self.assertEqual(summary['steps'][0]['httpStatus'], 200)
        for step in summary['steps'][1:]:
            self.assertEqual(step['reason'], 'dependency_failed:create')
        outputs = [json.loads(p.read_bytes()) for p in (work / 'create').glob('result-*.json')]
        result = next(v['result'] for v in outputs if 'result' in v)
        self.assertNotIn('extractedVariables', result)

    def test_business_chain_rejects_wrong_final_business_result(self):
        for mode in ['query_pending', 'query_wrong_id', 'query_wrong_message']:
            with self.subTest(mode=mode):
                summary, server, _ = self.business_chain(mode)
                self.assertEqual(summary['status'], 'failed')
                self.assertEqual([s['status'] for s in summary['steps']], ['passed', 'passed', 'failed'])
                self.assertEqual([s['httpStatus'] for s in summary['steps']], [201, 200, 200])
                self.assertEqual(summary['steps'][-1]['errorCode'], 'ASSERTION_FAILED')
                self.assertEqual(len(server.task_requests), 3)


def public_checks(binary):
    requests = [
        ("jsonplaceholder", "https://jsonplaceholder.typicode.com/todos/1",
         [{"source": "json", "operator": "equals", "selector": "/id", "expected": "1"},
          {"source": "json", "operator": "equals", "selector": "/completed", "expected": "false"}]),
        ("github", "https://api.github.com/repos/echoVic/curlgo",
         [{"source": "json", "operator": "equals", "selector": "/full_name",
           "expected": json.dumps("echoVic/curlgo")}]),
    ]
    failed = False
    with tempfile.TemporaryDirectory(prefix="curlgo-public-test-") as directory:
        work = Path(directory)
        for name, url, checks in requests:
            path = work / (name + ".json")
            private_write(path, json.dumps({"schemaVersion": 1, "request": {
                "url": url, "timeoutSeconds": 15, "assertions": [STATUS, *checks],
            }}).encode())
            proc = subprocess.run([binary, "run", "--request", str(path), "--out-dir", str(work)],
                                  capture_output=True, timeout=20)
            value = json.loads(proc.stdout)
            result = value.get("result", {})
            limited = result.get("headers", {}).get("X-Ratelimit-Remaining") == ["0"]
            print(json.dumps({"public": name, "exit": proc.returncode, "schemaVersion": value.get("schemaVersion"),
                              "status": result.get("statusCode"), "error": value.get("error", {}).get("code"),
                              "rateLimited": limited}))
            failed |= proc.returncode != 0 or value.get("schemaVersion") != 1 or not value.get("ok")
    return failed


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, help="Trusted CurlGo executable")
    parser.add_argument("--public", action="store_true", help="Also perform two unauthenticated public GETs")
    args = parser.parse_args()
    binary = shutil.which(args.binary)
    if not binary:
        parser.error("CurlGo executable not found")
    Scenarios.binary = binary
    outcome = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Scenarios))
    public_failed = public_checks(binary) if args.public else False
    raise SystemExit(0 if outcome.wasSuccessful() and not public_failed else 1)
