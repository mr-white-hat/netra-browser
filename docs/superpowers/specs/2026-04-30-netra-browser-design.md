---
title: netra-browser v1 design
status: approved
date: 2026-04-30
supersedes: BRAINSTORM-DRAFT.md
---

# netra-browser v1 — design spec

## Overview

`netra-browser` is a single-binary Go bridge that attaches to (or launches) a real Chrome via the Chrome DevTools Protocol and exposes browser primitives + high-level tasks over MCP. It targets AI agents (Claude Code, Claude Desktop, Cursor, Gemini, custom MCP clients) that need to drive the user's actual logged-in browser — cookies, MFA, corporate proxy, installed extensions intact.

**Tagline:** "Bring your own Chrome — the missing MCP bridge for AI agents that need a real, logged-in browser."

## Goals

- Drive a real, user-launched Chrome from any MCP-capable agent.
- Cross-platform: Linux, macOS, Windows on amd64 and arm64.
- Single static binary, no runtime dependencies.
- Token-economical: opt-in snapshots, server-side event buffering, no surprise streaming costs.
- Neutral codebase. Domain-specific workflows (bug bounty, QA, RPA, scraping) live in external agents and consume this over MCP.

## Non-goals (v1)

- Companion Chrome extension (deferred to v2 in a separate `netra-browser-extension` repo).
- Claude-for-Chrome integration (no public API; deferred indefinitely).
- Streaming MCP notifications. v1 uses request/response + `wait_for` + `get_recent_events`.
- Firefox/Safari/WebKit support.
- Homebrew-core submission. Tap only for v1.
- Auto-updater inside the binary.

## Architecture

```
cmd/netra-browser/main.go        # CLI entry; flags, mode select, signal handling
internal/cdp/                    # CDP websocket client; framing, request correlation, event fan-out
internal/browser/                # primitives: navigate/click/fill/eval/screenshot/cookies; locator resolution
internal/tasks/                  # high-level: capture_har, render_pdf, save/load_session, wait_for_download, run_with_proxy
internal/mcp/                    # JSON-RPC server; stdio + HTTP-SSE transports; tool registry
internal/profile/                # attach/launch, lock-file, session export/import
examples/{bug-bounty,auth-scraping,e2e-qa,claude-desktop}.md
.goreleaser.yaml
README.md
```

Each package has a single responsibility and is unit-testable in isolation. `cdp` knows nothing about MCP. `mcp` knows nothing about CDP method names. `browser` and `tasks` sit between them.

## Data flow

```
agent
  → MCP transport (stdio | HTTP-SSE)
  → tool registry (name → handler)
  → tasks/ or browser/ package
  → cdp.Client (websocket method call)
  → Chrome
```

Events return:

```
Chrome
  → cdp.Client (event dispatch)
  → per-target ring buffer (1000 events, drops oldest)
  → consumed by browser_get_recent_events / browser_wait_for
```

Concurrency: one cdp.Client per attached Chrome; each MCP session holds an `active_target_id`; tools take an optional `target_id` override.

## MCP tool surface

Tools are namespaced (`browser_*`, `task_*`, `meta_*`) to avoid collisions when an agent has multiple MCPs connected. Same set is exposed on stdio and HTTP-SSE.

### Targets / tabs

| Tool | Args | Returns |
|---|---|---|
| `browser_list_tabs` | — | `[{target_id, url, title, active}]` |
| `browser_new_tab` | `url?` | `{target_id}` |
| `browser_select_tab` | `target_id` | `{ok}` |
| `browser_close_tab` | `target_id?` | `{ok}` |

### Navigation

| Tool | Args | Returns |
|---|---|---|
| `browser_navigate` | `url, target_id?, wait_until?` | `{url, title, snapshot}` (snapshot always) |
| `browser_go_back` | `target_id?, return_snapshot?` | `{ok, snapshot?}` |
| `browser_go_forward` | `target_id?, return_snapshot?` | `{ok, snapshot?}` |
| `browser_reload` | `target_id?, return_snapshot?` | `{ok, snapshot?}` |

`wait_until` ∈ `"load"` (default — `Page.loadEventFired`) | `"domcontentloaded"` (`Page.domContentEventFired`) | `"networkidle"` (no in-flight network requests for 500ms).

### Interaction

All interaction tools accept a `locator` (see Locator system below).

| Tool | Args | Returns |
|---|---|---|
| `browser_click` | `locator, target_id?, return_snapshot?: false` | `{ok, snapshot?}` |
| `browser_fill` | `locator, value, target_id?, return_snapshot?: false` | `{ok, snapshot?}` |
| `browser_hover` | `locator, target_id?` | `{ok}` |
| `browser_select_option` | `locator, values[], target_id?` | `{ok}` |
| `browser_press_key` | `key, target_id?` | `{ok}` |
| `browser_upload_file` | `locator, file_path, target_id?` | `{ok}` |

### Inspection

| Tool | Args | Returns |
|---|---|---|
| `browser_snapshot` | `target_id?, mode?: "accessibility"|"dom_text"` | labeled tree |
| `browser_screenshot` | `target_id?, locator?, full_page?` | `{png_base64}` or `{path}` |
| `browser_eval` | `expression, target_id?` | `{result}` |
| `browser_get_cookies` | `url_filter?` | `[cookie...]` |
| `browser_set_cookies` | `cookies[]` | `{ok}` |

### Waiting

| Tool | Args | Returns |
|---|---|---|
| `browser_wait_for` | `event, predicate?, timeout_ms?, target_id?` | `{event...}` |
| `browser_get_recent_events` | `since?, types?, target_id?` | `[event...]` |

`event` ∈ `"navigation"` | `"network_request"` | `"console"` | `"dialog"` | `"download"` | `"selector"`.

### Dialogs

| Tool | Args | Returns |
|---|---|---|
| `browser_handle_dialog` | `action: "accept"|"dismiss", text?, target_id?` | `{ok}` |

### Tasks (high-level, all 5)

| Tool | Args | Returns |
|---|---|---|
| `task_capture_har` | `url, duration_ms?, target_id?` | `{har_path}` |
| `task_render_pdf` | `url?, target_id?, format?` | `{pdf_path}` |
| `task_save_session` | `name` | `{session_path, cookies_count}` |
| `task_load_session` | `name` | `{ok}` |
| `task_wait_for_download` | `trigger_action?, save_to?, timeout_ms?` | `{file_path, size}` |
| `task_run_with_proxy` | `proxy_url, tool_calls[]` | `{results[]}` |

**Task semantics:**
- `task_render_pdf`: if `url` is omitted, renders the active target's current page. If provided, opens a new tab, navigates, renders, closes the tab.
- `task_run_with_proxy`: executes the provided `tool_calls` array (each a `{tool, args}` MCP call) inside a fresh Chrome context with `--proxy-server=<proxy_url>`. Returns aligned results array. The proxy applies for the duration of the block only.
- `task_wait_for_download`: if `trigger_action` is provided (a `{tool, args}` shape), bridge invokes it then waits for the next download event. If omitted, simply waits for the next download.

### Meta

| Tool | Args | Returns |
|---|---|---|
| `meta_attach` | `debug_url?` | `{chrome_version, target_count}` |
| `meta_detach` | — | `{ok}` |
| `meta_health` | — | `{chrome_alive, ws_alive, uptime_ms}` |

## Locator system

A single `locator` parameter is a tagged union:

```
{role: "button", name: "Sign in", exact?: bool}    // accessibility, primary
{text: "Continue", exact?: bool}                    // text content
{snapshot_id: "#a3f"}                               // ID from prior browser_snapshot
{css: "button.login-submit"}                        // escape hatch
{xpath: "//button[@type='submit']"}                 // escape hatch
```

Resolution order: snapshot_id → role/name (`Accessibility.getFullAXTree`) → text (`DOM.querySelectorAll` with text match) → css → xpath. First match wins. Multiple matches return an error listing the candidates.

## Snapshot return policy

- Always returned: `browser_navigate`, `browser_go_back`, `browser_go_forward`, `browser_reload`.
- Opt-in (default false) via `return_snapshot: true`: `browser_click`, `browser_fill`, `browser_hover`, `browser_select_option`, `browser_press_key`, `browser_upload_file`.
- On-demand only: `browser_snapshot()`.

Snapshot format: compact accessibility tree, each node `{id, role, name, value?, children?}`. Nodes that aren't interactive and have no name are pruned.

## Session / profile management

### Modes

- **`attach`** — connect to a Chrome the user launched manually with `--remote-debugging-port=<port>`. Discovered via `http://127.0.0.1:<port>/json/version`.
- **`launch`** — bridge spawns Chrome with `--user-data-dir=$HOME/.config/google-chrome` (or platform equivalent) and a managed `--remote-debugging-port`.

### Lock file

`~/.config/netra-browser/active.lock` contains `{port, pid, started_at}`. A second bridge instance refuses to start unless `--force-reattach` is passed (which signals the existing instance to exit first). Stale locks (pid not alive) are auto-cleaned on startup.

### Sessions vs. profiles

`task_save_session(name)` exports cookies + localStorage + sessionStorage to `~/.config/netra-browser/sessions/<name>.json`. `task_load_session(name)` re-applies them to the current target's origin.

Sessions are NOT full profile clones. The full profile is only copied if the user passes `--profile-snapshot`, which clones to a temp dir before launching — useful for risky automation that shouldn't pollute the real profile.

## Error handling + reconnect

All tool errors return:

```json
{
  "ok": false,
  "error_code": "<stable_string>",
  "message": "<human readable>",
  "target_id": "<if relevant>"
}
```

### Failure modes

| Condition | Behavior |
|---|---|
| Websocket dropped | Bridge reconnects every 1s up to 30s. In-flight calls return `chrome_disconnected` with `reconnecting: true`. |
| Chrome process died | `meta_health.chrome_alive = false`. Subsequent calls fail fast with `chrome_dead`. Agent must call `meta_attach` (or new launch). |
| Target destroyed mid-call | Returns `target_destroyed`. If destroyed target was active, `active_target_id` becomes nil. |
| Per-call timeout | Default 30s, overridable per-call via `timeout_ms`. On timeout the bridge sends `Target.closeTarget` if the call originated one, otherwise abandons the request ID. Returns `timeout`. |
| Locator matches multiple | Returns `ambiguous_locator` with a list of candidate `{id, role, name}` so the agent can refine. |
| Locator matches none | Returns `not_found`. |

## Transport / auth

### stdio (default)

No auth. Process boundary IS the auth. Used by Claude Desktop, Claude Code, and any local MCP client subprocess.

### HTTP-SSE

- Default: bind `127.0.0.1:7878`, no auth required.
- Optional bearer token via `NETRA_BROWSER_TOKEN` env var. If set, all requests must include `Authorization: Bearer <token>`.
- **Non-localhost listen requires both `--listen 0.0.0.0:7878` AND `--token <required>`.** Bridge refuses to start non-localhost without a token.
- CORS: deny by default. Opt-in origins via `--allow-origin <origin>` (repeatable).

## Testing strategy

| Layer | Type | Approach |
|---|---|---|
| `internal/cdp/` | unit | Mock websocket; verify framing, request/response correlation, event dispatch. |
| `internal/profile/` | unit | Tempdir-based; verify lock-file lifecycle, session export/import. |
| `internal/browser/` | integration | Headless Chromium spawned per-test in CI. Tests real CDP behavior. |
| `internal/tasks/` | integration | Same harness as `browser`. Each task has at least one happy-path test. |
| `internal/mcp/` | golden-file | JSON-RPC request/response transcripts under `internal/mcp/testdata/`. Update with `-update-golden` flag. |
| End-to-end | smoke | Single test exercising stdio transport against a local fixture site (httptest server). |

Coverage targets: ≥70% on `internal/cdp` and `internal/mcp`. Integration tests cover `browser/` and `tasks/`.

CI: GitHub Actions matrix on `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`. Uses `chromedp/headless-shell` Docker image for Linux integration tests; native Chrome on macOS/Windows.

## Release / distribution

- **GoReleaser matrix:** `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/amd64`. Single static binary per target.
- **GitHub Releases:** tagged via `v*` push.
- **Homebrew tap:** `<user>/homebrew-netra-browser`, auto-updated by GoReleaser on release.
- **`go install`:** `go install github.com/<user>/netra-browser/cmd/netra-browser@latest` works.
- **Docker image:** `ghcr.io/<user>/netra-browser:<tag>` with HTTP-SSE transport + headless Chromium baked in. For headless server use cases.

## OSS positioning

README order:

1. Tagline + 60-second demo gif.
2. Install (one line per platform: brew, go install, docker, manual).
3. Claude Desktop quickstart (config snippet + `browser_navigate` example).
4. "BYOC: what's different" comparison table vs. Playwright, Puppeteer, Playwright-MCP, Browserbase.
5. Tool reference link (out to `docs/tools.md`).
6. Examples matrix linking to `examples/bug-bounty.md`, `auth-scraping.md`, `e2e-qa.md`, `claude-desktop.md`.

LICENSE: MIT. Slim CONTRIBUTING.md pointing at `good-first-issue` labels.

## Open items deferred to v1.x

These are intentionally out of v1 scope, captured here so we don't re-litigate:

- Streaming MCP notifications (`subscribe`/`unsubscribe`).
- Companion Chrome extension.
- Auto-updater.
- Firefox/WebKit support.
- Homebrew-core submission.
- Cookie/storage snapshot diffing.
