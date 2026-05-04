"""Thin HTTP-SSE client for the netra-browser bridge.

No third-party deps — stdlib `urllib` only. JSON-RPC POST to `/rpc`.
"""
from __future__ import annotations

import itertools
import json
import threading
import urllib.error
import urllib.request
from typing import Any, Optional


class BridgeError(Exception):
    """Transport-level failure (connection, timeout, auth, malformed response)."""


class RPCError(Exception):
    """Tool-level error returned in the JSON-RPC `error` field."""

    def __init__(self, code: int, message: str, data: Any = None):
        super().__init__(f"RPC error {code}: {message}")
        self.code = code
        self.message = message
        self.data = data


class Bridge:
    """HTTP-SSE client for one netra-browser instance.

    Thread-safe: the only shared state is the request-id counter, which is
    guarded by a Lock. Each `call()` issues an independent POST.
    """

    def __init__(
        self,
        url: str,
        token: Optional[str] = None,
        *,
        timeout_s: float = 30.0,
    ):
        self.url = url.rstrip("/") + "/rpc"
        self.token = token
        self.timeout_s = timeout_s
        self._counter = itertools.count(1)
        self._lock = threading.Lock()

    def call(self, method: str, params: Any = None, *, timeout_s: Optional[float] = None) -> Any:
        """Issue one JSON-RPC call. Returns the `result` payload.

        Raises:
            RPCError: tool returned an error envelope.
            BridgeError: HTTP/transport failure.
        """
        with self._lock:
            req_id = next(self._counter)
        body = {"jsonrpc": "2.0", "id": req_id, "method": method}
        if params is not None:
            body["params"] = params
        data = json.dumps(body).encode("utf-8")

        req = urllib.request.Request(self.url, data=data, method="POST")
        req.add_header("Content-Type", "application/json")
        if self.token:
            req.add_header("Authorization", f"Bearer {self.token}")

        try:
            with urllib.request.urlopen(req, timeout=timeout_s or self.timeout_s) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as e:
            raise BridgeError(f"HTTP {e.code} on {method}: {e.reason}") from e
        except urllib.error.URLError as e:
            raise BridgeError(f"transport error on {method}: {e.reason}") from e

        try:
            envelope = json.loads(raw)
        except json.JSONDecodeError as e:
            raise BridgeError(f"non-JSON response on {method}: {raw[:200]!r}") from e

        if "error" in envelope and envelope["error"] is not None:
            err = envelope["error"]
            raise RPCError(err.get("code", -1), err.get("message", ""), err.get("data"))
        return envelope.get("result")

    # Convenience wrappers for the most common bridge tools so users don't have to
    # remember names. Optional sugar — `bridge.call(...)` always works.

    def health(self) -> dict:
        return self.call("meta_health")

    def attach(self, debug_url: Optional[str] = None) -> dict:
        params = {"debug_url": debug_url} if debug_url else None
        return self.call("meta_attach", params)

    def new_tab(self, url: str = "about:blank") -> str:
        res = self.call("browser_new_tab", {"url": url})
        return res["target_id"]

    def close_tab(self, target_id: str) -> None:
        self.call("browser_close_tab", {"target_id": target_id})

    def navigate(self, target_id: str, url: str, **opts: Any) -> dict:
        return self.call("browser_navigate", {"target_id": target_id, "url": url, **opts})
