# netra-fanout

Concurrent multi-tab driver for the [netra-browser](../) MCP bridge. Open N tabs in one Chrome, run an independent task per tab in parallel, join the results.

This is **Plan H component #2** — sidecar tooling that lives outside the bridge core. It will eventually move to its own repo; for now it ships in the same repo for fast iteration.

## Install

```bash
pip install -e python/      # from the netra-browser repo root
```

No third-party dependencies — stdlib `urllib` only. Requires Python ≥ 3.9.

## Usage

Start the bridge in HTTP-SSE mode:

```bash
./netra-browser --listen 127.0.0.1:7878 --token YOUR_TOKEN --auto-attach
```

Then drive it from Python:

```python
from netra_fanout import Bridge, fan_out

b = Bridge("http://127.0.0.1:7878", token="YOUR_TOKEN")
b.attach()  # one-time meta_attach against the running Chrome

def run_check(bridge, target_id, label):
    bridge.navigate(target_id, f"https://example.com/{label}")
    snap = bridge.call("browser_snapshot", {"target_id": target_id})
    return snap

results = fan_out(b, [
    ("ticket-12", run_check),
    ("ticket-13", run_check),
    ("ticket-14", run_check),
], max_concurrency=4)

for r in results:
    print(r.label, "ok" if r.ok else f"fail: {r.error}")
```

## API

### `Bridge(url, token=None, timeout_s=30.0)`

JSON-RPC client over the bridge's HTTP-SSE transport. Thread-safe.

- `bridge.call(method, params=None, *, timeout_s=None)` — generic JSON-RPC call. Returns the `result` payload, raises `RPCError` on tool errors, `BridgeError` on transport failures.
- Sugar: `bridge.health()`, `bridge.attach(debug_url=None)`, `bridge.new_tab(url="about:blank") -> target_id`, `bridge.close_tab(target_id)`, `bridge.navigate(target_id, url, **opts)`.

### `fan_out(bridge, tasks, *, max_concurrency=4, open_url="about:blank", close_tabs=True)`

- `tasks`: iterable of `(label, fn)` pairs. `fn(bridge, target_id, label) -> Any`.
- Returns: list of `FanOutResult(label, target_id, ok, result, error)` in input order.
- One task failing does **not** abort the others — failures are recorded in the result row.

## How it composes with Plan G project isolation

When the bridge is started with `--project <name>`, every tab opened by `fan_out` (which uses `browser_new_tab` internally) is auto-tagged into that project. Multiple bridges, each with its own `--project`, each running their own `fan_out` against the same Chrome — no tab cross-talk.

## Development

```bash
cd python
pip install -e ".[dev]"   # or just: pip install pytest
pytest
```

Tests use an in-process HTTP fake of the bridge; no real Chrome needed.

## netra-actions JS bundle

Companion JS primitives library lives at [`../js/netra-actions.js`](../js/netra-actions.js). Inject it once per page, then call helpers via `browser_eval`:

```python
b.attach()
tid = b.new_tab("https://example.com/dashboard")
b.inject_actions(tid)  # reads ../js/netra-actions.js, injects via browser_eval
# Now window.__netra.{extractTable, scrollToBottom, formAutoFill, detectFrameworks, openShadowRoots}
rows = b.call("browser_eval", {"target_id": tid, "expression": "window.__netra.extractTable('table.results')"})["result"]
```

## Roadmap

- Async (`asyncio`) variant for callers already living in an event loop.
- `netra-fanout-node` — same surface for Node.js consumers (Plan H ships Python first).
- Built-in retry / circuit-breaker per task.
