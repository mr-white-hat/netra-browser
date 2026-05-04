"""Shared fixtures: an in-process HTTP server that mimics the bridge's /rpc."""
from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Callable

import pytest


class FakeBridgeServer:
    """Minimal stand-in for the netra-browser HTTP-SSE transport.

    Drives the test by routing JSON-RPC method names to handler callables
    registered via `register(method, fn)`. fn receives the params dict and
    returns the `result` payload (or raises to trigger an error envelope).
    """

    def __init__(self, token: str | None = None):
        self.token = token
        self.handlers: dict[str, Callable[[dict], object]] = {}
        self.calls: list[tuple[str, dict]] = []
        self._lock = threading.Lock()
        self._server: ThreadingHTTPServer | None = None
        self._thread: threading.Thread | None = None
        self.port = 0

    def register(self, method: str, fn: Callable[[dict], object]) -> None:
        self.handlers[method] = fn

    def start(self) -> str:
        outer = self

        class H(BaseHTTPRequestHandler):
            def log_message(self, *_):  # silence
                return

            def do_POST(self):
                if self.path != "/rpc":
                    self.send_error(404)
                    return
                if outer.token:
                    auth = self.headers.get("Authorization", "")
                    if auth != f"Bearer {outer.token}":
                        self.send_response(401)
                        self.end_headers()
                        return
                length = int(self.headers.get("Content-Length", 0))
                body = json.loads(self.rfile.read(length))
                with outer._lock:
                    outer.calls.append((body.get("method"), body.get("params") or {}))
                fn = outer.handlers.get(body.get("method"))
                resp = {"jsonrpc": "2.0", "id": body.get("id")}
                try:
                    if fn is None:
                        resp["error"] = {"code": -32601, "message": f"unknown: {body.get('method')}"}
                    else:
                        resp["result"] = fn(body.get("params") or {})
                except Exception as e:  # noqa: BLE001
                    resp["error"] = {"code": -32603, "message": str(e)}
                payload = json.dumps(resp).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

        self._server = ThreadingHTTPServer(("127.0.0.1", 0), H)
        self.port = self._server.server_address[1]
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()
        return f"http://127.0.0.1:{self.port}"

    def stop(self):
        if self._server is not None:
            self._server.shutdown()
            self._server.server_close()
        if self._thread is not None:
            self._thread.join(timeout=2)


@pytest.fixture
def fake_bridge():
    s = FakeBridgeServer()
    url = s.start()
    yield s, url
    s.stop()


@pytest.fixture
def fake_bridge_with_token():
    s = FakeBridgeServer(token="SECRET")
    url = s.start()
    yield s, url
    s.stop()
