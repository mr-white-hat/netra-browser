---
status: archived (superseded by docs/superpowers/specs/2026-04-30-netra-browser-design.md)
date: 2026-04-28
topic: neutral browser-MCP bridge to replace Playwright
---

# Brainstorm draft — neutral CDP+MCP browser bridge

> **Note:** this is the original brainstorming artifact from 2026-04-28, kept for
> historical context. The finished design lives at
> [`docs/superpowers/specs/2026-04-30-netra-browser-design.md`](docs/superpowers/specs/2026-04-30-netra-browser-design.md)
> and the implementation tracks Plans A-F in
> [`docs/superpowers/plans/`](docs/superpowers/plans/).

## Origin

A bug-bounty automation pipeline was using Playwright for session capture, JWT
capture, signup loops, PDF rendering, etc. The goal was to replace it with
something:

- That works against the user's **real, logged-in Chrome** (cookies, MFA,
  corporate proxy, installed extensions).
- That any AI agent (Claude Code, Claude Desktop, Cursor, Gemini, custom MCP
  clients) can drive via MCP.
- Cross-platform: arm64 + amd64, Linux/macOS/Windows.
- Neutral / general-purpose so it can serve broad audiences (NOT bug-bounty-only
  in the codebase — bug bounty is one use case shown in `examples/`).

## Premise correction (important)

The original framing was "connect Claude Code to Claude in Chrome". That product
is a **user-facing browser assistant**, not a programmable backend — no public
API to drive it from outside. So Claude-for-Chrome integration is **deferred to
v2** as an optional companion-extension capability. v1 ships as a pure CDP+MCP
bridge with zero dependency on Claude for Chrome.

## Decided

- **Approach:** CDP-based MCP bridge attached to a real Chrome via
  `--remote-debugging-port` (or launched by the bridge using the user's real
  user-data-dir).
- **Language:** Go. Single static binary, no runtime deps. GoReleaser for
  multi-arch artifacts.
- **Repo:** Single neutral repo, MIT. Domain-specific workflows live in external
  agents and consume this over MCP like any other user.
- **Companion extension:** deferred to v2 (`*-extension` repo). v1 has none.
- **Tagline:** "Bring your own Chrome — the missing MCP bridge for AI agents
  that need a real, logged-in browser."

## Component split (initial draft)

```
<repo-name>/
├── cmd/<binary>/main.go
├── internal/
│   ├── cdp/          # CDP websocket client (~600 LOC, no business logic)
│   ├── browser/      # primitives: navigate/click/fill/eval/screenshot/cookies
│   ├── tasks/        # generic high-level: capture_har, render_pdf,
│   │                 #   save/load_session, wait_for_download, run_with_proxy
│   └── mcp/          # stdio + http-sse transports (same tools, two transports)
├── examples/         # bug-bounty.md, auth-scraping.md, e2e-qa.md, claude-desktop.md
├── docs/
├── .goreleaser.yaml
└── README.md
```

Domain-specific workflows (signup loops, recon orchestration, scope checks,
report rendering) live in the consuming agent and call this bridge over MCP.

## Open decisions (resolved during brainstorm)

1. **Name.** Candidates: `netra-browser`, `chrome-bridge`, `tether`, `byoc-mcp`,
   `realchrome`, `browser-bridge-mcp`. → resolved to `netra-browser`.
2. **Keep or cut `tasks/` package in v1?** → resolved to keep, ships as
   built-ins for a "finished" feel.

## Sections designed during the brainstorm

- [x] Data flow (agent → MCP → tasks → browser → cdp → Chrome → events back up)
- [x] Concrete MCP tool surface (names, schemas, return shapes)
- [x] Session / profile management (attach vs. launch, lock-file handling,
      preventing two bridges fighting for the same Chrome)
- [x] Error handling + reconnect (Chrome crashes, websocket drops, target
      destruction mid-call)
- [x] Auth / safety on the HTTP-SSE transport (token gating, localhost-only
      default, opt-in remote)
- [x] Testing strategy (unit, CDP integration via headless Chrome in CI,
      golden-file MCP transcripts)
- [x] Release + distribution (GoReleaser matrix, Homebrew tap, `go install`,
      Docker image for headless server use)
- [x] OSS positioning (README structure, demo gif/video, example matrix)

All landed in Plans A-F.

## Pain that motivated the project

Agents burning model turns on screenshot-loop browser control plus Playwright
friction on real-world authenticated targets (re-MFA loops, broken extensions,
bot fingerprinting flagging fresh Chrome instances).
