"""Thin HTTP-SSE client for the netra-browser bridge.

No third-party deps — stdlib `urllib` only. JSON-RPC POST to `/rpc`.
"""
from __future__ import annotations

import itertools
import json
import os
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

    # ------------------------------------------------------------------
    # netra-actions JS bundle injection (Plan H component #5).
    # ------------------------------------------------------------------

    _actions_cache: Optional[str] = None

    def inject_actions(self, target_id: str, *, path: Optional[str] = None) -> str:
        """Inject the netra-actions JS bundle into target_id and return the bundle version.

        After this call, helpers live at `window.__netra.*` in the page —
        invoke them via `bridge.call("browser_eval", {target_id, expression: "window.__netra.detectFrameworks()"})`.

        Args:
            target_id: target to inject into.
            path: explicit path to netra-actions.js. Default: search next to the
                bridge binary, then fall back to a sibling `js/` dir of the
                netra_fanout package, then env var NETRA_ACTIONS_JS.

        Returns:
            The bundle's version string (e.g. "0.1.0").
        """
        if Bridge._actions_cache is None:
            Bridge._actions_cache = _load_actions_bundle(path)
        res = self.call("browser_eval", {"target_id": target_id, "expression": Bridge._actions_cache})
        return res.get("result")  # the IIFE returns __netra.version


    # ------------------------------------------------------------------
    # SSE event stream (Plan H component #6).
    # ------------------------------------------------------------------

    def subscribe_events(self, target_id: str, *, types: Optional[list[str]] = None, timeout_s: float = 60.0):
        """Stream live events from the bridge's /events SSE endpoint.

        Yields dicts of shape `{"event": str, "target_id": str, "at_ms": int, "params": dict}`
        as they arrive, plus one initial `{"event": "ready", ...}` so the caller
        knows the stream is connected.

        Stops when the connection drops or the iterator is closed.

        Args:
            target_id: target to subscribe to.
            types: friendly type names (`navigation`, `network_request`,
                `network_response`, `console`, `dialog`, `load`,
                `domcontentloaded`). Default: every supported type.
            timeout_s: max seconds to block on a single read. The 20s
                heartbeat from the server keeps the connection alive.

        Example:

            for ev in bridge.subscribe_events(tid, types=["console"]):
                if ev["event"] == "console":
                    print(ev["params"]["args"])
        """
        params = {"target_id": target_id}
        if types:
            params["types"] = ",".join(types)
        if self.token:
            params["token"] = self.token
        from urllib.parse import urlencode
        url = self.url.rsplit("/rpc", 1)[0] + "/events?" + urlencode(params)
        req = urllib.request.Request(url, method="GET")
        if self.token:
            req.add_header("Authorization", f"Bearer {self.token}")
        resp = urllib.request.urlopen(req, timeout=timeout_s)
        try:
            event_name: Optional[str] = None
            data_buf: list[str] = []
            while True:
                line = resp.readline()
                if not line:
                    return
                s = line.decode("utf-8").rstrip("\r\n")
                if s == "":
                    # Dispatch buffered event.
                    if event_name is not None and data_buf:
                        try:
                            payload = json.loads("".join(data_buf))
                        except json.JSONDecodeError:
                            payload = {"raw": "".join(data_buf)}
                        payload["event"] = event_name
                        yield payload
                    event_name = None
                    data_buf = []
                    continue
                if s.startswith(":"):
                    continue  # heartbeat / comment
                if s.startswith("event: "):
                    event_name = s[len("event: "):]
                elif s.startswith("data: "):
                    data_buf.append(s[len("data: "):])
        finally:
            resp.close()


def _load_actions_bundle(explicit: Optional[str]) -> str:
    """Locate netra-actions.js. Search order:
    1. Explicit `path=` arg
    2. NETRA_ACTIONS_JS env var
    3. ../../js/netra-actions.js relative to this package (in-repo dev layout)
    """
    candidates: list[str] = []
    if explicit:
        candidates.append(explicit)
    if os.environ.get("NETRA_ACTIONS_JS"):
        candidates.append(os.environ["NETRA_ACTIONS_JS"])
    here = os.path.dirname(os.path.abspath(__file__))
    candidates.append(os.path.normpath(os.path.join(here, "..", "..", "js", "netra-actions.js")))

    for c in candidates:
        if c and os.path.isfile(c):
            with open(c, "r", encoding="utf-8") as f:
                return f.read()
    raise FileNotFoundError(
        "netra-actions.js not found. Tried: " + ", ".join(c for c in candidates if c)
    )
