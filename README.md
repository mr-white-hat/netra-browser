# netra-browser

> **Bring your own Chrome — the missing MCP bridge for AI agents that need a real, logged-in browser.**

`netra-browser` is a single-binary Go bridge that connects AI agents (Claude Code, Claude Desktop, Trinetra, anything speaking [MCP](https://modelcontextprotocol.io)) to your **real Chrome** — the one with your cookies, MFA, Burp proxy, and installed extensions. It exposes 30+ MCP tools for navigation, snapshotting, interaction, network capture, and session persistence.

## Why?

Playwright and Puppeteer launch fresh, isolated Chrome instances. That's great for tests, terrible for agents that need to:

- Drive logged-in apps without re-doing MFA every run
- Inspect traffic through Burp / a corporate proxy
- Use installed extensions (1Password, Claude for Chrome, ad blockers)
- Hand off between human and agent without losing state

`netra-browser` attaches to (or launches) the user's actual Chrome via [CDP](https://chromedevtools.github.io/devtools-protocol/) and gives any MCP-capable agent the same access.

## Install

**Homebrew (macOS, Linux):**
```bash
brew install mr-white-hat/netra-browser/netra-browser
```

**Go install:**
```bash
go install github.com/mr-white-hat/netra-browser/cmd/netra-browser@latest
```

**Docker (HTTP-SSE + headless chromium baked in):**
```bash
docker run --rm -p 7878:7878 ghcr.io/mr-white-hat/netra-browser:latest --listen 0.0.0.0:7878 --token YOUR_TOKEN
```

**Direct download:** see [Releases](https://github.com/mr-white-hat/netra-browser/releases).

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

Now in Chrome, open it with the debug port:
```bash
google-chrome --remote-debugging-port=9222
```

Restart Claude Desktop. You'll have 30+ browser tools available. Try:

> Use `meta_attach`, then `browser_list_tabs`, and tell me what's open.

## How it works — end-to-end workflow

```
┌───────────────┐   stdio or HTTP    ┌──────────────────┐    CDP / WebSocket    ┌──────────┐
│   AI agent    │  ◄──── JSON-RPC ──►│  netra-browser   │ ◄─── --debug-port ──► │  Chrome  │
│ (Claude, etc) │                    │  (this binary)   │                       │ (yours)  │
└───────────────┘                    └──────────────────┘                       └──────────┘
```

A typical session:

**1. Start (or attach to) a Chrome with the debug port open.**
Either run Chrome yourself with `--remote-debugging-port=9222`, or pass `--launch` to let `netra-browser` spawn one for you.

**2. Configure your MCP client to spawn `netra-browser`.**
For Claude Desktop, the JSON above is enough. For Claude Code, add it via `claude mcp add netra-browser -- netra-browser`. For Trinetra or any other MCP client, point it at the binary's stdio (or its HTTP-SSE endpoint with `--listen 127.0.0.1:7878`).

**3. Agent attaches.**
The first tool call is always `meta_attach` (no args needed if Chrome is on the default port). After that, the agent can list tabs, open new ones, navigate, snapshot, click, fill, screenshot, eval, etc.

**4. Drive the page.**
Most flows look like: `browser_new_tab` → `browser_navigate` → `browser_snapshot` (to see the accessibility tree) → `browser_click`/`browser_fill` (using `{role,name}` or `snapshot_id` locators) → repeat. Snapshots are opt-in on interaction tools (`return_snapshot: true`) so the agent only pays for state when it asks.

**5. Persist auth (optional).**
After logging in once (manually or via the agent), call `task_save_session {"name": "<label>"}`. On the next run, `task_load_session {"name": "<label>"}` restores cookies into a fresh Chrome — no MFA re-entry.

**6. Capture artifacts (optional).**
`task_capture_har` (request log), `task_render_pdf` (printable copy), `browser_screenshot` (PNG) all return file paths your agent can attach to a report.

### Two attach modes

| Mode | When to use | Flag |
|---|---|---|
| **Attach** (default) | You already have Chrome open with `--remote-debugging-port=9222` and want the agent to drive YOUR session. | `--debug-url http://127.0.0.1:9222` |
| **Launch** | You want `netra-browser` to spawn Chrome itself (CI, headless servers, fresh profile per run). | `--launch [--launch-headless] [--profile-dir PATH]` |

### Two transport modes

| Transport | When to use | Flag |
|---|---|---|
| **stdio** (default) | Local MCP clients (Claude Desktop, Claude Code, Trinetra subprocess). One client per bridge. | (none) |
| **HTTP-SSE** | Remote agents, browsers, multiple clients, Docker. | `--listen 127.0.0.1:7878 [--token <T>]` |

Non-localhost listen requires `--token`; the bridge refuses to start without one.

## What's different

| | netra-browser | Playwright-MCP | Browserbase | Puppeteer |
|---|---|---|---|---|
| Drives YOUR logged-in Chrome | yes | no (fresh launch) | no (cloud) | no (fresh) |
| Cookies / MFA preserved | yes | no | no | no |
| Works with installed extensions | yes | no | no | no |
| Routes through your Burp proxy | yes | no | no | partial |
| Single-binary, no Node | yes | no | no | no |
| Token-economical (opt-in snapshots) | yes | no (always-snapshot) | no | n/a |
| Multi-tab parallel | yes | yes | yes | yes |
| HTTP-SSE transport | yes | yes | yes | n/a |

## Tool reference

See [`docs/tools.md`](docs/tools.md) for the full list of 30+ tools across `meta_*`, `browser_*`, and `task_*` namespaces.

## Examples

- [Claude Desktop quickstart](examples/claude-desktop.md) — full config + first session
- [Authenticated scraping](examples/auth-scraping.md) — preserve MFA, dump data behind login
- [Bug bounty workflow](examples/bug-bounty.md) — capture session, replay, scan
- [End-to-end QA](examples/e2e-qa.md) — drive a checkout flow, verify result

## Configuration

Common flags:

```
--listen 127.0.0.1:7878    HTTP-SSE transport (default: stdio)
--token <TOKEN>            required for non-localhost listen
--launch                   spawn Chrome ourselves
--profile-dir <PATH>       custom user-data-dir
--profile-snapshot         copy profile to temp dir before launching
--launch-headless          add --headless=new
--debug-url URL            attach to running Chrome (default http://127.0.0.1:9222)
--lock <PATH>              lock-file path
```

Sessions are stored at `~/.config/netra-browser/sessions/<name>.json`.

## License

MIT. See [LICENSE](LICENSE).

## Status

v0.x — actively developed. See [`docs/superpowers/specs/`](docs/superpowers/specs/) for design.
