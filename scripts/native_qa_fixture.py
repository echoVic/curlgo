#!/usr/bin/env python3
"""Loopback-only fixtures for manual macOS CurlGo acceptance.

All credentials and payloads are disposable test values. The observation log
records route names, outcomes, and connection counts, never request headers.
"""

import argparse
from base64 import b64encode as encode_ws_accept
import hashlib
import json
import os
from pathlib import Path
import socket
import struct
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit


TOKEN = "native-qa-token"
SECRET = "native-qa-secret-placeholder"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out-dir", type=Path, required=True)
    parser.add_argument("--port", type=int, default=0)
    args = parser.parse_args()
    args.out_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    lock = threading.Lock()
    counts = {"http": 0, "sse": 0, "ws": 0}

    def record(path, event):
        with lock:
            item = {"time": time.time(), "path": path, "event": event, "active": dict(counts)}
            with (args.out_dir / "observations.jsonl").open("a", encoding="utf-8") as stream:
                stream.write(json.dumps(item) + "\n")
                stream.flush()

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, *_):
            pass

        def reply(self, status, value, headers=()):
            body = json.dumps(value, ensure_ascii=False).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            for key, val in headers:
                self.send_header(key, val)
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self):
            self.do_GET()

        def do_GET(self):
            path = urlsplit(self.path).path
            kind = "sse" if path == "/sse" else "ws" if path == "/ws" else "http"
            with lock:
                counts[kind] += 1
            record(path, "opened")
            try:
                if path == "/health":
                    self.reply(200, {"ok": True, "message": "原生验收", "id": 9007199254740993})
                elif path == "/login":
                    self.reply(200, {"ok": True, "data": {"access_token": TOKEN}},
                               [("Set-Cookie", "native_qa=session-fixture; Path=/; HttpOnly")])
                elif path == "/protected":
                    accepted = self.headers.get("Authorization") == "Bearer " + TOKEN
                    cookie = "native_qa=session-fixture" in self.headers.get("Cookie", "")
                    self.reply(200 if accepted and cookie else 401,
                               {"ok": accepted and cookie, "cookieAccepted": cookie, "tokenAccepted": accepted})
                elif path == "/secret":
                    accepted = self.headers.get("Authorization") == "Bearer " + SECRET
                    self.reply(200 if accepted else 401, {"secretAccepted": accepted})
                elif path == "/business":
                    self.reply(200, {"ok": False, "data": {"access_token": "must-not-propagate"}})
                elif path == "/expire-cookie":
                    self.reply(200, {"ok": True}, [("Set-Cookie", "native_qa=; Max-Age=0; Path=/")])
                elif path == "/echo":
                    size = int(self.headers.get("Content-Length", "0"))
                    if size > 2 << 20:
                        self.reply(413, {"ok": False})
                        return
                    body = self.rfile.read(size)
                    try:
                        payload = json.loads(body)
                    except ValueError:
                        self.reply(400, {"ok": False})
                    else:
                        self.reply(200, {"ok": True, "payload": payload, "bytes": size})
                elif path == "/slow":
                    # Detect cancellation without leaving a sleeping handler.
                    self.connection.settimeout(0.25)
                    until = time.monotonic() + 60
                    while time.monotonic() < until:
                        try:
                            if self.connection.recv(1, socket.MSG_PEEK) == b"":
                                record(path, "peer_closed")
                                return
                        except socket.timeout:
                            pass
                    self.reply(200, {"ok": True})
                elif path == "/large":
                    self.reply(200, {"payload": "x" * (5 << 20)})
                elif path == "/sse":
                    self.send_response(200)
                    self.send_header("Content-Type", "text/event-stream")
                    self.send_header("Cache-Control", "no-cache")
                    self.end_headers()
                    for index in range(20000):
                        data = f"id: {index}\nevent: tick\ndata: native-{index} " + "x" * 1100 + "\n\n"
                        self.wfile.write(data.encode())
                        self.wfile.flush()
                        time.sleep(0.05)
                elif path == "/ws":
                    self.websocket()
                else:
                    self.reply(404, {"ok": False})
            except (BrokenPipeError, ConnectionResetError, TimeoutError):
                record(path, "peer_closed")
            finally:
                with lock:
                    counts[kind] -= 1
                record(path, "closed")

        def websocket(self):
            key = self.headers.get("Sec-WebSocket-Key", "")
            digest = hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()
            self.send_response(101)
            self.send_header("Upgrade", "websocket")
            self.send_header("Connection", "Upgrade")
            accept = encode_ws_accept(digest).decode("ascii")
            self.send_header("Sec-WebSocket-Accept", accept)
            self.end_headers()
            self.connection.settimeout(90)

            def read_exact(size):
                value = self.rfile.read(size)
                if len(value) != size:
                    raise ConnectionResetError()
                return value

            def frame(opcode, payload):
                length = len(payload)
                header = bytes([0x80 | opcode])
                if length < 126:
                    header += bytes([length])
                elif length <= 65535:
                    header += bytes([126]) + struct.pack("!H", length)
                else:
                    header += bytes([127]) + struct.pack("!Q", length)
                self.wfile.write(header + payload)
                self.wfile.flush()

            frame(1, "native-ws-ready".encode())
            while True:
                first, second = read_exact(2)
                opcode, length = first & 15, second & 127
                if length == 126:
                    length = struct.unpack("!H", read_exact(2))[0]
                elif length == 127:
                    length = struct.unpack("!Q", read_exact(8))[0]
                if length > 1 << 20 or not second & 128:
                    frame(8, struct.pack("!H", 1009))
                    return
                mask = read_exact(4)
                payload = bytes(byte ^ mask[i % 4] for i, byte in enumerate(read_exact(length)))
                if opcode == 8:
                    frame(8, payload)
                    return
                if opcode == 9:
                    frame(10, payload)
                elif opcode in (1, 2):
                    frame(opcode, payload)
                    record("/ws", "echoed")

    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    server.daemon_threads = True
    base = f"http://127.0.0.1:{server.server_port}"
    requests = []
    for name, route in [
        ("01 健康检查", "/health"), ("02 登录与提取", "/login"),
        ("03 Cookie 与 Bearer", "/protected"), ("04 业务失败阻止提取", "/business"),
        ("05 机密引用", "/secret"), ("06 慢请求取消", "/slow"),
        ("07 大响应截断", "/large"), ("08 SSE", "/sse"),
        ("09 WebSocket", "/ws"), ("10 Cookie 过期", "/expire-cookie"),
    ]:
        protocol = "SSE" if route == "/sse" else "WebSocket" if route == "/ws" else "HTTP"
        request = {
            "id": "native-" + route[1:], "name": name, "protocol": protocol, "method": "GET",
            "url": ("ws" + base[4:] if protocol == "WebSocket" else base) + route,
            "timeoutSeconds": 30, "followRedirects": True,
        }
        if route in ("/health", "/login", "/protected", "/business"):
            request["assertions"] = [
                {"source": "status", "operator": "equals", "expected": "200"},
                {"source": "json", "operator": "equals", "selector": "/ok", "expected": "true"},
            ]
        if route in ("/login", "/business"):
            request["extractors"] = [{"variable": "token", "source": "json", "selector": "/data/access_token"}]
        if route in ("/protected", "/secret"):
            request["auth"] = {"mode": "bearer", "token": "{{token}}" if route == "/protected" else "{{secret.token}}"}
        requests.append(request)
    collection = {"format": "curlgo-collection", "version": 1, "name": "Mac 原生验收",
                  "groups": [], "requests": requests}
    # Always write UTF-8: on Windows setlocale/cp1252 is the default text
    # encoding, and the fixture contains Chinese names and messages.
    (args.out_dir / "collection.json").write_text(
        json.dumps(collection, ensure_ascii=False, indent=2), encoding="utf-8")
    (args.out_dir / "server.json").write_text(
        json.dumps({"baseURL": base, "pid": os.getpid()}), encoding="utf-8")
    print(json.dumps({"baseURL": base, "pid": os.getpid()}), flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
