# netra-browser

> **Bring your own Chrome — the missing MCP bridge for AI agents that need a real, logged-in browser.**

`netra-browser` is a single-binary Go bridge that connects AI agents (Claude Code, Claude Desktop, Cursor, Gemini, anything speaking [MCP](https://modelcontextprotocol.io)) to your **real Chrome** — the one with your cookies, MFA, corporate proxy, and installed extensions. **43 MCP tools** for navigation, snapshotting, interaction, network capture and blocking, viewport / device / geolocation / offline emulation, Web Vitals, Chrome traces, file uploads, dialogs, screenshots, JS evaluation, cookie management, HAR capture, session persistence, and **per-agent tab groups so multiple Claude agents can drive separate tabs of one Chrome concurrently without clobbering each other** — all over a single attached or launched Chrome instance.

[![tests](https://img.shields.io/badge/tests-passing-success)](https://github.com/mr-white-hat/netra-browser/actions) [![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE) [![go](https://img.shields.io/badge/go-1.22+-00ADD8)](go.mod)

---

## Why this exists

Most browser-automation tools (Playwright, Puppeteer, Selenium, Browserbase) launch fresh, isolated Chrome instances. That's the right model for hermetic CI tests. It's the **wrong** model for everything else an AI agent might want to do with a browser:

- **Authenticated apps:** every fresh launch re-fires MFA, captcha, device-trust prompts. After three runs your account is rate-limited or locked.
- **Corporate networks:** proxies, VPNs, and ZTNA agents that work in your real Chrome don't reliably attach to a Playwright-spawned context.
- **Extensions:** password managers, MFA helpers, ad-blockers, screenshot tools, or browser-side AI assistants exist in your real profile and are absent from a fresh one.
- **Stateful workflows:** drag-drop a file, scroll halfway down, fill three fields, then ask the AI to take over — fresh-launch tools start at zero.
- **Stable fingerprints:** sites that fingerprint browsers flag a fresh Playwright instance as a bot. Your daily Chrome doesn't get flagged.

`netra-browser` solves all five by **attaching to (or launching) your actual Chrome** via the [Chrome DevTools Protocol](https://chromedevtools.github.io/devtools-protocol/), then exposing 43 high-level tools over MCP that any modern AI agent can drive.

---

## What's special

- **Bring your own Chrome.** Attach to a Chrome already running on `--remote-debugging-port=9222`. Or let `netra-browser` launch one with your real `~/.config/google-chrome` profile. Your cookies, MFA, extensions all preserved.
- **Single static Go binary.** ~10MB. No Node runtime, no Python deps, no headless-Chromium download. Drop on Linux/macOS/Windows arm64/amd64.
- **Token-economical.** Snapshots are opt-in on interaction tools — pass `return_snapshot: true` only when you actually need post-action state. Most tools default to a 1-line `{ok}` response.
- **Five-strategy locator system.** `{role, name}` (accessibility-tree, the recommended default), `{text}`, `{snapshot_id}` (stable IDs from a prior `browser_snapshot`), `{css}`, or `{xpath}` — all interchangeable, picked per call.
- **Two transports, same tool surface.** stdio for local MCP clients (Claude Desktop, Claude Code), HTTP-SSE for remote / multi-client / Docker.
- **Session persistence built in.** `task_save_session` exports browser-wide cookies; `task_load_session` restores them into a fresh Chrome. No more re-MFA loops.
- **First-class network and event observability.** `browser_get_recent_events` exposes the network log, console output, and dialog events. `task_capture_har` produces standard HAR 1.2 for offline analysis.
- **43 tools across 4 namespaces.** `meta_*` (3 tools, lifecycle), `browser_*` (33 tools, page-level — including per-agent tab groups), `task_*` (7 tools, high-level workflows). Comprehensive enough that you rarely have to drop into raw `browser_eval`.
- **Friendly tab IDs.** `browser_list_tabs` and `browser_new_tab` return a short `tab_id` (`t1`, `t2`, …) alongside the raw 32-char `target_id`. Every tool accepts either.
- **Honest test coverage.** 7 end-to-end tests against real headless Chromium prove the bridge actually drives a browser — not just unit tests against a mock.

---

## Install

**Homebrew (macOS, Linux):**
```bash
brew install mr-white-hat/netra-browser/netra-browser
```

**Go install:**
```bash
go install github.com/mr-white-hat/netra-browser/cmd/netra-browser@latest
```

**Docker** (HTTP-SSE + headless chromium baked in):
```bash
docker run --rm -p 7878:7878 ghcr.io/mr-white-hat/netra-browser:latest --listen 0.0.0.0:7878 --token YOUR_TOKEN
```

**Build from source:**
```bash
git clone https://github.com/mr-white-hat/netra-browser
cd netra-browser
make build              # → ./netra-browser
./netra-browser --version
```

Other Make targets: `make test` (unit), `make e2e` (e2e, needs Chrome), `make lint` (vet + gofmt check), `make install` (copies binary to `$PREFIX/bin`, default `/usr/local/bin`), `make clean`.

**Direct download:** see [Releases](https://github.com/mr-white-hat/netra-browser/releases).

---

## Quickstart — Claude Desktop

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "netra-browser": {
      "command": "netra-browser"
    }
  }
}
```

Open Chrome with the debug port:
```bash
google-chrome --remote-debugging-port=9222
```

Restart Claude Desktop. You'll have all 43 tools available. Try:

> Use `meta_attach`, then `browser_list_tabs`, and tell me what's open. Take a screenshot of the active tab.

---

## How it works

```
┌───────────────┐   stdio or HTTP    ┌──────────────────┐    CDP / WebSocket    ┌──────────┐
│   AI agent    │ ◄──── JSON-RPC ───►│  netra-browser   │ ◄─── --debug-port ──► │  Chrome  │
│ (Claude, etc) │                    │  (this binary)   │                       │ (yours)  │
└───────────────┘                    └──────────────────┘                       └──────────┘
```

A typical agent session:

**1. Start (or attach to) Chrome with the debug port open.**
Run Chrome yourself with `--remote-debugging-port=9222`, or pass `--launch` to let `netra-browser` spawn one for you using your real profile.

**2. Configure your MCP client to spawn `netra-browser`.**
Claude Desktop: the JSON above. Claude Code: `claude mcp add netra-browser -- netra-browser`. Any other MCP client: point it at the binary's stdio or its HTTP-SSE endpoint.

**3. Agent attaches.**
First call is `meta_attach`. After that, the agent has 43 tools to navigate, inspect, interact, and capture.

**4. Drive the page.**
Most flows: `browser_new_tab` → `browser_navigate` → `browser_snapshot` (accessibility tree with stable IDs) → `browser_click` / `browser_fill` (using `{role,name}` or `snapshot_id`) → repeat.

**5. Persist auth (optional).**
After login, `task_save_session {"name": "<label>"}`. Next run: `task_load_session {"name": "<label>"}` — no MFA re-entry.

**6. Capture artifacts (optional).**
`task_capture_har`, `task_render_pdf`, `browser_screenshot` all return file paths your agent can attach to a report.

### Two attach modes

| Mode | When to use | Flag |
|---|---|---|
| **Attach** (default) | You already have Chrome open with `--remote-debugging-port=9222` and want the agent to drive YOUR session. | `--debug-url http://127.0.0.1:9222` |
| **Launch** | You want `netra-browser` to spawn Chrome itself (CI, headless servers, fresh profile per run). | `--launch [--launch-headless] [--profile-dir PATH]` |

### Two transport modes

| Transport | When to use | Flag |
|---|---|---|
| **stdio** (default) | Local MCP clients (Claude Desktop, Claude Code, etc). One client per bridge instance. | (none) |
| **HTTP-SSE** | Remote agents, browsers, multiple clients, Docker. | `--listen 127.0.0.1:7878 [--token <T>]` |

Non-localhost listen requires `--token`; the bridge refuses to start without one.

---

## Features

### Lifecycle (`meta_*`)
| Tool | Purpose |
|---|---|
| `meta_attach` | Connect to a Chrome on a debug port |
| `meta_detach` | Cleanly drop the connection |
| `meta_health` | Liveness check (Chrome + WS, probed by `Browser.getVersion`) |

### Tab management (`browser_*`)
| Tool | Purpose |
|---|---|
| `browser_list_tabs` | Tabs in the current project (`{include_all: true}` for everything) |
| `browser_new_tab` | Open a new tab — auto-tagged into the current project |
| `browser_select_tab` | Make a tab active |
| `browser_close_tab` | Close a tab |
| `browser_adopt_tab` / `browser_release_tab` | Claim / un-claim a pre-existing tab into this project |
| `browser_list_projects` | Diagnostic — every project sidecar in the projects dir |
| `browser_create_group` | Open a fresh per-agent tab group (returns `group_id` like `g1`); opens a tab in it by default |
| `browser_list_groups` | Every live group, its owned tabs, and each group's active tab |
| `browser_close_group` | Tear down a group — releases its tabs by default, or closes them with `{close_tabs: true}` |

### Navigation
| Tool | Purpose |
|---|---|
| `browser_navigate` | Go to a URL with `wait_until: load \| domcontentloaded \| networkidle` |
| `browser_go_back` / `browser_go_forward` / `browser_reload` | History + reload |

### Inspection
| Tool | Purpose |
|---|---|
| `browser_snapshot` | Compact accessibility tree with stable `#aN` IDs (or `dom_text` mode) |
| `browser_screenshot` | PNG, optionally clipped to a locator or full-page |
| `browser_eval` | Run any JS, return the value |
| `browser_get_cookies` / `browser_set_cookies` | Cookie management |

### Interaction
All accept the locator union `{role, name} \| {text} \| {snapshot_id} \| {css} \| {xpath}`.

| Tool | Purpose |
|---|---|
| `browser_click` | Mouse click at element center |
| `browser_fill` | Focus + clear + type into an input |
| `browser_hover` | Mouse hover |
| `browser_select_option` | Set `<select>` values |
| `browser_press_key` | Synthesize a keyDown + keyUp |
| `browser_upload_file` | Set a file on a `<input type="file">` |
| `browser_click_at` / `browser_hover_at` / `browser_drag` | Coordinate-based escape hatch for canvas / SVG / drag-drop |
| `browser_drop_files` | Drag-drop file upload — auto-detects hidden file input, falls back to native CDP drag sequence |

### Events + dialogs
| Tool | Purpose |
|---|---|
| `browser_wait_for` | Block until a future event fires (with optional predicate) |
| `browser_get_recent_events` | Buffered events; `{include_bodies: true}` augments network events with payloads |
| `browser_handle_dialog` | Accept/dismiss `alert`/`confirm`/`prompt` |
| `browser_diagnose` | Composite "is anything wrong?" — health + tab check + screenshot + snapshot + recent events |

### Emulation
| Tool | Purpose |
|---|---|
| `browser_set_viewport` | `setDeviceMetricsOverride` (width, height, scale factor, mobile). Zero dimensions clear the override. |
| `browser_emulate_device` | Preset table: `iphone_14`, `iphone_se`, `pixel_8`, `ipad_pro`, `desktop_1080p`, `desktop_macbook` — applies viewport + UA in one call |
| `browser_list_device_presets` | Discover the preset names |
| `browser_set_user_agent` | `setUserAgentOverride` |
| `browser_set_geolocation` | `setGeolocationOverride` (zero-arg clears) |
| `browser_set_offline` | Toggle `Network.emulateNetworkConditions` to fail every request with `net::ERR_INTERNET_DISCONNECTED` |
| `browser_block_urls` | Block a list of wildcard URL patterns at the network layer (analytics, ads, third-party trackers); empty list clears |

### Performance
| Tool | Purpose |
|---|---|
| `browser_get_vitals` | Install a `PerformanceObserver` on first call and read Core Web Vitals — LCP, CLS, FCP, TTFB, INP. `wait_ms` lets observers accumulate (1500–3000 typical post-navigation). |

### High-level tasks (`task_*`)
| Tool | Purpose |
|---|---|
| `task_capture_har` | Build a HAR 1.2 file from a window of network activity |
| `task_capture_trace` | Record a Chrome `chrome://tracing` trace — Perfetto-shaped JSON, loadable in [Perfetto UI](https://ui.perfetto.dev) or Chrome DevTools Performance panel |
| `task_render_pdf` | Print page to PDF (Letter / A4) |
| `task_save_session` / `task_load_session` | Browser-wide cookie persistence by name |
| `task_wait_for_download` | Set download dir, optionally trigger an action, wait for completion |
| `task_run_with_proxy` | (v1 stub — see [docs/tools.md](docs/tools.md)) |
| `task_action_diff` | Snapshot before, run an action, snapshot after, return the diff (URL / cookies / console / network / DOM hash) |
| `task_record_recipe` / `task_replay_recipe` / `task_list_recipes` | Save an action sequence to JSON, replay it later with `$VAR` substitution, list saved recipes |

Full reference: [`docs/tools.md`](docs/tools.md).

---

## What's different

| | netra-browser | Playwright-MCP | Browserbase | Puppeteer |
|---|---|---|---|---|
| Drives YOUR logged-in Chrome | yes | no (fresh launch) | no (cloud) | no (fresh) |
| Cookies / MFA preserved across runs | yes | no | no | no |
| Works with installed extensions | yes | no | no | no |
| Routes through your existing proxy | yes | no | no | partial |
| Single-binary, no Node runtime | yes | no | no | no |
| Token-economical (opt-in snapshots) | yes | no (always-snapshot) | no | n/a |
| Native HAR capture | yes | partial | yes | partial |
| Session save/load built in | yes | no | partial | no |
| Five-strategy locator union | yes | partial | partial | css/xpath only |
| Multi-tab parallel | yes | yes | yes | yes |
| HTTP-SSE transport | yes | yes | yes | n/a |

---

## Use cases

`netra-browser` works for any AI-agent workflow that needs a real, stateful browser:

- **AI coding assistants browsing real apps** — Claude Code, Cursor, etc. inspecting your dashboard, dev console, GitHub, Notion while you work, with live cookies.
- **End-to-end QA** — drive a real checkout against your staging app with real auth, render a PDF receipt, attach to test reports.
- **RPA-style internal automation** — fill a form in your company's intranet that requires SSO, daily.
- **Customer-support automation** — operator-style "watch a screen, suggest the next action" workflows that share Chrome with the human.
- **Content scraping behind auth** — scrape your own dashboards, exports, account history without re-MFA each run.
- **Integration testing of OAuth/SSO flows** — start logged in once, run hundreds of API/UI tests without burning your token quota.
- **Recon / surface mapping** — when an agent needs to actually load JS-heavy SPAs to enumerate routes and components.
- **Bug bounty research** — drive authenticated targets through real flows, capture JWTs from network logs, replay sessions across days.
- **Browser-driven research** — agents reading documentation, copy-pasting between Notion + GitHub + Linear, with all your tabs in scope.

If you're an agent author whose users are tired of "log in again" loops, this tool is for you.

---

## Examples

- [Claude Desktop quickstart](examples/claude-desktop.md) — full config + first session
- [Authenticated scraping](examples/auth-scraping.md) — preserve MFA, scrape behind login
- [End-to-end QA](examples/e2e-qa.md) — drive a checkout, verify the result
- [Bug bounty workflow](examples/bug-bounty.md) — capture session, replay, scan

More integrations under [`docs/integrations/`](docs/integrations/).

---

## Configuration

Common flags:

```
--listen 127.0.0.1:7878         HTTP-SSE transport (default: stdio)
--token <TOKEN>                 required for non-localhost listen
--launch                        spawn Chrome ourselves
--profile-dir <PATH>            custom user-data-dir for launch mode
--profile-snapshot              copy profile to a temp dir before launching
--launch-headless               add --headless=new
--debug-url URL                 attach to running Chrome (default http://127.0.0.1:9222)
--lock <PATH>                   lock-file path (default ~/.config/netra-browser/active.lock)
--allow-origin <ORIGIN>         CORS allowlist for HTTP-SSE (repeatable)
--project <name>                tab-isolation project (default: auto-generated)
--projects-dir <PATH>           project sidecars (default ~/.config/netra-browser/projects)
--default-wait-until <mode>     server-side default for browser_navigate (default domcontentloaded)
--default-call-timeout-ms <ms>  default timeout for waiting tools (default 5000)
--snapshot-prune-aggressive     strip empty WebArea/generic/group nodes from snapshots
```

Sessions live at `~/.config/netra-browser/sessions/<name>.json`. Recipes live at `~/.config/netra-browser/recipes/<name>.json`.

### Multiple bridges, one Chrome

Each bridge gets its own `--project <name>` and `--lock <unique-path>`. `browser_list_tabs` then only shows that bridge's owned tabs by default; pass `{include_all: true}` to see everything Chrome has open. Adopt pre-existing tabs into your project with `browser_adopt_tab`.

### Multiple agents, one bridge — per-agent tab groups

When several Claude agents share **one** bridge (e.g. an orchestrator fanning out subagents, or multiple MCP clients on the same HTTP-SSE endpoint), they used to collide on a single shared "active tab": whichever agent navigated last moved every other agent's implicit target. **Tab groups** fix this — each agent gets a private lane with its own active tab and its own set of owned tabs.

Workflow per agent:

1. `browser_create_group` → returns a `group_id` (`g1`, `g2`, …) and, by default, opens a fresh tab already set as that group's active tab.
2. Pass that `group_id` on subsequent page calls (`browser_navigate`, `browser_click`, `browser_fill`, `browser_new_tab`, the coordinate tools, …). With a `group_id` set, omitting `target_id` resolves to **that group's** active tab — never a global one.
3. `browser_close_group` when done (releases the tabs, or closes them with `{close_tabs: true}`).

Isolation guarantees:
- A tab opened or first touched under a `group_id` is **owned** by that group.
- Acting on a tab owned by a *different* group returns `cross_group` — agents cannot drive each other's tabs.
- Opening a grouped tab never touches the legacy session-wide active target, so a grouped agent and a legacy (un-grouped) caller don't interfere.
- Calls with **no** `group_id` keep the original single-active-target behavior, so existing single-agent setups are unchanged.

This is cooperative isolation keyed by `group_id` (it works identically over stdio and HTTP-SSE). For hard, per-connection isolation, run a separate bridge per agent with distinct `--project`/`--lock` (see above).

---

## Roadmap

**Shipped**
- Core bridge: lifecycle, navigation, snapshot, interaction, eval, cookies, screenshots — Plans A–F.
- Hotfixes + project groups + `browser_diagnose` + speed defaults.
- Per-agent tab groups (`browser_create_group` / `browser_list_groups` / `browser_close_group`) — concurrent agents drive separate tabs of one Chrome without clobbering a shared active tab.
- Companion ecosystem: `netra-fanout` (Python concurrent multi-tab driver, [`python/`](python/)), `netra-actions` (JS primitives bundle, [`js/netra-actions.js`](js/netra-actions.js)), localStorage in `task_save_session` / `task_load_session`, SSE event streaming on `/events`.
- `browser_drop_files` — drag-drop file uploads (auto-detects hidden file input, falls back to native CDP drag sequence).
- Resource hygiene: `Subscribe` / `SubscribeOnTarget` return `(chan, cleanup)`, `Page.Close` tears down every collector goroutine, `browser_close_tab` reaps server-side state, `Target.targetDestroyed` reaps user-closed tabs, O(1) RingBuffer.
- Emulation suite (9 tools): viewport / device / UA / geo / offline emulation, Web Vitals, Chrome trace recording, URL blocking. Friendly tab IDs (`t1`, `t2`, …) surfaced alongside CDP target IDs.

**Next up / deferred**
- Full Fetch-domain request interception (mock/modify, not just block).
- Snapshot / screenshot diffing against a baseline.
- Encrypted state files for `task_save_session` (AES-GCM).
- React DevTools introspection.
- `netra-classifier`, `netra-watch`, `netra-ocr` companion tools.

Foundational milestones are tagged in git: `plan-a-foundation` → `plan-f-release`.

---

## Field learnings

Real bugs and usability gaps found integrating netra-browser with a third-party agent are documented neutrally in [`docs/integrations/v1-field-learnings.md`](docs/integrations/v1-field-learnings.md), including three deployable agent-prompt policies (recovery, live adaptation, speed) you can drop into any client to make it behave well against the bridge.

---

## Contributing

PRs and issues welcome. See [CONTRIBUTING.md](CONTRIBUTING.md). Areas where help is most useful:

- New examples in [`examples/`](examples/)
- Cross-platform launch testing (macOS, Windows)
- Companion-ecosystem sidecar tools (`netra-classifier`, `netra-watch`, `netra-ocr`) — most don't require touching the bridge

---

## License

MIT. See [LICENSE](LICENSE).

## Status

v0.x — actively developed. Core feature set complete and tagged; companion ecosystem, drag-drop uploads, resource-hygiene rewrite, and emulation suite all shipped. See [`docs/tools.md`](docs/tools.md) for the full tool reference.
