---
title: v1 field learnings
date: 2026-05-02
status: active
---

# Field learnings — porting a third-party agent to netra-browser v1

This document captures issues found while integrating netra-browser v1 with an existing agent that previously used Playwright. The findings apply to any AI agent that drives a real, logged-in browser — QA automation, e2e testing, customer-support workflows, content scraping, RPA, recon. It also documents the agent-prompt policies that worked.

For the shipped fixes, see [`docs/superpowers/plans/2026-04-30-netra-browser-plan-g-hotfixes-and-projects.md`](../superpowers/plans/2026-04-30-netra-browser-plan-g-hotfixes-and-projects.md). Per-bug "Status (Plan G, shipped)" callouts below.

---

## Bugs encountered in v1

### 1. Stale binary in repo root after dev cycle

**Symptom:** the `netra-browser` binary at the repo root prints `netra-browser: not yet implemented; see --version` instead of running.

**Root cause:** the binary was produced during Plan A Task 1 when `main.go` was just a `--version` stub. Subsequent task work used `go run ./cmd/netra-browser` or built to `/tmp`, never replacing the root-level artifact. `.gitignore` covers it for new clones, but anyone who ran `go build` once at the start has a stale 2.4MB stub instead of the 9.6MB real bridge.

**Workaround:**
```
go build -o netra-browser ./cmd/netra-browser
```

**Status (Plan G, shipped):** Plan G Task 4 — add `Makefile` with `make build` and document in README.

---

### 2. `browser_navigate` silent no-op without an active target

**Symptom:** `browser_navigate {url: "..."}` returns `{ok: true, url: null}` and does nothing visible. No error.

**Root cause:** `internal/mcp/tools/browser_nav.go` falls back to `sess.ActiveTarget()` when `target_id` is omitted. When `ActiveTarget` is empty (e.g. tab was closed, or was never set because `meta_attach` succeeded but the agent never opened a tab), the dispatch path still goes through with no target validation and the page-bound CDP call no-ops.

**Workaround:** always pass an explicit `target_id`. After `browser_new_tab`, capture the returned `target_id` and thread it through every subsequent call rather than relying on the active-target fallback.

**Status (Plan G, shipped):** Plan G Task 1 — validate the target exists in `browser_list_tabs` before dispatching; return `{error_code: "invalid_args", message: "no active target"}` otherwise.

---

### 3. `browser_eval` inconsistent return shape

**Symptom:** `browser_eval` sometimes returns the JS expression's value already decoded into a Go-native type (number, object, array), and sometimes returns a JSON-encoded string of that value. Agents have to detect and re-parse defensively.

**Root cause:** `internal/browser/eval.go` attempts `json.Unmarshal` on the CDP `result.value` field. If unmarshal fails (rare but possible for some object shapes), it falls back to `string(rawBytes)`. The two branches return different types for the same conceptual result.

**Workaround:** in client code, always attempt to JSON-decode the `result` field; if it's already decoded the decode is a no-op for primitives, idempotent for objects.

**Status (Plan G, shipped):** Plan G Task 2 — always decode and surface the unmarshal error explicitly. Return `nil` (with the error) if undecodable. Never return the raw string fallback.

---

### 4. `meta_attach` false-positive on dead Chrome

**Symptom:** `meta_attach {debug_url: "http://127.0.0.1:9222"}` returns `{ok: true}` even when no Chrome is listening on that port. Subsequent calls (`browser_new_tab`, `browser_list_tabs`, etc.) fail with confusing low-level CDP errors instead of an upfront "Chrome isn't there."

**Root cause:** `cdp.Attach` does two checks: `profile.Discover` (HTTP `GET /json/version`) and `cdp.Dial` (open the WebSocket). Both can succeed against a half-alive Chrome that won't actually respond to CDP method calls. The bridge never round-trips an actual CDP method to confirm the connection is live.

**Workaround:** after `meta_attach`, immediately call `meta_health` and check `chrome_alive` and `ws_alive`. If either is false, treat `meta_attach` as failed and abort.

**Status (Plan G, shipped):** Plan G Task 3 — after `cdp.Dial`, issue `Browser.getVersion` and only return success if it round-trips. Reflect actual liveness in `meta_health`.

---

## Usability gaps surfaced

### 5. Multiple agents running against one Chrome collide on tabs

**Symptom:** two agents driving the same physical Chrome (or one agent running multiple parallel projects) interfere — `browser_list_tabs` shows everyone's tabs, agents accidentally close or interact with each other's pages, lock files conflict.

**Root cause:** v1 has no concept of project ownership over tabs. The bridge sees Chrome's global tab list. The lock file prevents two bridges from owning the same `lock` path but doesn't isolate tab visibility once attached.

**Workaround:** use `--lock <unique-path>` per agent, and have each agent track the `target_id`s it created and only operate on those. Avoid `browser_list_tabs` for selection.

**Status (Plan G, shipped):** Plan G Tasks 5-6 — `--project <name>` flag (auto-generated short ID if omitted), sidecar JSON tracking owned `target_id`s, project-filtered `browser_list_tabs` (with `include_all: true` opt-out), new tools `browser_adopt_tab` / `browser_release_tab` / `browser_list_projects`.

---

### 6. Per-step latency dominated by agent reasoning, not the bridge

**Symptom:** agent loops spending 2-3 minutes per browser action. Felt like the bridge was slow.

**Root cause:** the bridge itself round-trips CDP in <100ms. The latency was in:

| Source | Cost | Fix |
|---|---|---|
| Model reasoning between tool calls | 8-30s | Use a smaller model for routine calls; reduce vision/screenshot input |
| `wait_until: "networkidle"` on chatty pages | 5-60s | Use `"domcontentloaded"` unless network quiet is required |
| Snapshot returned on every action | 2-10s of token-processing | Take ONE snapshot per page state, reuse `snapshot_id`s for many actions |
| Default 30s `wait_for` timeouts | up to 30s | Set explicit `timeout_ms: 5000` per call |
| Screenshots in the hot loop | 5-15s | Screenshot for diagnosis only, not as a checkpoint |

Realistic floor with an AI in the loop is **3-8s per step**. Below that, the model isn't reasoning about what it's doing.

**Status (Plan G, shipped):** Plan G Task 9 — server-side defaults that make every call cheaper without per-call args:
- `--default-wait-until` (default `domcontentloaded`, was `load`)
- `--default-call-timeout-ms` (default 5000, was 30000)
- `--snapshot-prune-aggressive` (strips empty `WebArea`/`generic`/`group` containers)

---

### 7. No coordinate-based interaction

**Symptom:** can't drive canvas-based UIs (drawing tools, charting libraries, games), drag-and-drop interactions on bare `<svg>` elements, or screenshot-driven coordinate clicks.

**Root cause:** v1 only exposes element-addressed interaction. Internally `browser_click` already calls `Input.dispatchMouseEvent` at computed pixel coordinates — there's just no tool that takes raw `(x, y)` directly.

**Workaround:** none for canvas. For DOM-addressable cases, escape via `browser_eval` to compute coordinates yourself and dispatch via the JS DOM event API.

**Status (Plan G, shipped):** Plan G Task 8 — add three coordinate tools that pass through to the existing CDP path:
- `browser_click_at {x, y, target_id?, button?, click_count?}`
- `browser_hover_at {x, y, target_id?}`
- `browser_drag {from: {x,y}, to: {x,y}, target_id?, button?, steps?}`

Document explicitly as the escape hatch — coordinate clicks are brittle to viewport, scroll, zoom, and dynamic layout changes. Default workflow stays accessibility/`snapshot_id` locators.

---

## Agent-prompt policies that worked

These three paragraphs proved out during the integration. Drop them into any agent that drives netra-browser. They're additive — recovery handles failures, live adaptation handles unexpected pages, speed handles per-step cost.

### RECOVERY POLICY — when a tool fails or hangs

```
Restart Chrome is the LAST resort, not the first. Restart loses session state, MFA, network history, recent navigation context. Use it only when meta_health proves Chrome is dead.

Diagnostic chain BEFORE deciding what to do:
  a. meta_health                                 — is Chrome alive?
  b. browser_list_tabs                           — does my target_id still exist?
  c. browser_screenshot {target_id}              — what does the user actually see right now?
  d. browser_snapshot {target_id, mode:"accessibility"}  — what's interactive?
  e. browser_get_recent_events {target_id, types:["dialog","navigation","console"]}  — what happened in the last few seconds?

Map diagnosis to action — DO NOT relaunch:
  - Dialog open (alert/confirm/prompt)? → browser_handle_dialog {action:"dismiss"} (CDP blocks on these; many "hangs" are this)
  - Page navigated away from where you expected? → browser_navigate back, or accept the new state
  - Locator stale (element gone)? → re-snapshot, find a new locator
  - Network call hanging? → check console events for CORS/CSP errors first
  - Auth wall appeared? → task_load_session if you have one saved; otherwise pause and report

Only if meta_health returns chrome_alive:false → THEN meta_detach + relaunch.
After ANY recovery action, re-run a-e to confirm the fix worked.
```

### LIVE ADAPTATION POLICY — when the page doesn't match the script

```
ALL browser state (cookies, current URL, scroll position, form contents, JS variables) PERSISTS across every MCP tool call. Taking a screenshot does NOT reset anything. ONLY closing/relaunching Chrome resets state. Never restart to "see what's there." Look while the page is still live.

When the script's next planned action doesn't fit what's on screen — captcha, extra form field, moved button, error toast, unfamiliar redirect — STOP the script and switch to live mode:

1. browser_screenshot {target_id} — what is actually on the page right now?
2. browser_snapshot {target_id, mode: "accessibility"} — which interactive elements exist? what are their snapshot_ids?
3. (Optional) browser_eval / browser_get_cookies / browser_get_recent_events for state the screen doesn't show.
4. Reason about it. The script said "click Continue" but the screen says "Verify your phone." That's not a failure — it's the real workflow. Decide:
   - Solvable inline (consent banner, cookie notice, confirmation modal)? → solve with browser_click/fill against the snapshot_ids you just got.
   - Needs a human (MFA, captcha, payment)? → pause, report what you see, ask. Don't loop.
   - Page actually broken (5xx in screenshot)? → wait + reload, NOT restart.
5. After your inline action, re-snapshot to confirm the page moved forward. Resume the original script from the next step or from a step adjusted to match the new state.
6. NEVER restart Chrome to "get back to a clean state."

Mid-flight adaptation > rigid scripts. The script is a starting plan. The accessibility snapshot is ground truth. When they disagree, trust the snapshot.
```

### SPEED POLICY — make every step cheap by default

```
1. Default wait_until is "domcontentloaded", NOT "load" or "networkidle". Use networkidle ONLY when you specifically need the page to stop fetching before reading state.

2. ONE snapshot per page state, not one per action. After browser_navigate (which always returns a snapshot), reuse the snapshot_ids for all subsequent clicks/fills. Re-snapshot ONLY when:
   - The page navigated (URL changed)
   - You took an action that mutated the DOM significantly (form submit, modal open)
   - The next action failed with not_found / ambiguous_locator

3. Pass return_snapshot: false (or omit it) on click/fill/hover. Don't re-fetch state you already have.

4. Set explicit short timeouts. Default browser_wait_for / browser_navigate timeout is 30s. For predictable flows, set timeout_ms: 5000 or even 3000. Fail fast.

5. Don't take screenshots in the hot loop. Screenshots are for diagnosis and final reporting — not for every step. The accessibility snapshot already gives you the interactive tree as text.

6. Use browser_eval for read-many-things-at-once instead of multiple snapshot/cookie/url calls. Example:
   JSON.stringify({url: location.href, title: document.title, forms: [...document.forms].map(f=>f.action)})
   is one call instead of three.

7. For routine sequenced steps (form-fill loops, pagination), prefer fewer model invocations: write a multi-action JS expression and run it via browser_eval, instead of issuing browser_fill 8 times.

If a step is taking >5s and Chrome looks fine in screenshots, the bottleneck is the wait_until or snapshot size. Tune those before reaching for anything else.
```

---

## Wishlist captured for v2 (not in Plan G)

These came up during the integration but don't fit the "fix what's broken + isolate projects" focus of Plan G:

- **JS handle retention across calls** — `Runtime.releaseObject` lifecycle so an agent can grab a DOM node once and reuse the handle.
- ~~**Network request bodies in event payloads**~~ — shipped in Plan G as `browser_get_recent_events {include_bodies: true}`.
- **Per-tab cookie filtering server-side** — `Network.getCookies` returns browser-wide; clients filter manually.
- **localStorage / sessionStorage in `task_save_session`** — v1 stores cookies only.
- **Real `task_run_with_proxy`** — currently a stub that points users at the manual workaround (separate bridge instance with `--proxy-server` in Chrome args).
- **Graceful `meta_detach` after `--launch`** — should only kill Chrome if WE launched it; currently kills unconditionally.
- **A `browser_assert` tool** — fail loudly with structured error when the page doesn't match an expected condition, so agents get a typed error to reason over instead of silent no-ops.

These belong in a Plan H or get cherry-picked into Plan G if they fit the iteration.
