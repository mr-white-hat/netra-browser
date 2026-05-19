# netra-actions — JS primitives for netra-browser agents

A versioned single-file JS bundle agents inject via `browser_eval`, exposing pre-baked DOM helpers under `window.__netra.*`.

## Why

Saves agents from rewriting common probes (table extraction, form auto-fill, framework detection, infinite-scroll handling, shadow-DOM walking). One inject per page state, then each helper is a one-call invocation.

## Helpers

| Function | Returns |
|---|---|
| `extractTable(selector)` | `{ok, headers, rows}` — each row is an object keyed by header text |
| `scrollToBottom({timeoutMs, settleMs})` | Promise → `{ok, final_height, elapsed_ms, timed_out?}` — scrolls until height stabilizes for `settleMs` or until `timeoutMs` |
| `formAutoFill({email, password, username, first_name, last_name, phone, address, city, zip, country})` | `{ok, filled: [{key, selector_hint, value_was}], skipped_count}` — heuristic field detection by type/name/id/autocomplete/placeholder/label |
| `detectFrameworks()` | `{ok, frameworks: {react, next, vue, nuxt, angular, svelte, jquery, htmx, alpine, gatsby, remix, solid}}` |
| `openShadowRoots()` | `{ok, count, roots: [{host_tag, host_id, text}]}` — walks every open shadow root (closed roots are unreachable from page-context JS) |

`window.__netra.version` returns the bundle version.

## Usage from Python

```python
from netra_fanout import Bridge

b = Bridge("http://127.0.0.1:7878", token="...")
b.attach()
tid = b.new_tab("https://example.com/dashboard")
b.inject_actions(tid)

rows = b.call("browser_eval", {
    "target_id": tid,
    "expression": "window.__netra.extractTable('table.results')",
})["result"]
```

## Usage from any MCP client

```jsonrpc
// 1. fetch the bundle text once (read js/netra-actions.js from disk or HTTP).
// 2. inject:
{"method":"browser_eval","params":{"target_id":"T-1","expression":"<bundle>"}}
// 3. invoke any helper:
{"method":"browser_eval","params":{"target_id":"T-1","expression":"window.__netra.detectFrameworks()"}}
```

Re-injecting replaces the namespace cleanly; safe to call once per page state.

## Versioning

Hand-bumped `VERSION` constant inside the bundle. Pin by checking `window.__netra.version` after injection. SemVer when the API stabilizes; pre-1.0 minor bumps may introduce breaking changes.

## Roadmap

- `viewportScreenshot` — deferred (use `browser_screenshot` via CDP for now)
- `extractList` for `<ul>` / `<ol>` / `<dl>`
- `waitForSelector` with mutation-observer-based detection
- esbuild + GitHub releases pipeline once the API is stable
- Recipe-runner integration: let `task_replay_recipe` reference `__netra.*` calls as first-class actions
