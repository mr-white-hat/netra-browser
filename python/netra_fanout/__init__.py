"""netra-fanout — concurrent multi-tab driver for the netra-browser MCP bridge.

Open N tabs in one Chrome, run an independent task per tab in parallel, join.
Uses the bridge's HTTP-SSE transport.

Quick start:

    from netra_fanout import Bridge, fan_out

    b = Bridge("http://127.0.0.1:7878", token="...")

    def run_check(bridge, target_id, label):
        bridge.call("browser_navigate", {"target_id": target_id, "url": f"https://example.com/{label}"})
        return bridge.call("browser_screenshot", {"target_id": target_id})

    results = fan_out(b, [
        ("ticket-12", run_check),
        ("ticket-13", run_check),
        ("ticket-14", run_check),
    ], max_concurrency=4)
"""

from .bridge import Bridge, BridgeError, RPCError
from .fanout import fan_out, FanOutResult

__all__ = ["Bridge", "BridgeError", "RPCError", "fan_out", "FanOutResult"]
__version__ = "0.1.0"
