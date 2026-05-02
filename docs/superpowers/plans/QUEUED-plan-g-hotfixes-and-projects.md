# Plan G — Hotfixes + Project Groups + Diagnose (QUEUED)

> **Status:** queued, not started. Ground-truth scope below for whoever picks this up next.
> **Source of issues:** Trinetra porting feedback from 2026-05-02 (`~/BugBounty/Tools/playwright/PORTING-REPORT-NETRA.md`) plus follow-up on agent recovery loops.
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

### 5. Trinetra prompt + porting report addendum

Update the prompt artifact (in this repo: `docs/integrations/trinetra-prompt.md` — create as part of this plan) with:
- The `--project <engagement>` convention.
- The recovery policy (see `docs/integrations/trinetra-recovery-policy.md`).
- New tools' surface.

## Tasks (estimated 8-10)

1. Fix `browser_navigate` no-active-target check + tests.
2. Fix `browser_eval` return shape + tests.
3. Fix `meta_attach` false-positive (round-trip Browser.getVersion) + tests.
4. Makefile + README "Build from source" section.
5. Project sidecar package (`internal/profile/project.go`) + tests.
6. Wire project filtering into Session + browser_list_tabs/new_tab; add adopt/release/list_projects tools + tests.
7. Implement `browser_diagnose` composite tool + test.
8. Update e2e tests to exercise project isolation (two bridges, one Chrome, no cross-talk).
9. Update README + integration prompts.
10. Verify + tag `plan-g-hotfixes-projects`.

## Wishlist captured from Trinetra feedback (DEFER — not Plan G)

- JS handle retention across calls (`Runtime.releaseObject` lifecycle).
- Network request bodies in event payloads (need `Network.enable` with `getResponseBody` follow-up).
- Per-tab cookie filtering server-side.
- localStorage/sessionStorage in `task_save_session`.
- Real `task_run_with_proxy` (separate Chrome instance with `--proxy-server`).
- Graceful `meta_detach` after `--launch` (currently kills Chrome process; should only kill if WE launched it).

These belong in a Plan H or get cherry-picked into Plan G if they fit the iteration.
