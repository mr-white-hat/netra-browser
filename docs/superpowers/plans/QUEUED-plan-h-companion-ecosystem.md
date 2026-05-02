# Plan H — Companion Ecosystem (QUEUED)

> **Status:** queued, not started. Picks up after Plan G ships.
> **Purpose:** sidecar tools and language-agnostic libraries that complement the bridge but live OUTSIDE the Go binary so they can iterate independently.
> **Builds on:** `plan-g-hotfixes-projects` tag.

The bridge core (Plans A-G) does one thing: expose Chrome over MCP. Everything in Plan H is **adjacent** — separate processes, separate repos eventually, often different languages — that connects to the bridge over its existing JSON-RPC surface.

Why split it out: the bridge should stay small, fast, and dependency-free. A 10MB Go static binary stays a 10MB Go static binary. Anyone who wants the page-state classifier downloads a separate ~1GB model + Python service. Anyone who only needs the bridge skips it.

---

## Why companions, not core?

| Item | Why outside the bridge |
|---|---|
| Page-state classifier | Needs an ML model (50MB-2GB depending on backbone), Python or ONNX runtime, periodic retraining. Belongs in a sidecar service. |
| Concurrent multi-tab fan-out | Pure orchestration logic — better as a thin client library in each language users prefer (Python, Node, Go). The bridge already supports multi-target via `target_id`. |
| Visual regression | Background scheduler + perceptual-hash store + alert delivery — out of scope for an MCP server. |
| OCR fallback | Big binary dependency (Tesseract/PaddleOCR), GPU-friendly; sidecar service that returns text given a screenshot. |
| JS primitives library | Versioned JS bundle deployed independently of the Go binary; agents inject via existing `browser_eval`. |
| Streaming notifications | Adds long-lived connection complexity to the bridge — better implemented as a separate fanout proxy that sits between the bridge and clients if it's needed at all. |
| localStorage in sessions | Could go in Plan G but better explored in a sidecar first to find the right CDP path (it's gnarly because LS is per-origin and per-tab). |

---

## Components

### 1. `netra-classifier` — local page-state classifier

A small HTTP service that takes a screenshot (PNG bytes or path) and returns `{state, confidence, alternatives}`.

**States** (initial v1 set, expandable via fine-tune):
- `login_form`
- `signup_form`
- `mfa_prompt`
- `captcha_v2`
- `captcha_v3_invisible` (heuristic: `.grecaptcha-badge` presence flagged separately)
- `consent_banner`
- `paywall`
- `error_page` (4xx/5xx)
- `dashboard` (authenticated content)
- `content_page` (unauthenticated)
- `loading` (skeleton screens, spinners)
- `empty` (no meaningful content)
- `unknown`

**Architecture:** small ViT or CLIP-style backbone (~3-4B params for general use, runs on commodity CPU; smaller distilled models for edge). Hand-labeled fine-tuning dataset of ~5000 screenshots.

**Why this matters for speed:** removes a whole AI-reasoning turn per page. The bridge's `browser_diagnose` returns a screenshot; today the agent sends it to its own model and reasons "is this a login page?" The classifier answers in 50-200ms with a structured label.

**Deployment:** Docker image `ghcr.io/mr-white-hat/netra-classifier:latest`. HTTP API on `:7879`. Bridge stays unchanged; agents call the classifier directly when they want the hint.

**Tasks:** dataset collection script, ONNX export, FastAPI service, Docker image, README, evaluation harness.

---

### 2. `netra-fanout` — concurrent multi-tab driver

Thin client libraries (Python first, Node second) that wrap the bridge's HTTP-SSE transport and fan out N independent task sequences across N tabs in one Chrome.

```python
from netra_fanout import Bridge, fan_out

b = Bridge("http://127.0.0.1:7878", token=os.getenv("TOKEN"))

results = fan_out(b, [
    ("ticket-12", run_qa_recipe),
    ("ticket-13", run_qa_recipe),
    ("ticket-14", run_qa_recipe),
], max_concurrency=4)
```

Internally: opens N tabs via `browser_new_tab`, threads each `target_id` through the per-task callable, joins on completion, returns aligned results.

**Why this matters:** the bridge already supports `target_id` per call. The missing piece is the orchestration layer agents reach for. A library that turns "do this 5 times in parallel against one Chrome" into one function call is high leverage and low effort.

**Tasks:** Python package, Node package, examples, integration with Plan G's project groups so each fan-out pinned to its own project.

---

### 3. `netra-watch` — visual regression scheduler

Cron-style service that periodically:
1. Loads a recipe (from Plan G's recipe store)
2. Drives the bridge to navigate to a list of URLs
3. Takes screenshots
4. Compares against the previous baseline via perceptual hash (pHash / aHash / dHash)
5. Alerts on drift above a configurable threshold (Slack, email, webhook)

**Use cases:** monitoring product pages for visual breakage, monitoring competitor sites, monitoring authenticated dashboards for layout regressions, monitoring landing pages for unauthorized changes.

**Architecture:** separate Go service (or Python — TBD), reads YAML config, stores baselines + diffs in sqlite or a directory tree. Optional integration with the classifier (#1) so "page state changed from `dashboard` to `login_form`" triggers an alert.

**Tasks:** config schema, scheduler, hash comparator, baseline store, alert dispatchers, docker image.

---

### 4. `netra-ocr` — OCR fallback for opaque pages

Sidecar HTTP service that takes a screenshot and returns extracted text + bounding boxes. For pages where the DOM is intentionally opaque (canvas-based design tools, image-only PDFs in browser, custom rendering), this gives agents a way to read content the accessibility tree doesn't expose.

**Backend:** Tesseract (CPU-friendly, ~100ms) or PaddleOCR (better accuracy, GPU-preferred, ~30ms on GPU). User chooses at install.

**Why a sidecar, not in-bridge:** OCR backends are big (Tesseract is ~50MB + language packs; PaddleOCR is ~200MB). Forcing every bridge user to download them is wrong. Sidecar lets you skip if you don't need it.

**Tasks:** dual-backend service, language pack management, Dockerfile, simple Python client.

---

### 5. `netra-actions` — JS primitives library

A versioned JavaScript bundle that agents inject into pages via the bridge's `browser_eval`, exposing pre-baked helpers under `window.__netra`. Saves agents from rewriting common probes:

```js
window.__netra.extractTable("table.results")           // → array of row objects
window.__netra.scrollToBottom({timeoutMs: 5000})       // smooth scroll, await new content
window.__netra.formAutoFill({email:"a@b", password:"x"}) // best-effort field detection + fill
window.__netra.viewportScreenshot({format:"png"})       // canvas-based capture, no CDP round-trip
window.__netra.detectFrameworks()                       // → {react: true, vue: false, ...}
window.__netra.openShadowRoots()                        // walk + return all shadow DOMs
```

**Distribution:** a single `.js` file hosted on GitHub releases. Agents fetch it once, inject via `browser_eval(fetch(...).then(...))` or pre-cache locally. Versioned with semver so agents can pin.

**Tasks:** initial helpers, bundling (esbuild), versioned releases, docs, Plan G's recipe runner reads `__netra.*` calls as first-class actions.

---

### 6. Streaming MCP notifications

The bridge's `/events` HTTP endpoint currently returns 204 — reserved for future use. Plan H implements proper SSE streaming where clients subscribe to a target's event stream and receive `Network.requestWillBeSent`, `Page.frameNavigated`, console messages, etc. as they happen, instead of polling `browser_get_recent_events`.

**Why deferred:** adds long-lived-connection complexity, backpressure handling, reconnect logic, and per-client subscription state to the bridge. Worth it once a real workload demands it; not before.

**Tasks:** SSE handler in `internal/mcp/httpsse.go`, per-client subscription state, integration test with a long-running client, docs.

---

### 7. localStorage / sessionStorage in `task_save_session`

v1 sessions persist cookies only. Many modern apps store auth tokens in `localStorage` instead. Adding LS/SS support requires:

1. Iterating origins the user has visited (via `Storage.getStorageKeyForFrame`).
2. Per-origin `Runtime.evaluate("JSON.stringify(localStorage)")` to extract.
3. On load, navigate to each origin first, then `Runtime.evaluate("Object.assign(localStorage, JSON.parse(...))")` to restore.

The "navigate to each origin first" step makes load slow and visible. There may be a less ugly path via `Storage` domain that warrants exploration before patching the bridge.

**Tasks:** spike on cleanest CDP path, implement in `internal/profile/sessions.go`, extend session JSON schema with backward compat.

---

## Out of scope for Plan H

- A custom local LLM replacement for Claude. The model IS the value; running locally trades capability for speed in the wrong direction.
- A fully autonomous agent that operates without user guidance. Operational discipline (recovery, live adaptation, speed policies) is exactly what humans-in-the-loop provide; removing them removes the feature.
- A captcha solver. Score-gated v3 is solved by attach mode (real Chrome looks like a real user). Visible v2 is better outsourced to paid services if you ever need it; building a solver is a research project that defenders break in weeks.

---

## Why split into components instead of one monolith?

Each component above stands alone. A user might want only `netra-fanout` and skip everything else. Or only `netra-watch` running against a pre-existing bridge. The bridge core stays the only required dependency; each companion adds capability without forcing it on people who don't want it.

This also matches how MCP itself is designed: small composable services connected by a known protocol, not a monolithic platform.

---

## Suggested order

1. `netra-fanout` (Python first) — half a day, immediate parallel-work win for any user.
2. `netra-actions` JS primitives — one week, compounds with everything else.
3. `netra-classifier` v1 with hand-labeled dataset — three to five days, removes a reasoning turn per page.
4. localStorage in sessions — two days, completes the session-persistence story.
5. Streaming notifications — three days, only if a real workload demands it.
6. `netra-watch` — week-long-ish, audience-dependent.
7. `netra-ocr` — week-long, niche but valuable for canvas-app users.
