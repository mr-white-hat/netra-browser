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
Args: none
Result: `{ok, tabs: [{target_id, url, title, active}]}`

### `browser_new_tab`
Args: `{url?}` (default `about:blank`)
Result: `{ok, target_id}`

### `browser_select_tab`
Args: `{target_id}`
Result: `{ok}`

### `browser_close_tab`
Args: `{target_id?}` (default: active)
Result: `{ok}`

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

## Events

Event names: `navigation` | `network_request` | `network_response` | `console` | `dialog` | `load` | `domcontentloaded`.

### `browser_wait_for`
Args: `{event, predicate?, timeout_ms?, target_id?}`
Result: `{ok, event, params}` or `{error_code:"timeout"}`

Predicate is a flat object of dotted-key paths to expected values, e.g. `{"frame.url": "https://example.com"}`.

**Note:** `wait_for` only catches events that fire AFTER the call subscribes. For events that may have already fired, use `browser_get_recent_events`.

### `browser_get_recent_events`
Args: `{since?: ms_since_epoch, types?: [string], target_id?}`
Result: `{ok, events: [{event, at_ms, params}]}`

### `browser_handle_dialog`
Args: `{action: "accept"|"dismiss", text?, target_id?}`
Result: `{ok}`

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

Exports browser-wide cookies via `Storage.getCookies` to `~/.config/netra-browser/sessions/<name>.json`. v1: cookies only; localStorage deferred.

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
