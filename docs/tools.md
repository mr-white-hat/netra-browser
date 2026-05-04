# netra-browser tool reference

All tools follow the MCP JSON-RPC 2.0 convention: request body
`{"jsonrpc":"2.0","id":N,"method":"<tool_name>","params":<args>}`, response
body `{"jsonrpc":"2.0","id":N,"result":<result>}`.

Tool errors return `{"ok":false,"error_code":"<stable_string>","message":"<human>"}` inside the result.

## Meta

### `meta_attach`
Args: `{debug_url?: string}` (default `http://127.0.0.1:9222`)
Result: `{ok, chrome_version, target_count}`

### `meta_detach`
Args: none
Result: `{ok}`

### `meta_health`
Args: none
Result: `{ok, chrome_alive, ws_alive, uptime_ms}`

## Targets / tabs

### `browser_list_tabs`
Args: `{include_all?: bool}`
Result: `{ok, tabs: [{target_id, url, title, active, owned?}]}`

When the bridge is running with a project (default: auto-generated `proj-<hex>`), the list is **filtered to tabs the project owns**. Pass `include_all: true` to see every Chrome tab regardless of project.

### `browser_new_tab`
Args: `{url?}` (default `about:blank`)
Result: `{ok, target_id}`
Auto-tagged into the bridge's current project.

### `browser_select_tab`
Args: `{target_id}`
Result: `{ok}`

### `browser_close_tab`
Args: `{target_id?}` (default: active)
Result: `{ok}`

### `browser_adopt_tab`
Args: `{target_id}`
Result: `{ok}`
Claim a pre-existing tab into the current project (so subsequent filtered `browser_list_tabs` shows it).

### `browser_release_tab`
Args: `{target_id}`
Result: `{ok}`
Stop owning the tab — it remains in Chrome but disappears from this project's filtered list.

### `browser_list_projects`
Args: none
Result: `{ok, active, projects: [{name, owner_pid, owned_target_ids, created_at, is_self}]}`
Diagnostic — lists every project sidecar in the projects directory.

## Navigation

### `browser_navigate`
Args: `{url, target_id?, wait_until?: "load"|"domcontentloaded"|"networkidle"}`
Result: `{ok, url, title, snapshot}` (snapshot always returned)

### `browser_go_back`, `browser_go_forward`, `browser_reload`
Args: `{target_id?, return_snapshot?}`
Result: `{ok, snapshot?}`

## Inspection

### `browser_snapshot`
Args: `{target_id?, mode?: "accessibility"|"dom_text"}` (default `accessibility`)
Result: `{ok, snapshot: <tree>}`

### `browser_screenshot`
Args: `{target_id?, locator?, full_page?}`
Result: `{ok, png_base64}`

### `browser_eval`
Args: `{expression, target_id?}`
Result: `{ok, result}`

### `browser_get_cookies`
Args: `{url_filter?: [string], target_id?}`
Result: `{ok, cookies: [...]}`

### `browser_set_cookies`
Args: `{cookies: [...], target_id?}`
Result: `{ok}`

## Interaction

All locators are: `{role, name, exact?} | {text, exact?} | {snapshot_id} | {css} | {xpath}`.

### `browser_click`
Args: `{locator, target_id?, return_snapshot?}`
Result: `{ok, snapshot?}`

### `browser_fill`
Args: `{locator, value, target_id?, return_snapshot?}`
Result: `{ok, snapshot?}`

### `browser_hover`
Args: `{locator, target_id?}`
Result: `{ok}`

### `browser_select_option`
Args: `{locator, values: [string], target_id?}`
Result: `{ok}`

### `browser_press_key`
Args: `{key, target_id?}`
Result: `{ok}`

### `browser_upload_file`
Args: `{locator, file_path, target_id?}`
Result: `{ok}`

### `browser_click_at` / `browser_hover_at` / `browser_drag` (escape hatch)

Coordinate-based interaction for canvas, SVG, games, drag-and-drop, screenshot-driven clicks. Bypass locator/box-model resolution entirely. **Use only when the accessibility/snapshot_id locators don't apply** — coordinate clicks are brittle to viewport, scroll, zoom, and dynamic layout.

- `browser_click_at`: `{x, y, target_id?, button?: "left"|"right"|"middle", click_count?}` → `{ok}`
- `browser_hover_at`: `{x, y, target_id?}` → `{ok}`
- `browser_drag`: `{from: {x,y}, to: {x,y}, target_id?, button?, steps?}` → `{ok}` (interpolates `mouseMoved` events between from/to so HTML5 dnd / canvas brushes / sliders register the gesture).

## Events

Event names: `navigation` | `network_request` | `network_response` | `console` | `dialog` | `load` | `domcontentloaded`.

### `browser_wait_for`
Args: `{event, predicate?, timeout_ms?, target_id?}`
Result: `{ok, event, params}` or `{error_code:"timeout"}`

Predicate is a flat object of dotted-key paths to expected values, e.g. `{"frame.url": "https://example.com"}`.

**Note:** `wait_for` only catches events that fire AFTER the call subscribes. For events that may have already fired, use `browser_get_recent_events`.

### `browser_get_recent_events`
Args: `{since?: ms_since_epoch, types?: [string], target_id?, include_bodies?: bool, body_max_size?: int}`
Result: `{ok, events: [{event, at_ms, params, body?, truncated?}]}`

When `include_bodies` is true, `network_request` and `network_response` events are augmented with their inline payload via `Network.getRequestPostData` / `Network.getResponseBody`. `body_max_size` defaults to 65536 (64 KB); larger bodies are truncated and marked `truncated: true`.

### `browser_handle_dialog`
Args: `{action: "accept"|"dismiss", text?, target_id?}`
Result: `{ok}`

### SSE event stream — `GET /events` (HTTP-SSE transport)

Plan H #6. Live tail of CDP events instead of polling `browser_get_recent_events`. Open with EventSource (browser) or any SSE client (curl, Python `urllib`, the netra-fanout `Bridge.subscribe_events()` helper).

- **Path:** `GET /events?target_id=<TID>&types=<comma-sep>` on the bridge's HTTP listener.
- **Auth:** `Authorization: Bearer <T>` for non-browser clients, or `?token=<T>` for EventSource which can't set headers.
- **Types:** any combination of `navigation`, `network_request`, `network_response`, `console`, `dialog`, `load`, `domcontentloaded`. Default = all.
- **Wire format:** SSE. First event is `event: ready` with `data: {"target_id":"..."}`. Subsequent events use the friendly type as `event:` and `data:` is `{"target_id":"...","at_ms":..., "params":{...CDP params...}}`. Heartbeat comment lines (`: ping`) every 20s keep the connection alive.
- **Backpressure:** slow consumers drop events silently (v0). Reconnect to resume the live tail; in-flight events during the gap are NOT replayed.

### `browser_diagnose`

Composite "is anything wrong?" tool — bundles `meta_health` + tab-existence check + screenshot + snapshot + recent events into one round trip.

Args: `{target_id?, recent_events_window_ms?: int}` (default window 5000)
Result: `{ok, chrome_alive, ws_alive, target_exists, target_id?, screenshot_png_base64?, snapshot?, recent_events?, error?}`

Sub-calls fail independently; you always get a partial result so the agent can decide what to do next.

## Tasks

### `task_capture_har`
Args: `{url?, duration_ms?, target_id?}`
Result: `{ok, har_path}`

If `url` is provided, navigates first; otherwise uses the current target.

### `task_render_pdf`
Args: `{url?, target_id?, format?: "Letter"|"A4"}`
Result: `{ok, pdf_path}`

### `task_save_session`
Args: `{name}`
Result: `{ok, session_path}`

Exports browser-wide cookies via `Storage.getCookies` to `~/.config/netra-browser/sessions/<name>.json`.

`task_save_session` also captures **localStorage** (Plan H #7) for every origin currently open in any tab — uses the `DOMStorage` CDP domain on each target's session. The session JSON gets a `local_storage` map keyed by origin (e.g. `"https://example.com": {"auth_token": "..."}`).
- **Args:** `{name, skip_local_storage?: bool}` (set `skip_local_storage` to keep behavior cookies-only).
- **Returns:** `{ok, session_path, local_storage_origins: [string]}`.

`task_load_session` applies cookies (always) and localStorage (per saved origin):
- **Args:** `{name, skip_navigation?: bool}`. If `skip_navigation` is false (default), the bridge auto-opens a tab on each saved origin so its LS can be applied. If true, only origins with an already-open tab get LS applied — useful when the origin's root URL redirects elsewhere.
- **Returns:** `{ok, local_storage_origins_applied: [string]}`. Origins where application failed (typically the origin's root redirected to a different host, breaking the frame-origin match `DOMStorage` requires) are silently skipped.

sessionStorage is NOT captured (it's per-tab, not per-origin — saving and replaying is fundamentally weird; deferred).

### `task_action_diff`

Snapshot state, run an action via the registry, snapshot state again, return the diff. Removes the "what just happened?" reasoning turn agents otherwise need after each significant action.

Args:
```json
{
  "action": {"tool": "browser_click", "args": {"locator": {...}}},
  "target_id": "...",
  "capture": ["url", "cookies", "console", "network", "dom_summary"]
}
```
Result:
```json
{
  "ok": true,
  "action_result": {...},
  "url_changed": true, "url_before": "...", "url_after": "...",
  "new_cookies": [...], "removed_cookies": [...],
  "new_console_messages": [...],
  "new_network_requests": [...],
  "dom_summary_changed": true, "dom_summary_before": "<hash>", "dom_summary_after": "<hash>"
}
```

`dom_summary` is a SHA-256 hash of the snapshot tree's role/name pairs — cheap and change-sensitive.

### `task_record_recipe` / `task_replay_recipe` / `task_list_recipes`

Capture an interaction sequence as a deterministic, replayable JSON file.

- **Record**: `{name, actions: [{tool, args}], success_marker?: {url_pattern|text}, target_pattern?}` → `{ok, recipe_path}`. Saves to `~/.config/netra-browser/recipes/<name>.json`.
- **Replay**: `{name, target_id?, env?: {VAR: "..."}}` → `{ok, steps_executed, last_step_result, success_verified}`. Substitutes `$VAR` in action args with values from `env`. Missing variables abort the replay.
- **List**: `{}` → `{ok, recipes: [{name, step_count, target_pattern?, last_used_at?, created_at}]}`.

### `task_load_session`
Args: `{name}`
Result: `{ok}`

### `task_wait_for_download`
Args: `{trigger_action?: {tool, args}, save_to, timeout_ms?, target_id?}`
Result: `{ok, file_path, size}` or `{error_code:"timeout"}`

If `trigger_action` is provided, the tool invokes it after registering the download listener. Files land at `<save_to>/<guid>`.

### `task_run_with_proxy`
Args: `{proxy_url, tool_calls: [{tool, args}]}`
Result: `{error_code:"not_implemented_v1"}`

v1 stub. Workaround: launch a separate `netra-browser` instance with `--launch` and add `--proxy-server=<url>` to its Chrome args.

## Error codes

`chrome_disconnected`, `chrome_dead`, `target_destroyed`, `timeout`, `ambiguous_locator`, `not_found`, `not_attached`, `invalid_args`, plus tool-specific codes (`not_implemented_v1`, etc.).
