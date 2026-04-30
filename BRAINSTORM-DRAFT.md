---
status: in-progress (paused 2026-04-28)
topic: neutral browser-MCP bridge to replace Playwright
next-step: resume by re-invoking superpowers:brainstorming and pointing at this file
---

# Brainstorm draft — neutral CDP+MCP browser bridge

## Origin
User was using Playwright inside Trinetra (`~/BugBounty/Tools/playwright/` + the
`autonomous-test-account-creation` skill) for session capture, JWT capture, signup
loops, PDF rendering, etc. Wants to replace it with something:

- That works against the user's **real, logged-in Chrome** (cookies, MFA, Burp
  proxy, installed extensions including Claude for Chrome).
- That any AI agent (Trinetra, Claude Code, Claude Desktop, Claude.ai) can drive
  via MCP.
- Cross-platform: arm64 + amd64, Linux/macOS/Windows.
- Neutral / general-purpose so it can pull stars and forks (NOT bug-bounty-only
  in the codebase — bug bounty is one use case shown in `examples/`).

## Premise correction (important)
The original framing was "connect Claude Code to Claude in Chrome". That product
is a **user-facing browser assistant**, not a programmable backend — no public
API to drive it from outside. So Claude-for-Chrome integration is **deferred to
v2** as an optional companion-extension capability. v1 ships as a pure CDP+MCP
bridge with zero dependency on Claude for Chrome.

## Decided so far

- **Approach:** CDP-based MCP bridge attached to a real Chrome via
  `--remote-debugging-port` (or launched by the bridge using the user's real
  user-data-dir).
- **Language:** Go. Single static binary, no runtime deps. GoReleaser for
  multi-arch artifacts.
- **Repo:** Single neutral repo, MIT. Bug-bounty stays in Trinetra and consumes
  this over MCP like any other user.
- **Companion extension:** deferred to v2 (`*-extension` repo). v1 has none.
- **Tagline:** "Bring your own Chrome — the missing MCP bridge for AI agents
  that need a real, logged-in browser."

## Component split (current draft)

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

Bug-bounty workflows (`SignupWithEmailLoop`, loot integration, scope-check, the
`bb-render-pdf` replacement) live in Trinetra and call this bridge over MCP.

## Open decisions (when we resume)

1. **Name.** Candidates: `netra-browser` (ties to Trinetra brand), `chrome-bridge`,
   `tether`, `byoc-mcp`, `realchrome`, `browser-bridge-mcp`. Or pick later.
2. **Keep or cut `tasks/` package in v1?** Keep = `capture_har`, `render_pdf`,
   `save_session` etc. ship as built-ins. Cut = v1 is only low-level `browser`
   primitives, users compose. Recommendation was **keep** — gives the tool a
   "finished" feel. User hasn't confirmed.

## Sections still to design (per brainstorming skill)

- [ ] Data flow (how a tool call moves: agent → MCP → tasks → browser → cdp →
      Chrome → events back up)
- [ ] Concrete MCP tool surface (names, schemas, return shapes)
- [ ] Session / profile management (attach vs. launch, lock-file handling,
      preventing two bridges fighting for the same Chrome)
- [ ] Error handling + reconnect (Chrome crashes, websocket drops, target
      destruction mid-call)
- [ ] Auth / safety on the HTTP-SSE transport (token gating, localhost-only
      default, opt-in remote)
- [ ] Testing strategy (unit, CDP integration via headless Chrome in CI,
      golden-file MCP transcripts)
- [ ] Release + distribution (GoReleaser matrix, Homebrew tap, `go install`,
      Docker image for headless server use)
- [ ] OSS positioning (README structure, demo gif/video, example matrix)

## How to resume

1. Re-invoke `/superpowers:brainstorming`.
2. Point at this file.
3. Answer the two open decisions (name, keep-or-cut `tasks/`).
4. Continue from "data flow + tool surface".

## Reference: prior conversation context

User runs Trinetra (autonomous bug-bounty agent, R1→S5 pipeline) at
`~/BugBounty/`. Currently uses Playwright in `~/BugBounty/Tools/playwright/`.
Empty placeholder dir at `~/ClaudePlaywright/` is where this draft lives.
Pain that motivated the project: agent burning turns on screenshot-loop browser
control, plus Playwright friction on real-world authenticated targets.
