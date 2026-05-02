# netra-browser Plan F — Release Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development.

**Goal:** Add the release pipeline (GoReleaser config, Dockerfile), README with quickstart + comparison table, examples, LICENSE, CONTRIBUTING. After Plan F the project is shippable.

**Builds on:** `plan-e-high-level-tasks` tag.

---

## File Structure (Plan F)

| Path | Responsibility |
|---|---|
| `.goreleaser.yaml` | Multi-arch matrix; GitHub Releases; Homebrew tap |
| `Dockerfile` | Multi-stage build with chromium baked in for HTTP-SSE/headless server use |
| `LICENSE` | MIT |
| `README.md` (replace stub) | Tagline + install + quickstart + BYOC table + tool list + examples links |
| `CONTRIBUTING.md` | Slim guide pointing at issues |
| `docs/tools.md` | Generated tool reference (one section per tool) |
| `examples/bug-bounty.md` | Auth scraping / signup loop pattern |
| `examples/auth-scraping.md` | Capture session, replay, scrape behind login |
| `examples/e2e-qa.md` | QA test flow |
| `examples/claude-desktop.md` | Claude Desktop config snippet |
| `.github/workflows/ci.yml` | Run go test on push (linux runner) |

---

## Task 1: LICENSE + CONTRIBUTING

- [ ] **Step 1:** Create `LICENSE`:

```
MIT License

Copyright (c) 2026 Pavan Kumar

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

- [ ] **Step 2:** Create `CONTRIBUTING.md`:

```markdown
# Contributing to netra-browser

Issues and PRs welcome. Areas where help is most useful:

- New examples in `examples/`
- Cross-platform launch testing (macOS, Windows)
- HTTP-SSE streaming notifications (deferred from v1)
- Companion Chrome extension (planned for v2 in a separate repo)

Look for issues labeled `good-first-issue`.

## Development

- Go 1.22+ required
- Run `go test ./...` for unit tests
- Run `go test -tags e2e ./e2e/...` for end-to-end tests (requires `chromium` in PATH)

## Style

Follow `gofmt -l . | grep -v '^docs/'` (must be empty). Add tests for new behavior.
```

- [ ] **Step 3:** Commit

```bash
git add LICENSE CONTRIBUTING.md
git commit -m "release: add LICENSE (MIT) and CONTRIBUTING.md"
```

---

## Task 2: README

Replace the README stub with the full pitch.

- [ ] **Step 1:** Replace `README.md` with:

````markdown
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
brew install <user>/netra-browser/netra-browser
```

**Go install:**
```bash
go install github.com/<user>/netra-browser/cmd/netra-browser@latest
```

**Docker (HTTP-SSE + headless chromium baked in):**
```bash
docker run --rm -p 7878:7878 ghcr.io/<user>/netra-browser:latest --listen 0.0.0.0:7878 --token YOUR_TOKEN
```

**Direct download:** see [Releases](https://github.com/<user>/netra-browser/releases).

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

## What's different from \<X\>?

| | netra-browser | Playwright-MCP | Browserbase | Puppeteer |
|---|---|---|---|---|
| Drives YOUR logged-in Chrome | ✅ | ❌ (fresh launch) | ❌ (cloud) | ❌ (fresh) |
| Cookies / MFA preserved | ✅ | ❌ | ❌ | ❌ |
| Works with installed extensions | ✅ | ❌ | ❌ | ❌ |
| Routes through your Burp proxy | ✅ | ❌ | ❌ | partial |
| Single-binary, no Node | ✅ | ❌ | ❌ | ❌ |
| Token-economical (opt-in snapshots) | ✅ | ❌ (always-snapshot) | ❌ | n/a |
| Multi-tab parallel | ✅ | ✅ | ✅ | ✅ |
| HTTP-SSE transport | ✅ | ✅ | ✅ | n/a |

## Tool reference

See [`docs/tools.md`](docs/tools.md) for the full list of 30+ tools across `meta_*`, `browser_*`, and `task_*` namespaces.

## Examples

- [Bug bounty workflow](examples/bug-bounty.md) — capture session, replay, scrape behind login
- [Auth scraping](examples/auth-scraping.md) — preserve MFA, dump data
- [E2E QA flow](examples/e2e-qa.md) — drive a form, verify result
- [Claude Desktop](examples/claude-desktop.md) — full config + first session

## Configuration

Common flags:

```
--listen 127.0.0.1:7878    # HTTP-SSE transport (default: stdio)
--token <TOKEN>             # required for non-localhost listen
--launch                    # spawn Chrome ourselves
--profile-dir <PATH>        # custom user-data-dir
--profile-snapshot          # copy profile to temp dir before launching
--launch-headless           # add --headless=new
--debug-url URL             # attach to running Chrome (default http://127.0.0.1:9222)
--lock <PATH>               # lock-file path
```

Sessions are stored at `~/.config/netra-browser/sessions/<name>.json`.

## License

MIT. See [LICENSE](LICENSE).

## Status

v0.x — actively developed. See [docs/superpowers/specs/](docs/superpowers/specs/) for design.

````

NOTE: replace `<user>` with the actual GitHub username before publishing.

- [ ] **Step 2:** Commit

```bash
git add README.md
git commit -m "release: replace README stub with full pitch and quickstart"
```

---

## Task 3: docs/tools.md

A flat reference listing every tool, its args, and its return shape. Generate by inspecting the registrations.

- [ ] **Step 1:** Write `docs/tools.md`:

```markdown
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

### `task_render_pdf`
Args: `{url?, target_id?, format?: "Letter"|"A4"}`
Result: `{ok, pdf_path}`

### `task_save_session`
Args: `{name}`
Result: `{ok, session_path}`

### `task_load_session`
Args: `{name}`
Result: `{ok}`

### `task_wait_for_download`
Args: `{trigger_action?: {tool, args}, save_to, timeout_ms?, target_id?}`
Result: `{ok, file_path, size}` or `{error_code:"timeout"}`

### `task_run_with_proxy`
Args: `{proxy_url, tool_calls: [{tool, args}]}`
Result: `{error_code:"not_implemented_v1"}` — see README workaround.

## Error codes

`chrome_disconnected`, `chrome_dead`, `target_destroyed`, `timeout`, `ambiguous_locator`, `not_found`, `not_attached`, `invalid_args`, plus tool-specific codes.
```

- [ ] **Step 2:** Commit

```bash
git add docs/tools.md
git commit -m "docs: add tool reference"
```

---

## Task 4: Examples

Four short markdown files with copy-pasteable JSON-RPC sequences.

- [ ] **Step 1:** Create `examples/claude-desktop.md`:

```markdown
# Claude Desktop quickstart

## 1. Configure Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "netra-browser": {
      "command": "netra-browser"
    }
  }
}
```

## 2. Open Chrome with the debug port

```bash
google-chrome --remote-debugging-port=9222
```

(or use `--launch` mode and let `netra-browser` spawn it for you)

## 3. Restart Claude Desktop

The 30+ tools should appear in Claude's tool picker.

## 4. First session

Ask Claude:

> Use `meta_attach`, then `browser_list_tabs`, and summarize what's open.

If it works, you're set. From here you can drive any logged-in app.
```

- [ ] **Step 2:** Create `examples/auth-scraping.md`:

```markdown
# Authenticated scraping with session reuse

Use case: log in to a site once (with MFA), save the session, then have an agent scrape data behind the login on subsequent runs.

## Step 1: Manual login + save_session

Open Chrome, log in to the target site normally (entering MFA codes if required).

In your MCP client (Claude Desktop, Trinetra, etc.), invoke:

```jsonc
// 1. Attach to the running Chrome
meta_attach: {}

// 2. Save the current cookies under a name
task_save_session: {"name": "target-site"}
```

Output: `{"ok": true, "session_path": "/home/you/.config/netra-browser/sessions/target-site.json"}`

## Step 2: Headless replay later

In a new session (possibly from a fresh Chrome instance):

```jsonc
meta_attach: {"debug_url": "http://127.0.0.1:9222"}
task_load_session: {"name": "target-site"}

browser_new_tab: {"url": "https://target-site/dashboard"}
browser_navigate: {"url": "https://target-site/dashboard", "wait_until": "load"}

// You're now on an authenticated page.
browser_snapshot: {"mode": "accessibility"}
// Use the snapshot ids to extract data:
browser_eval: {"expression": "document.querySelector('.account-balance').textContent"}
```

## Notes

- v1 saves cookies only. localStorage / sessionStorage are not yet preserved.
- The session file is portable across Chrome instances on the same machine. Cross-machine use works if the target site doesn't bind cookies to a device fingerprint.
```

- [ ] **Step 3:** Create `examples/bug-bounty.md`:

```markdown
# Bug bounty workflow

Bug bounty agents need to drive *real* targets — production sites with MFA, anti-bot, browser fingerprinting. `netra-browser` is built for this.

## Pattern: signup loop with disposable accounts

```jsonc
// 1. Set up: launch Chrome with your Burp proxy
meta_attach: {}

// 2. For each iteration:
browser_new_tab: {"url": "https://target/signup"}
browser_navigate: {"url": "https://target/signup", "wait_until": "load"}
browser_fill: {"locator": {"css": "#email"}, "value": "user+0001@example.com"}
browser_fill: {"locator": {"css": "#password"}, "value": "AutoGen-Pw-0001"}
browser_click: {"locator": {"role": "button", "name": "Sign up"}}

// 3. Wait for confirmation email/page
browser_wait_for: {"event": "navigation", "timeout_ms": 10000}

// 4. Capture HAR for analysis
task_capture_har: {"duration_ms": 5000}
```

## Pattern: capture JWT after auth

```jsonc
browser_navigate: {"url": "https://target/login", "wait_until": "load"}
// (manual login if needed)
browser_get_recent_events: {"types": ["network_request"]}
// Filter for the request that contains the JWT in Authorization headers.
```

## Trinetra integration

Run `netra-browser --listen 127.0.0.1:7878 --token $TOKEN` and have Trinetra POST to `/rpc`. The bridge stays neutral — bug-bounty workflows live in Trinetra, this is the browser layer they consume.
```

- [ ] **Step 4:** Create `examples/e2e-qa.md`:

```markdown
# End-to-end QA flow

Use `netra-browser` from your CI to test that real-user flows work in the actual product.

## Sample: login + checkout

```jsonc
meta_attach: {}

// Load test user state
task_load_session: {"name": "qa-user"}

browser_new_tab: {"url": "https://shop.example/cart"}
browser_navigate: {"url": "https://shop.example/cart", "wait_until": "networkidle"}

browser_click: {"locator": {"role": "button", "name": "Checkout"}}
browser_fill: {"locator": {"role": "textbox", "name": "Card number"}, "value": "4242 4242 4242 4242"}
browser_click: {"locator": {"role": "button", "name": "Pay"}}

browser_wait_for: {"event": "navigation", "predicate": {"frame.url": "https://shop.example/order/confirmed"}, "timeout_ms": 10000}

task_render_pdf: {"target_id": "..."}
// Attach the PDF to your test report.
```

## CI integration

Run `netra-browser --launch --launch-headless --listen 127.0.0.1:0` in your CI job. The `--launch` mode spawns a fresh Chrome per job; `--profile-snapshot` keeps the source profile clean.
```

- [ ] **Step 5:** Commit

```bash
git add examples/
git commit -m "docs: add 4 example workflows (claude-desktop, auth-scraping, bug-bounty, e2e-qa)"
```

---

## Task 5: GoReleaser config

- [ ] **Step 1:** Create `.goreleaser.yaml`:

```yaml
version: 2

project_name: netra-browser

before:
  hooks:
    - go mod tidy
    - go test ./...

builds:
  - id: netra-browser
    main: ./cmd/netra-browser
    binary: netra-browser
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    ldflags:
      - -s -w -X main.Version={{.Version}}

archives:
  - id: default
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    formats: ["tar.gz"]
    format_overrides:
      - goos: windows
        formats: ["zip"]

checksum:
  name_template: "checksums.txt"

snapshot:
  name_template: "{{ incpatch .Version }}-next"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^chore:"
      - Merge pull request

brews:
  - name: netra-browser
    repository:
      owner: mr-white-hat
      name: homebrew-netra-browser
    homepage: "https://github.com/mr-white-hat/netra-browser"
    description: "MCP bridge for AI agents to drive a real, logged-in Chrome via CDP"
    license: "MIT"
    install: |
      bin.install "netra-browser"
    test: |
      system "#{bin}/netra-browser --version"

release:
  github:
    owner: mr-white-hat
    name: netra-browser
  draft: true
```

- [ ] **Step 2:** Smoke test:

```bash
# Verify the config parses (without releasing).
which goreleaser >/dev/null 2>&1 || go install github.com/goreleaser/goreleaser/v2@latest
goreleaser check
```

If `goreleaser` isn't in PATH, install it first.

- [ ] **Step 3:** Commit

```bash
git add .goreleaser.yaml
git commit -m "release: add GoReleaser config (multi-arch + Homebrew tap)"
```

---

## Task 6: Dockerfile + GitHub Actions CI

- [ ] **Step 1:** Create `Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1.5
FROM golang:1.22 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${VERSION}" -o /out/netra-browser ./cmd/netra-browser

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        chromium \
        ca-certificates \
        fonts-liberation \
        libasound2 \
        libnss3 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/netra-browser /usr/local/bin/netra-browser
ENV PATH="/usr/local/bin:${PATH}"
EXPOSE 7878
ENTRYPOINT ["/usr/local/bin/netra-browser"]
CMD ["--listen", "0.0.0.0:7878", "--launch", "--launch-headless"]
```

- [ ] **Step 2:** Create `.github/workflows/ci.yml`:

```yaml
name: CI
on:
  push:
    branches: [master, main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: sudo apt-get update && sudo apt-get install -y chromium
      - run: go test ./...
      - run: go test -tags e2e ./e2e/... -timeout 300s
      - run: go vet ./...

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: |
          test -z "$(gofmt -l . | grep -v '^docs/' | grep -v '^BRAINSTORM')"
```

- [ ] **Step 3:** Commit

```bash
git add Dockerfile .github/workflows/ci.yml
git commit -m "release: add Dockerfile + GitHub Actions CI"
```

---

## Task 7: Verify + tag + summary

- [ ] **Step 1:** Final verify

```bash
go test ./...
go test -tags e2e ./e2e/... -timeout 300s
go vet ./...
gofmt -l . | grep -v '^docs/' | grep -v '^BRAINSTORM' || echo "all formatted"
```

Expected: all green.

- [ ] **Step 2:** Smoke test the docker build

```bash
docker build -t netra-browser:test --build-arg VERSION=plan-f .
docker images | grep netra-browser
```

If docker isn't available, skip and note.

- [ ] **Step 3:** Tag

```bash
git tag plan-f-release
git tag -l
```

After Plan F: project is shippable. Final tally:
- 31 MCP tools
- 5 plans (A-E feature, F release)
- ~70+ commits
- 7 e2e tests passing against headless chromium

---

## Self-Review Notes

**Spec coverage check (final):**
- ✅ MIT license
- ✅ README with quickstart + comparison + tool reference link
- ✅ Examples matrix (4 files)
- ✅ GoReleaser multi-arch + Homebrew tap
- ✅ Docker image with chromium baked in
- ✅ GitHub Actions CI
- ⚠️ Demo gif — not in plan; flag for follow-up. The README references it but the file isn't created. Note as known TODO at the bottom of the README, OR drop the gif reference.

**Known follow-ups left for the user:**
- Replace `<user>` placeholder in README, .goreleaser.yaml with real GitHub username (the plan uses `mr-white-hat` as a default; verify before tagging a release).
- Demo gif (record on first internal use).
- Create the `homebrew-netra-browser` tap repo on GitHub.
- Push the first version tag (`v0.1.0`) to trigger GoReleaser.
