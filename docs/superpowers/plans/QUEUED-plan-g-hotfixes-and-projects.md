# Plan G — Hotfixes + Project Groups + Diagnose (QUEUED)

> **Status:** queued, not started. Ground-truth scope below for whoever picks this up next.
> **Source of issues:** field integration testing with a third-party MCP agent during 2026-05-02 (see [`docs/integrations/v1-field-learnings.md`](../../integrations/v1-field-learnings.md)) plus follow-up on agent recovery loops.
> **Builds on:** `plan-f-release` tag.

## In scope

### 1. Three production bugs (must fix)

- **`browser_navigate` no-op without active target.** `internal/mcp/tools/browser_nav.go` falls back to `sess.ActiveTarget()` and returns `{ok:true, url:None}` when stale. Validate target exists in `browser_list_tabs` result before dispatching; return `mcp.ErrInvalidArgs` with `"no active target"` otherwise.
- **`browser_eval` inconsistent return shape.** `internal/browser/eval.go` falls back to `string(resp.Result.Value)` when JSON unmarshal fails, mixing decoded values and JSON-strings. Always decode; return `nil` (and surface error) if undecodable.
- **`meta_attach` false-positive.** `cdp.Attach` calls `profile.Discover` (HTTP `/json/version`) and `cdp.Dial` (WS open) but never round-trips a CDP method. Add `Browser.getVersion` after Dial; fail attach if it errors. Reflect actual liveness in `meta_health.chrome_alive`/`ws_alive`.

### 2. Build hygiene

- Add `Makefile` with `build`, `test`, `e2e`, `clean`, `install` targets.
- Confirm `.gitignore` covers `/netra-browser` (it does); ensure no committed binary creeps back in.
- README "Build from source" section: `make build` → `./netra-browser --version`.

### 3. Project groups (the multi-tab/multi-project feature)

**Why:** running multiple bug-bounty engagements against one real Chrome causes tab collision. Each agent should only see its own tabs.

**Design:**
- New flag `--project <name>` (default: auto-gen short ID like `proj-3a7f`).
- Sidecar JSON at `~/.config/netra-browser/projects/<name>.json` holds `{name, owner_pid, owned_target_ids: [...], created_at}`.
- `browser_new_tab` auto-tags the new target into the current project.
- `browser_list_tabs` filters to current project by default; `{include_all: true}` returns everything.
- New tools:
  - `browser_adopt_tab {target_id}` — claim a pre-existing tab into the current project.
  - `browser_release_tab {target_id}` — un-claim (becomes orphan, visible to all).
  - `browser_list_projects` — for diagnostics.
- Stale projects (owner pid not alive) are cleaned up on bridge startup, same pattern as `profile.Lock`.
- Multiple bridges can run concurrently against one Chrome — each `--lock <unique-path>` and `--project <unique-name>`.

**Out of scope for v1 of this feature:** Chrome's native tab-groups visual indication (browser tab-bar coloring). Defer to v2 — adds CDP `Target.setAutoAttach` + `chrome.tabGroups` complexity, sidecar-only is enough for isolation.

### 4. `browser_diagnose` composite tool

Bundles the recovery diagnostic chain into one MCP call so agents don't burn 5 round-trips per "is this stuck" check.

**Args:** `{target_id?, recent_events_window_ms?: int (default 5000)}`
**Returns:** `{ok, chrome_alive, ws_alive, target_exists, screenshot_png_base64, snapshot, recent_events: [{event, at_ms, params}]}`

Should be implementable by composing existing methods — no new CDP calls.

### 5. Coordinate-based interaction tools (canvas/SVG/drag-drop escape hatch)

Three tools that bypass locator resolution entirely and operate on raw viewport pixels. Reuse the same `Input.dispatchMouseEvent` path that `browser_click` already invokes internally.

- `browser_click_at {x: number, y: number, target_id?, button?: "left"|"right"|"middle" (default "left"), click_count?: int (default 1)}` → `{ok}`
- `browser_hover_at {x: number, y: number, target_id?}` → `{ok}`
- `browser_drag {from: {x,y}, to: {x,y}, target_id?, button?: "left"|"middle" (default "left"), steps?: int (default 10)}` → `{ok}` (interpolated mouseMoved events between from and to so the drag registers in apps that listen to drag events)

Document explicitly in `docs/tools.md`: these are the escape hatch for canvas, SVG, games, drag-drop, and screenshot-driven coordinate clicks. The accessibility/snapshot_id locators remain the recommended default for everything else because they survive viewport/DOM changes.

### 6. Speed-default flags (server-side)

Reduce default cost per call so agents don't have to remember to override:

- `--default-wait-until <load|domcontentloaded|networkidle>` (default `domcontentloaded`, was `load`).
- `--default-call-timeout-ms <int>` (default 5000, was 30000) — applied to `browser_wait_for`, `browser_navigate` event waits.
- `--snapshot-prune-aggressive` flag: when set, `browser_snapshot` strips `WebArea`/`generic`/`group` containers with no name AND no children's names, halving typical snapshot size.

These are additive — explicit per-call args still win. Default change is one-line in `internal/browser/navigate.go` and `events.go`.

### 7. Network response/request bodies in events

Currently `browser_get_recent_events` exposes only network METADATA (URL, method, headers, status). Bodies require the agent to round-trip through `browser_eval` or set up its own observation. CDP supports `Network.getResponseBody` and `Network.getRequestPostData` after enabling the Network domain.

**New args on `browser_get_recent_events`:** `{include_bodies?: bool, body_max_size?: int (default 64KB)}`. When set, augment each `network_request`/`network_response` event with `request_body?` and `response_body?` fields. Bodies larger than `body_max_size` truncated with a `truncated: true` marker.

**Why:** unblocks agents doing API discovery, integration testing, debugging, data extraction, or any workflow where they need to see what the page actually fetched/sent. Today most agents work around this by running parallel `curl` probes — wasteful and stateless.

**Cost:** size-limited and opt-in, so default behavior unchanged.

### 8. Action-diff helper (`task_action_diff`)

Composite tool that takes one tool call as an argument, snapshots state before, executes, snapshots after, and returns the diff. Removes the "what just happened?" reasoning turn agents currently need after each significant action.

**Args:** `{action: {tool: string, args: object}, target_id?, capture?: ["url","cookies","console","network","dom_summary"]}`
**Returns:**
```json
{
  "ok": true,
  "url_changed": true,
  "url_before": "...", "url_after": "...",
  "new_cookies": [...],
  "removed_cookies": [...],
  "new_console_messages": [...],
  "new_network_requests": [...],
  "dom_summary_changed": true
}
```

`dom_summary` defaults to a hash of the snapshot tree's role/name pairs — cheap to compute, change-sensitive, doesn't need to ship the tree.

### 9. Workflow recipe save/replay

Two new tools that turn a successful interaction sequence into a deterministic, replayable JSON recipe. The first time the agent figures out a flow (e.g. consent banner → login → MFA prompt → dashboard), `task_record_recipe` captures the action sequence. On subsequent runs, `task_replay_recipe` executes the saved JSON without the agent having to rediscover.

**Tools:**
- `task_record_recipe {name, actions: [{tool, args}], success_marker?: {locator | url_pattern | text}, target_id?}` → `{ok, recipe_path}` — saves to `~/.config/netra-browser/recipes/<name>.json`.
- `task_replay_recipe {name, target_id?, env?: {EMAIL: "...", PWD: "..."}}` → `{ok, steps_executed, last_step_result, success_verified: bool}` — variable substitution in `args` values via `$VAR` syntax.
- `task_list_recipes` → `{recipes: [{name, target_pattern?, last_used_at, step_count}]}`.

**Recipe format example:**
```json
{
  "name": "example-com-login",
  "target_pattern": "*.example.com",
  "actions": [
    {"tool": "browser_navigate", "args": {"url": "https://example.com/login", "wait_until": "load"}},
    {"tool": "browser_click", "args": {"locator": {"role": "button", "name": "Accept cookies"}}},
    {"tool": "browser_fill",  "args": {"locator": {"css": "#email"}, "value": "$EMAIL"}},
    {"tool": "browser_click", "args": {"locator": {"role": "button", "name": "Continue"}}},
    {"tool": "browser_fill",  "args": {"locator": {"css": "#password"}, "value": "$PWD"}},
    {"tool": "browser_click", "args": {"locator": {"role": "button", "name": "Sign in"}}}
  ],
  "success_marker": {"locator": {"text": "Welcome back"}},
  "created_at": "2026-05-02T...",
  "first_succeeded_at": "2026-05-02T..."
}
```

**Why:** saves agents the cost of rediscovering UI flows on every run. The recipe replays in seconds; the discovery loop takes minutes. Recipes are portable — share them with other agents or commit them next to your test suite.

**Out of scope here:** auto-recording every action transparently. Plan H may add an `--auto-record <recipe-name>` flag that wraps every tool call into a recipe. v1 of the feature requires explicit `task_record_recipe` so the agent decides what's worth saving.

### 5. Integration prompts addendum

Update the integration-prompt artifacts in `docs/integrations/` (create as part of this plan) with:
- The `--project <name>` convention.
- The recovery / live-adaptation / speed policies (already drafted in `docs/integrations/v1-field-learnings.md`).
- New tools' surface.

## Tasks (estimated 16-18)

1. Fix `browser_navigate` no-active-target check + tests.
2. Fix `browser_eval` return shape + tests.
3. Fix `meta_attach` false-positive (round-trip Browser.getVersion) + tests.
4. Makefile + README "Build from source" section.
5. Project sidecar package (`internal/profile/project.go`) + tests.
6. Wire project filtering into Session + browser_list_tabs/new_tab; add adopt/release/list_projects tools + tests.
7. Implement `browser_diagnose` composite tool + test.
8. Implement coordinate tools: `browser_click_at`, `browser_hover_at`, `browser_drag` + tests.
9. Add speed-default flags (`--default-wait-until`, `--default-call-timeout-ms`, `--snapshot-prune-aggressive`) + tests.
10. Add `include_bodies` to `browser_get_recent_events` (response/request body capture) + tests.
11. Implement `task_action_diff` composite + tests.
12. Implement `task_record_recipe` / `task_replay_recipe` / `task_list_recipes` + tests.
13. Update e2e tests to exercise project isolation (two bridges, one Chrome, no cross-talk).
14. Update e2e tests to exercise recipe round-trip (record → replay → verify).
15. Update README + integration prompts (RECOVERY POLICY + LIVE ADAPTATION POLICY + SPEED POLICY all documented in `docs/integrations/`).
16. Verify + tag `plan-g-hotfixes-projects`.

## Items deferred to Plan H (companion ecosystem)

These belong in [Plan H](QUEUED-plan-h-companion-ecosystem.md) — they're sidecar tools that complement the bridge but don't change its core.

- Local page-state classifier (small ViT/CLIP returning `{state}` so agents skip a reasoning turn on routine pages).
- Concurrent multi-tab fan-out helper.
- Visual regression diff (perceptual hash).
- OCR fallback for canvas / image-heavy pages.
- Bundled JS primitives library (DOM helpers, table extraction, scroll utilities).
- Streaming MCP notifications (`/events` SSE stream — currently 204 stub).
- localStorage/sessionStorage in `task_save_session`.

## Items deferred to a future Plan I or later

- JS handle retention across calls (`Runtime.releaseObject` lifecycle).
- Per-tab cookie filtering server-side.
- Real `task_run_with_proxy` (separate Chrome instance with `--proxy-server`).
- Graceful `meta_detach` after `--launch` (currently kills Chrome process; should only kill if WE launched it).
- A `browser_assert` tool that fails loudly with structured error when the page doesn't match an expected condition.
