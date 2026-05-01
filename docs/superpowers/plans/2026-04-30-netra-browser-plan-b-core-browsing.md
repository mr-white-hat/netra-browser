# netra-browser Plan B — Core Browsing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add navigation, accessibility-tree snapshot, locator resolution, and interaction tools so an agent can drive forms on a real page.

**Architecture:** Introduce per-target CDP sessions (every interaction with a tab is routed via `sessionId`). New `internal/browser/` package wraps a target and exposes `Navigate`, `Snapshot`, `Click`, `Fill`, etc. Locator union resolves to a backend node ID via a five-strategy fallback chain. Snapshot returns a compact accessibility tree with stable `#id` labels that interaction tools accept.

**Tech Stack:** Same as Plan A — Go 1.22+, gorilla/websocket, stdlib-only otherwise.

**Source spec:** `docs/superpowers/specs/2026-04-30-netra-browser-design.md`

**Builds on:** `plan-a-foundation` tag (`a372786`).

---

## File Structure (Plan B)

| Path | Responsibility |
|---|---|
| `internal/cdp/session.go` | Per-target session attach (`AttachToTarget`), `SendOnTarget`, session-filtered `SubscribeOnTarget` |
| `internal/cdp/session_test.go` | Tests for per-target dispatch |
| `internal/browser/page.go` | `Page` struct binding a `*cdp.Client` + `targetID` + `sessionID`. Holds per-page state (snapshot id table, network counter for networkidle). |
| `internal/browser/navigate.go` | `Page.Navigate`, `GoBack`, `GoForward`, `Reload`. wait_until logic. |
| `internal/browser/snapshot.go` | `Page.Snapshot(mode)` → compact accessibility tree with stable `#id` labels |
| `internal/browser/locator.go` | Resolve `Locator` union to backend node ID |
| `internal/browser/interact.go` | `Click`, `Fill`, `Hover`, `SelectOption`, `PressKey`, `UploadFile` |
| `internal/browser/page_test.go` | Per-method tests against a fake CDP sender |
| `internal/browser/locator_test.go` | Locator resolution tests |
| `internal/browser/snapshot_test.go` | AX tree → compact tree tests with golden fixtures |
| `internal/mcp/tools/browser_nav.go` | Tool registrations: navigate, go_back, go_forward, reload |
| `internal/mcp/tools/browser_inspect.go` | Tool registrations: snapshot, screenshot stub (full screenshot in Plan C) |
| `internal/mcp/tools/browser_interact.go` | Tool registrations: click, fill, hover, select_option, press_key, upload_file |
| `internal/mcp/session.go` (modify) | Add `Pages map[targetID]*browser.Page` cache so each tool call reuses the page state instead of re-attaching |
| `e2e/forms_test.go` | E2E: navigate to httptest fixture form, snapshot, click, fill, verify |

---

## Task 1: Per-target CDP sessions

**Files:**
- Modify: `internal/cdp/client.go` — make `Send` route by optional `sessionID`
- Create: `internal/cdp/session.go`
- Create: `internal/cdp/session_test.go`

The browser-level WS we already connect to (Plan A) handles `Target.*` methods globally. To drive a tab we must call `Target.attachToTarget{flatten: true}` to get a `sessionId`, then attach `"sessionId":"..."` to every method we send for that tab. Likewise, events arriving from that tab carry `sessionId` and must be filtered.

- [ ] **Step 1: Failing test**

`internal/cdp/session_test.go`:

```go
package cdp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestAttachToTargetReturnsSession(t *testing.T) {
	url, stop := fakeChrome(t, func(c *websocket.Conn) {
		defer c.Close()
		var m Method
		if err := c.ReadJSON(&m); err != nil {
			return
		}
		// Reply: sessionId for the requested target.
		_ = c.WriteJSON(Response{ID: m.ID, Result: json.RawMessage(`{"sessionId":"S1"}`)})
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sid, err := c.AttachToTarget(ctx, "T1")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if sid != "S1" {
		t.Fatalf("got %q", sid)
	}
}

func TestSendOnTargetIncludesSessionID(t *testing.T) {
	got := make(chan Method, 1)
	url, stop := fakeChrome(t, func(c *websocket.Conn) {
		defer c.Close()
		var m Method
		if err := c.ReadJSON(&m); err != nil {
			return
		}
		got <- m
		_ = c.WriteJSON(Response{ID: m.ID, Result: json.RawMessage(`{}`)})
	})
	defer stop()

	c, _ := Dial(context.Background(), url)
	defer c.Close()
	_, err := c.SendOnTarget(context.Background(), "S1", "Page.enable", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := <-got
	if m.SessionID != "S1" {
		t.Fatalf("expected sessionId on outbound method, got %+v", m)
	}
}

func TestSubscribeOnTargetFiltersBySession(t *testing.T) {
	url, stop := fakeChrome(t, func(c *websocket.Conn) {
		defer c.Close()
		_ = c.WriteJSON(Event{Method: "Page.frameNavigated", Params: json.RawMessage(`{}`), SessionID: "S1"})
		_ = c.WriteJSON(Event{Method: "Page.frameNavigated", Params: json.RawMessage(`{}`), SessionID: "S2"})
		time.Sleep(200 * time.Millisecond)
	})
	defer stop()

	c, _ := Dial(context.Background(), url)
	defer c.Close()
	ch := c.SubscribeOnTarget("S1", "Page.frameNavigated")
	select {
	case e := <-ch:
		if e.SessionID != "S1" {
			t.Fatalf("got session %q", e.SessionID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("event missing")
	}
	// no second event for S1 should arrive
	select {
	case e := <-ch:
		t.Fatalf("unexpected second event: %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/cdp/ -run "AttachToTarget|SendOnTarget|SubscribeOnTarget" -v`
Expected: FAIL — undefined methods.

- [ ] **Step 3: Implement**

Add to `internal/cdp/session.go`:

```go
package cdp

import (
	"context"
	"encoding/json"
	"fmt"
)

// AttachToTarget asks Chrome to give us a flattened session for targetID.
func (c *Client) AttachToTarget(ctx context.Context, targetID string) (string, error) {
	raw, err := c.Send(ctx, "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	})
	if err != nil {
		return "", fmt.Errorf("attachToTarget: %w", err)
	}
	var resp struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decode attachToTarget: %w", err)
	}
	if resp.SessionID == "" {
		return "", fmt.Errorf("attachToTarget returned empty sessionId")
	}
	return resp.SessionID, nil
}

// SendOnTarget sends a CDP method scoped to a session.
func (c *Client) SendOnTarget(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan Response, 1)
	c.pending.Store(id, ch)
	defer c.pending.Delete(id)

	c.writeMu.Lock()
	err := c.conn.WriteJSON(Method{ID: id, Name: method, Params: params, SessionID: sessionID})
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", method, err)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, fmt.Errorf("cdp client closed")
	case r := <-ch:
		if r.Error != nil {
			return nil, r.Error
		}
		return r.Result, nil
	}
}

// SubscribeOnTarget returns a channel that receives only events whose SessionID matches.
func (c *Client) SubscribeOnTarget(sessionID, method string) chan BufferedEvent {
	src := c.Subscribe(method)
	out := make(chan BufferedEvent, 16)
	go func() {
		defer close(out)
		for e := range src {
			if e.SessionID == sessionID {
				select {
				case out <- e:
				default:
				}
			}
		}
	}()
	return out
}
```

The `SubscribeOnTarget` filtering helper currently leaks the underlying source channel (Subscribe never closes). That's fine for v1 — the subscriber goroutine exits when the client closes. Document this with a `// TODO` for cleanup in Plan D.

- [ ] **Step 4: Run, verify pass**

Run: `go test ./internal/cdp/ -v`
Expected: all PASS (Plan A tests + 3 new).

- [ ] **Step 5: Commit**

```bash
cd /home/mrwhitehat/ClaudePlaywright
git add internal/cdp/session.go internal/cdp/session_test.go
git commit -m "cdp: add per-target session attach + SendOnTarget + SubscribeOnTarget"
```

---

## Task 2: `browser` package skeleton + Page

**Files:**
- Create: `internal/browser/page.go`
- Create: `internal/browser/page_test.go`

A `Page` is a thin wrapper: target ID + session ID + a CDP sender. It owns a snapshot ID table (filled later). The interface it depends on is small and is satisfied by `*cdp.Client`.

- [ ] **Step 1: Failing test**

`internal/browser/page_test.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeSender struct {
	calls    []call
	results  map[string]json.RawMessage
}
type call struct {
	method  string
	session string
	params  any
}

func (f *fakeSender) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, call{method: method, params: params})
	if r, ok := f.results[method]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeSender) SendOnTarget(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, call{method: method, session: session, params: params})
	if r, ok := f.results[method]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeSender) AttachToTarget(ctx context.Context, targetID string) (string, error) {
	return "S-" + targetID, nil
}

func TestNewPageAttaches(t *testing.T) {
	f := &fakeSender{}
	p, err := NewPage(context.Background(), f, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if p.SessionID() != "S-T1" {
		t.Fatalf("session: %q", p.SessionID())
	}
}

func TestPageEnableDomainsCalled(t *testing.T) {
	f := &fakeSender{}
	if _, err := NewPage(context.Background(), f, "T1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"Page.enable", "DOM.enable", "Runtime.enable", "Accessibility.enable"}
	for _, m := range want {
		found := false
		for _, c := range f.calls {
			if c.method == m && strings.HasPrefix(c.session, "S-") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s on session", m)
		}
	}
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/browser/ -v`
Expected: FAIL — package doesn't exist or `NewPage` undefined.

- [ ] **Step 3: Implement**

`internal/browser/page.go`:

```go
// Package browser exposes the higher-level browser primitives that build on cdp.
package browser

import (
	"context"
	"encoding/json"
	"sync"
)

// Sender is what Page needs from a CDP transport. *cdp.Client satisfies it.
type Sender interface {
	Send(ctx context.Context, method string, params any) (json.RawMessage, error)
	SendOnTarget(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error)
	AttachToTarget(ctx context.Context, targetID string) (string, error)
}

// Page binds a single target plus its CDP session.
type Page struct {
	cdp       Sender
	targetID  string
	sessionID string

	mu        sync.Mutex
	snapshot  *Snapshot      // last snapshot for snapshot_id resolution
}

// NewPage attaches to a target and enables Page/DOM/Runtime/Accessibility domains.
func NewPage(ctx context.Context, c Sender, targetID string) (*Page, error) {
	sid, err := c.AttachToTarget(ctx, targetID)
	if err != nil {
		return nil, err
	}
	p := &Page{cdp: c, targetID: targetID, sessionID: sid}
	for _, m := range []string{"Page.enable", "DOM.enable", "Runtime.enable", "Accessibility.enable"} {
		if _, err := c.SendOnTarget(ctx, sid, m, nil); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// TargetID returns the underlying target id.
func (p *Page) TargetID() string { return p.targetID }

// SessionID returns the attached session id.
func (p *Page) SessionID() string { return p.sessionID }

// send is the internal helper for routing to the right session.
func (p *Page) send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return p.cdp.SendOnTarget(ctx, p.sessionID, method, params)
}

// Snapshot is set later by snapshot.go; keeping the type forward-declared here.
type Snapshot struct {
	Nodes  []SnapshotNode
	byID   map[string]*SnapshotNode // populated during render
}

type SnapshotNode struct {
	ID       string         `json:"id"`
	Role     string         `json:"role"`
	Name     string         `json:"name,omitempty"`
	Value    string         `json:"value,omitempty"`
	Children []SnapshotNode `json:"children,omitempty"`

	// internal: backend node id from Accessibility.getFullAXTree
	BackendNodeID int64 `json:"-"`
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/browser/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/browser/page.go internal/browser/page_test.go
git commit -m "browser: add Page type with per-target attach and domain enable"
```

---

## Task 3: Page.Navigate (load + domcontentloaded)

**Files:**
- Create: `internal/browser/navigate.go`
- Create: `internal/browser/navigate_test.go`

- [ ] **Step 1: Failing test**

`internal/browser/navigate_test.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNavigateLoadWaitUntil(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")

	res, err := p.Navigate(context.Background(), NavigateOpts{URL: "https://example.com", WaitUntil: WaitLoad})
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	// Verify we issued Page.navigate on the right session.
	saw := false
	for _, c := range f.calls {
		if c.method == "Page.navigate" && c.session == "S-T1" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("Page.navigate not sent: %+v", f.calls)
	}
	if res.URL != "https://example.com" {
		t.Fatalf("res.URL: %s", res.URL)
	}
}

func TestNavigateInvalidWaitUntilDefaults(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	_, err := p.Navigate(context.Background(), NavigateOpts{URL: "https://example.com", WaitUntil: ""})
	if err != nil {
		t.Fatal(err)
	}
}
```

The fake `Sender` here doesn't fire load events. The `WaitLoad` path must therefore not block on event delivery in this test — it must complete on the `Page.navigate` reply alone. To make the test honest while keeping the path testable, we model navigate as: issue `Page.navigate`; if a `loadEventFired` event is buffered, return; else return on a short fallback once `Page.navigate` returns. Since the fake doesn't subscribe to events, just complete on the reply when no event source is wired. **This compromise is intentional for unit tests.** The real behavior is exercised in the e2e test (Task 12).

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/browser/ -run Navigate -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`internal/browser/navigate.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

type WaitUntil string

const (
	WaitLoad             WaitUntil = "load"
	WaitDOMContentLoaded WaitUntil = "domcontentloaded"
	WaitNetworkIdle      WaitUntil = "networkidle"
)

type NavigateOpts struct {
	URL       string
	WaitUntil WaitUntil
}

type NavigateResult struct {
	URL       string
	Title     string
	FrameID   string
	Snapshot  *Snapshot // filled by tool layer if return_snapshot=true
}

// Navigate issues Page.navigate. The wait_until logic is best-effort in unit tests
// (event-bus unwired); real waits are validated by the e2e test in Task 12.
func (p *Page) Navigate(ctx context.Context, opts NavigateOpts) (*NavigateResult, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("URL required")
	}
	if opts.WaitUntil == "" {
		opts.WaitUntil = WaitLoad
	}

	raw, err := p.send(ctx, "Page.navigate", map[string]any{"url": opts.URL})
	if err != nil {
		return nil, err
	}
	var resp struct {
		FrameID   string `json:"frameId"`
		ErrorText string `json:"errorText,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode Page.navigate: %w", err)
	}
	if resp.ErrorText != "" {
		return nil, fmt.Errorf("navigate: %s", resp.ErrorText)
	}

	// In real usage, we'd block on the relevant event for opts.WaitUntil.
	// Plan B Task 4 wires up an event subscriber; for now we return immediately.
	return &NavigateResult{URL: opts.URL, FrameID: resp.FrameID}, nil
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/browser/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/browser/navigate.go internal/browser/navigate_test.go
git commit -m "browser: add Page.Navigate with wait_until placeholder"
```

---

## Task 4: Wait-until event waiter

**Files:**
- Modify: `internal/browser/page.go` — add `waitForEvent` helper using `cdp.Client.SubscribeOnTarget`
- Modify: `internal/browser/navigate.go` — actually block on the event in `Navigate`
- Create: `internal/browser/navigate_wait_test.go`

`waitForEvent` needs an event source. The `Sender` interface above only routes methods; to subscribe to events we need a richer interface. Add:

```go
// EventSource is implemented by *cdp.Client; tests provide a fake.
type EventSource interface {
	SubscribeOnTarget(sessionID, method string) <-chan struct {
		Method string
	}
}
```

Wait — that's an awkward intermediate type. Better: make `Sender` extend with a typed subscribe.

Concrete plan: rename and extend `Sender` to also include `SubscribeOnTarget(sessionID, method string) chan cdp.BufferedEvent`. Inside `internal/browser/`, take a dependency on `internal/cdp` for the `BufferedEvent` type.

Actually `internal/browser/` can import `internal/cdp`. We're already inside `internal/`. Import it.

- [ ] **Step 1: Update `Sender` interface in `internal/browser/page.go`**

Replace the existing `Sender` interface with:

```go
import (
	// ...existing...
	"github.com/pavankumar2138/netra-browser/internal/cdp"
)

// Sender is what Page needs from a CDP transport. *cdp.Client satisfies it.
type Sender interface {
	Send(ctx context.Context, method string, params any) (json.RawMessage, error)
	SendOnTarget(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error)
	AttachToTarget(ctx context.Context, targetID string) (string, error)
	SubscribeOnTarget(sessionID, method string) chan cdp.BufferedEvent
}
```

Update `fakeSender` in `page_test.go` and `navigate_test.go` to implement `SubscribeOnTarget` returning a closed channel.

- [ ] **Step 2: Failing test for blocking wait**

`internal/browser/navigate_wait_test.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
)

// pumpableSender lets a test push events at will.
type pumpableSender struct {
	*fakeSender
	events map[string]chan cdp.BufferedEvent
}

func newPumpable() *pumpableSender {
	return &pumpableSender{
		fakeSender: &fakeSender{},
		events:     map[string]chan cdp.BufferedEvent{},
	}
}
func (p *pumpableSender) SubscribeOnTarget(_ , method string) chan cdp.BufferedEvent {
	ch, ok := p.events[method]
	if !ok {
		ch = make(chan cdp.BufferedEvent, 4)
		p.events[method] = ch
	}
	return ch
}
func (p *pumpableSender) push(method string) {
	if ch, ok := p.events[method]; ok {
		ch <- cdp.BufferedEvent{Method: method, Params: json.RawMessage(`{}`)}
	}
}

func TestNavigateBlocksUntilLoadEvent(t *testing.T) {
	p := newPumpable()
	page, _ := NewPage(context.Background(), p, "T1")

	done := make(chan error, 1)
	go func() {
		_, err := page.Navigate(context.Background(), NavigateOpts{URL: "https://example.com", WaitUntil: WaitLoad})
		done <- err
	}()

	// Make sure Navigate hasn't returned yet.
	select {
	case err := <-done:
		t.Fatalf("returned too early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	p.push("Page.loadEventFired")

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not return after loadEventFired")
	}
}
```

Update existing tests' `fakeSender` to add a no-op `SubscribeOnTarget(_, _ string) chan cdp.BufferedEvent { return make(chan cdp.BufferedEvent) }`.

- [ ] **Step 3: Implement**

In `navigate.go`, replace the body of `Navigate` so it subscribes BEFORE issuing `Page.navigate`, then blocks on the event after the reply. Use a 30-second default timeout if the caller's context has none.

```go
func (p *Page) Navigate(ctx context.Context, opts NavigateOpts) (*NavigateResult, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("URL required")
	}
	if opts.WaitUntil == "" {
		opts.WaitUntil = WaitLoad
	}

	var waitMethod string
	switch opts.WaitUntil {
	case WaitLoad:
		waitMethod = "Page.loadEventFired"
	case WaitDOMContentLoaded:
		waitMethod = "Page.domContentEventFired"
	case WaitNetworkIdle:
		// Plan B Task 5 implements networkidle properly.
		waitMethod = "Page.loadEventFired"
	default:
		return nil, fmt.Errorf("unknown wait_until: %s", opts.WaitUntil)
	}

	events := p.cdp.SubscribeOnTarget(p.sessionID, waitMethod)

	raw, err := p.send(ctx, "Page.navigate", map[string]any{"url": opts.URL})
	if err != nil {
		return nil, err
	}
	var resp struct {
		FrameID   string `json:"frameId"`
		ErrorText string `json:"errorText,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode Page.navigate: %w", err)
	}
	if resp.ErrorText != "" {
		return nil, fmt.Errorf("navigate: %s", resp.ErrorText)
	}

	select {
	case <-events:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &NavigateResult{URL: opts.URL, FrameID: resp.FrameID}, nil
}
```

- [ ] **Step 4: Run all browser tests**

Run: `go test ./internal/browser/ -v`
Expected: PASS (existing + new wait test).

- [ ] **Step 5: Commit**

```bash
git add internal/browser/navigate.go internal/browser/navigate_wait_test.go internal/browser/page.go internal/browser/page_test.go internal/browser/navigate_test.go
git commit -m "browser: block Navigate on wait_until event delivery"
```

---

## Task 5: networkidle wait

**Files:**
- Create: `internal/browser/networkidle.go`
- Create: `internal/browser/networkidle_test.go`
- Modify: `internal/browser/navigate.go` — call into networkidle helper when `WaitUntil == WaitNetworkIdle`

`networkidle` = no in-flight `Network.requestWillBeSent` outstanding for 500ms. Track outstanding requests by subscribing to `Network.requestWillBeSent`, `Network.loadingFinished`, `Network.loadingFailed`.

- [ ] **Step 1: Failing test**

`internal/browser/networkidle_test.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
)

func TestNetworkIdleReturnsAfterQuiet(t *testing.T) {
	p := newPumpable()
	p.events["Network.requestWillBeSent"] = make(chan cdp.BufferedEvent, 8)
	p.events["Network.loadingFinished"] = make(chan cdp.BufferedEvent, 8)
	p.events["Network.loadingFailed"] = make(chan cdp.BufferedEvent, 8)

	page, _ := NewPage(context.Background(), p, "T1")

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		done <- page.WaitNetworkIdle(ctx, 200*time.Millisecond)
	}()

	// Fire one request, then finish it.
	p.events["Network.requestWillBeSent"] <- cdp.BufferedEvent{Method: "Network.requestWillBeSent", Params: json.RawMessage(`{"requestId":"R1"}`)}
	time.Sleep(50 * time.Millisecond)
	p.events["Network.loadingFinished"] <- cdp.BufferedEvent{Method: "Network.loadingFinished", Params: json.RawMessage(`{"requestId":"R1"}`)}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("did not return")
	}
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/browser/ -run NetworkIdle -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`internal/browser/networkidle.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// WaitNetworkIdle blocks until there are zero in-flight network requests for `quiet` duration.
func (p *Page) WaitNetworkIdle(ctx context.Context, quiet time.Duration) error {
	willBeSent := p.cdp.SubscribeOnTarget(p.sessionID, "Network.requestWillBeSent")
	finished := p.cdp.SubscribeOnTarget(p.sessionID, "Network.loadingFinished")
	failed := p.cdp.SubscribeOnTarget(p.sessionID, "Network.loadingFailed")

	var mu sync.Mutex
	inflight := map[string]struct{}{}
	idle := time.NewTimer(quiet)
	defer idle.Stop()
	resetIdle := func() {
		if !idle.Stop() {
			select {
			case <-idle.C:
			default:
			}
		}
		idle.Reset(quiet)
	}
	bump := func(rid string, add bool) {
		mu.Lock()
		defer mu.Unlock()
		if add {
			inflight[rid] = struct{}{}
		} else {
			delete(inflight, rid)
		}
		if len(inflight) == 0 {
			resetIdle()
		}
	}

	for {
		select {
		case e := <-willBeSent:
			var v struct {
				RequestID string `json:"requestId"`
			}
			_ = json.Unmarshal(e.Params, &v)
			bump(v.RequestID, true)
		case e := <-finished:
			var v struct {
				RequestID string `json:"requestId"`
			}
			_ = json.Unmarshal(e.Params, &v)
			bump(v.RequestID, false)
		case e := <-failed:
			var v struct {
				RequestID string `json:"requestId"`
			}
			_ = json.Unmarshal(e.Params, &v)
			bump(v.RequestID, false)
		case <-idle.C:
			mu.Lock()
			n := len(inflight)
			mu.Unlock()
			if n == 0 {
				return nil
			}
			// requests came back; idle hasn't truly passed
			idle.Reset(quiet)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
```

Modify `navigate.go`'s `WaitNetworkIdle` branch:

```go
case WaitNetworkIdle:
    // First wait for load to fire, then for network idle.
    waitMethod = "Page.loadEventFired"
    // (hooked below)
```

After the `select` block that waits for the load event, add:

```go
if opts.WaitUntil == WaitNetworkIdle {
    if err := p.WaitNetworkIdle(ctx, 500*time.Millisecond); err != nil {
        return nil, err
    }
}
```

Also add `"time"` to imports if missing.

- [ ] **Step 4: Run**

Run: `go test ./internal/browser/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/browser/networkidle.go internal/browser/networkidle_test.go internal/browser/navigate.go
git commit -m "browser: implement networkidle wait"
```

---

## Task 6: GoBack / GoForward / Reload

**Files:**
- Modify: `internal/browser/navigate.go`
- Create: `internal/browser/navigate_history_test.go`

- [ ] **Step 1: Failing test**

`internal/browser/navigate_history_test.go`:

```go
package browser

import (
	"context"
	"strings"
	"testing"
)

func TestReloadSendsPageReload(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, c := range f.calls {
		if strings.HasSuffix(c.method, "Page.reload") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("Page.reload not sent: %+v", f.calls)
	}
}
```

(Add similar trivial tests for `GoBack`/`GoForward` if desired — the implementer can write them.)

- [ ] **Step 2: Run failure**

Run: `go test ./internal/browser/ -run Reload -v`. Expected FAIL.

- [ ] **Step 3: Implement**

Add to `navigate.go`:

```go
func (p *Page) GoBack(ctx context.Context) error {
	_, err := p.send(ctx, "Page.navigateToHistoryEntry", map[string]any{"entryId": -1}) // placeholder
	// CDP actually: get history first via Page.getNavigationHistory, then navigate to current-1
	return err
}

func (p *Page) GoForward(ctx context.Context) error {
	_, err := p.send(ctx, "Page.navigateToHistoryEntry", map[string]any{"entryId": 1})
	return err
}

func (p *Page) Reload(ctx context.Context) error {
	_, err := p.send(ctx, "Page.reload", nil)
	return err
}
```

The `GoBack`/`GoForward` shape above is a simplification — proper history navigation requires:
1. `Page.getNavigationHistory` to get `{currentIndex, entries[]}`.
2. `Page.navigateToHistoryEntry` with `entries[currentIndex-1].id`.

Replace the simplified versions with the proper two-step flow. The implementer should look up current Chrome's CDP doc for `Page.getNavigationHistory`.

```go
func (p *Page) GoBack(ctx context.Context) error {
	return p.historyStep(ctx, -1)
}
func (p *Page) GoForward(ctx context.Context) error {
	return p.historyStep(ctx, +1)
}
func (p *Page) historyStep(ctx context.Context, delta int) error {
	raw, err := p.send(ctx, "Page.getNavigationHistory", nil)
	if err != nil {
		return err
	}
	var hist struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &hist); err != nil {
		return err
	}
	target := hist.CurrentIndex + delta
	if target < 0 || target >= len(hist.Entries) {
		return fmt.Errorf("history out of range")
	}
	_, err = p.send(ctx, "Page.navigateToHistoryEntry", map[string]any{"entryId": hist.Entries[target].ID})
	return err
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/browser/ -v`. Expected PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/browser/navigate.go internal/browser/navigate_history_test.go
git commit -m "browser: add GoBack/GoForward/Reload"
```

---

## Task 7: Snapshot — accessibility tree fetch + render

**Files:**
- Create: `internal/browser/snapshot.go`
- Create: `internal/browser/snapshot_test.go`
- Create: `internal/browser/testdata/ax_simple.json` (golden fixture)

`Accessibility.getFullAXTree` returns a list of `AXNode` with `nodeId`, `role`, `name`, `value`, `childIds[]`, `backendDOMNodeId`. We render this to a tree with stable `#id` labels (incrementing counter or hash of nodeId).

- [ ] **Step 1: Create golden fixture**

`internal/browser/testdata/ax_simple.json`:

```json
{
  "nodes": [
    {"nodeId":"1","role":{"value":"WebArea"},"name":{"value":"Test"},"childIds":["2","3"],"backendDOMNodeId":100},
    {"nodeId":"2","role":{"value":"button"},"name":{"value":"Click me"},"backendDOMNodeId":101},
    {"nodeId":"3","role":{"value":"textbox"},"name":{"value":"Email"},"value":{"value":"a@b.c"},"backendDOMNodeId":102}
  ]
}
```

- [ ] **Step 2: Failing test**

`internal/browser/snapshot_test.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestSnapshotRendersAXTree(t *testing.T) {
	raw, err := os.ReadFile("testdata/ax_simple.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Accessibility.getFullAXTree": raw,
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	snap, err := p.Snapshot(context.Background(), SnapshotAccessibility)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(snap.Nodes))
	}
	root := snap.Nodes[0]
	if root.Role != "WebArea" || len(root.Children) != 2 {
		t.Fatalf("root: %+v", root)
	}
	if root.Children[0].Role != "button" || root.Children[0].Name != "Click me" {
		t.Fatalf("button child wrong: %+v", root.Children[0])
	}
	if root.Children[1].Value != "a@b.c" {
		t.Fatalf("textbox value: %q", root.Children[1].Value)
	}
	// IDs are stable monotonic
	if root.ID != "#a0" || root.Children[0].ID != "#a1" {
		t.Fatalf("ids: %s %s", root.ID, root.Children[0].ID)
	}
	// snapshot_id resolution table populated
	if snap.byID["#a1"] == nil {
		t.Fatal("byID missing #a1")
	}
}
```

- [ ] **Step 3: Run failure**

Run: `go test ./internal/browser/ -run Snapshot -v`. Expected FAIL.

- [ ] **Step 4: Implement**

`internal/browser/snapshot.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

type SnapshotMode string

const (
	SnapshotAccessibility SnapshotMode = "accessibility"
	SnapshotDOMText       SnapshotMode = "dom_text"
)

type axNode struct {
	NodeID         string   `json:"nodeId"`
	BackendDOMNodeID int64  `json:"backendDOMNodeId"`
	Role           axValue  `json:"role"`
	Name           axValue  `json:"name"`
	Value          axValue  `json:"value"`
	ChildIDs       []string `json:"childIds"`
	Ignored        bool     `json:"ignored"`
}
type axValue struct {
	Value any `json:"value"`
}

func (v axValue) String() string {
	if v.Value == nil {
		return ""
	}
	switch s := v.Value.(type) {
	case string:
		return s
	default:
		return fmt.Sprint(s)
	}
}

func (p *Page) Snapshot(ctx context.Context, mode SnapshotMode) (*Snapshot, error) {
	if mode == "" {
		mode = SnapshotAccessibility
	}
	if mode == SnapshotDOMText {
		return p.snapshotDOMText(ctx)
	}

	raw, err := p.send(ctx, "Accessibility.getFullAXTree", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Nodes []axNode `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode AX tree: %w", err)
	}

	byNodeID := make(map[string]*axNode, len(resp.Nodes))
	for i := range resp.Nodes {
		byNodeID[resp.Nodes[i].NodeID] = &resp.Nodes[i]
	}

	snap := &Snapshot{byID: map[string]*SnapshotNode{}}
	var counter int
	mkID := func() string {
		id := "#a" + strconv.Itoa(counter)
		counter++
		return id
	}

	var visit func(node *axNode) *SnapshotNode
	visit = func(node *axNode) *SnapshotNode {
		if node == nil || node.Ignored {
			return nil
		}
		out := &SnapshotNode{
			ID:            mkID(),
			Role:          node.Role.String(),
			Name:          node.Name.String(),
			Value:         node.Value.String(),
			BackendNodeID: node.BackendDOMNodeID,
		}
		for _, cid := range node.ChildIDs {
			if c := visit(byNodeID[cid]); c != nil {
				out.Children = append(out.Children, *c)
			}
		}
		// prune nodes that have no name/value AND no children — they're noise.
		if out.Name == "" && out.Value == "" && len(out.Children) == 0 {
			counter-- // rewind id
			return nil
		}
		snap.byID[out.ID] = out
		return out
	}

	// Roots are nodes whose nodeId isn't in any childIds list.
	childSet := map[string]struct{}{}
	for _, n := range resp.Nodes {
		for _, c := range n.ChildIDs {
			childSet[c] = struct{}{}
		}
	}
	for i := range resp.Nodes {
		if _, isChild := childSet[resp.Nodes[i].NodeID]; isChild {
			continue
		}
		if v := visit(&resp.Nodes[i]); v != nil {
			snap.Nodes = append(snap.Nodes, *v)
		}
	}

	// Save on page for snapshot_id lookups.
	p.mu.Lock()
	p.snapshot = snap
	p.mu.Unlock()
	return snap, nil
}

func (p *Page) snapshotDOMText(ctx context.Context) (*Snapshot, error) {
	// Cheap dom_text mode: dump body text via Runtime.evaluate.
	raw, err := p.send(ctx, "Runtime.evaluate", map[string]any{
		"expression":   "document.body && document.body.innerText",
		"returnByValue": true,
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	snap := &Snapshot{
		Nodes: []SnapshotNode{{ID: "#t0", Role: "text", Value: resp.Result.Value}},
		byID:  map[string]*SnapshotNode{},
	}
	snap.byID["#t0"] = &snap.Nodes[0]
	p.mu.Lock()
	p.snapshot = snap
	p.mu.Unlock()
	return snap, nil
}
```

- [ ] **Step 5: Run**

Run: `go test ./internal/browser/ -v`. Expected PASS (golden fixture test passes).

- [ ] **Step 6: Commit**

```bash
git add internal/browser/snapshot.go internal/browser/snapshot_test.go internal/browser/testdata/ax_simple.json
git commit -m "browser: render Accessibility.getFullAXTree to compact snapshot with stable IDs"
```

---

## Task 8: Locator resolver

**Files:**
- Create: `internal/browser/locator.go`
- Create: `internal/browser/locator_test.go`

The `Locator` is a tagged union accepting any of:

```go
type Locator struct {
	Role       string `json:"role,omitempty"`
	Name       string `json:"name,omitempty"`
	Exact      bool   `json:"exact,omitempty"`
	Text       string `json:"text,omitempty"`
	SnapshotID string `json:"snapshot_id,omitempty"`
	CSS        string `json:"css,omitempty"`
	XPath      string `json:"xpath,omitempty"`
}
```

Resolution returns a `backendNodeId`. Resolution order:

1. `snapshot_id` → look up in page's last snapshot's `byID` table.
2. `role+name` → walk the AX tree from the last snapshot (or fetch fresh) and match.
3. `text` → Runtime.evaluate `document.evaluate("//*[contains(normalize-space(.), '<text>')]", ...)`.
4. `css` → `DOM.querySelector` from the document root.
5. `xpath` → Runtime.evaluate.

Multiple matches: return error `ambiguous_locator` with candidates. No matches: `not_found`.

- [ ] **Step 1: Failing test**

`internal/browser/locator_test.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveBySnapshotID(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	// Manually populate snapshot.
	p.snapshot = &Snapshot{
		byID: map[string]*SnapshotNode{
			"#a3": {ID: "#a3", Role: "button", Name: "Submit", BackendNodeID: 999},
		},
	}
	id, err := p.Resolve(context.Background(), Locator{SnapshotID: "#a3"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 999 {
		t.Fatalf("got %d", id)
	}
}

func TestResolveByRoleName(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	p.snapshot = &Snapshot{
		Nodes: []SnapshotNode{
			{ID: "#a0", Role: "WebArea", Children: []SnapshotNode{
				{ID: "#a1", Role: "button", Name: "Sign in", BackendNodeID: 42},
				{ID: "#a2", Role: "button", Name: "Cancel", BackendNodeID: 43},
			}},
		},
	}
	id, err := p.Resolve(context.Background(), Locator{Role: "button", Name: "Sign in"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 42 {
		t.Fatalf("got %d", id)
	}
}

func TestResolveByRoleNameAmbiguous(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	p.snapshot = &Snapshot{
		Nodes: []SnapshotNode{
			{ID: "#a0", Role: "WebArea", Children: []SnapshotNode{
				{ID: "#a1", Role: "button", Name: "Submit", BackendNodeID: 1},
				{ID: "#a2", Role: "button", Name: "Submit", BackendNodeID: 2},
			}},
		},
	}
	_, err := p.Resolve(context.Background(), Locator{Role: "button", Name: "Submit"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous, got %v", err)
	}
}

func TestResolveByCSS(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"DOM.getDocument":   json.RawMessage(`{"root":{"nodeId":1}}`),
			"DOM.querySelector": json.RawMessage(`{"nodeId":77}`),
			"DOM.describeNode":  json.RawMessage(`{"node":{"backendNodeId":777}}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	id, err := p.Resolve(context.Background(), Locator{CSS: "#submit"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 777 {
		t.Fatalf("got %d", id)
	}
}
```

- [ ] **Step 2: Run failure**

Run: `go test ./internal/browser/ -run Resolve -v`. Expected FAIL.

- [ ] **Step 3: Implement**

`internal/browser/locator.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Locator struct {
	Role       string `json:"role,omitempty"`
	Name       string `json:"name,omitempty"`
	Exact      bool   `json:"exact,omitempty"`
	Text       string `json:"text,omitempty"`
	SnapshotID string `json:"snapshot_id,omitempty"`
	CSS        string `json:"css,omitempty"`
	XPath      string `json:"xpath,omitempty"`
}

// Resolve returns a backendNodeId for the locator, or an error.
func (p *Page) Resolve(ctx context.Context, l Locator) (int64, error) {
	switch {
	case l.SnapshotID != "":
		return p.resolveSnapshotID(l.SnapshotID)
	case l.Role != "" || l.Name != "":
		return p.resolveRoleName(ctx, l)
	case l.Text != "":
		return p.resolveText(ctx, l.Text, l.Exact)
	case l.CSS != "":
		return p.resolveCSS(ctx, l.CSS)
	case l.XPath != "":
		return p.resolveXPath(ctx, l.XPath)
	}
	return 0, fmt.Errorf("locator empty")
}

func (p *Page) resolveSnapshotID(id string) (int64, error) {
	p.mu.Lock()
	snap := p.snapshot
	p.mu.Unlock()
	if snap == nil {
		return 0, fmt.Errorf("no snapshot taken yet")
	}
	n, ok := snap.byID[id]
	if !ok {
		return 0, fmt.Errorf("not_found: snapshot id %s", id)
	}
	return n.BackendNodeID, nil
}

func (p *Page) resolveRoleName(ctx context.Context, l Locator) (int64, error) {
	p.mu.Lock()
	snap := p.snapshot
	p.mu.Unlock()
	if snap == nil {
		// Take a fresh snapshot on demand.
		s, err := p.Snapshot(ctx, SnapshotAccessibility)
		if err != nil {
			return 0, err
		}
		snap = s
	}
	var matches []*SnapshotNode
	var walk func([]SnapshotNode)
	walk = func(ns []SnapshotNode) {
		for i := range ns {
			n := &ns[i]
			if (l.Role == "" || n.Role == l.Role) && nameMatch(n.Name, l.Name, l.Exact) {
				matches = append(matches, n)
			}
			walk(n.Children)
		}
	}
	walk(snap.Nodes)
	if len(matches) == 0 {
		return 0, fmt.Errorf("not_found")
	}
	if len(matches) > 1 {
		var cands []string
		for _, m := range matches {
			cands = append(cands, fmt.Sprintf("%s {role:%q,name:%q}", m.ID, m.Role, m.Name))
		}
		return 0, fmt.Errorf("ambiguous_locator: %s", strings.Join(cands, "; "))
	}
	return matches[0].BackendNodeID, nil
}

func nameMatch(got, want string, exact bool) bool {
	if want == "" {
		return true
	}
	if exact {
		return got == want
	}
	return strings.Contains(strings.ToLower(got), strings.ToLower(want))
}

func (p *Page) resolveCSS(ctx context.Context, sel string) (int64, error) {
	rawDoc, err := p.send(ctx, "DOM.getDocument", nil)
	if err != nil {
		return 0, err
	}
	var doc struct {
		Root struct {
			NodeID int64 `json:"nodeId"`
		} `json:"root"`
	}
	if err := json.Unmarshal(rawDoc, &doc); err != nil {
		return 0, err
	}
	rawQ, err := p.send(ctx, "DOM.querySelector", map[string]any{"nodeId": doc.Root.NodeID, "selector": sel})
	if err != nil {
		return 0, err
	}
	var q struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := json.Unmarshal(rawQ, &q); err != nil {
		return 0, err
	}
	if q.NodeID == 0 {
		return 0, fmt.Errorf("not_found: css %s", sel)
	}
	rawD, err := p.send(ctx, "DOM.describeNode", map[string]any{"nodeId": q.NodeID})
	if err != nil {
		return 0, err
	}
	var d struct {
		Node struct {
			BackendNodeID int64 `json:"backendNodeId"`
		} `json:"node"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		return 0, err
	}
	return d.Node.BackendNodeID, nil
}

func (p *Page) resolveText(ctx context.Context, text string, exact bool) (int64, error) {
	expr := fmt.Sprintf(`(function(){
		const xpath = %q;
		const r = document.evaluate(xpath, document, null, XPathResult.ANY_TYPE, null);
		const n = r.iterateNext();
		if (!n) return null;
		const second = r.iterateNext();
		if (second) return "AMBIGUOUS";
		return n;
	})()`, textXPath(text, exact))
	return p.runtimeNode(ctx, expr)
}

func (p *Page) resolveXPath(ctx context.Context, xpath string) (int64, error) {
	expr := fmt.Sprintf(`(function(){
		const r = document.evaluate(%q, document, null, XPathResult.ANY_TYPE, null);
		const n = r.iterateNext();
		if (!n) return null;
		const second = r.iterateNext();
		if (second) return "AMBIGUOUS";
		return n;
	})()`, xpath)
	return p.runtimeNode(ctx, expr)
}

func textXPath(text string, exact bool) string {
	t := strings.ReplaceAll(text, `'`, `\'`)
	if exact {
		return fmt.Sprintf(`//*[normalize-space(.)='%s']`, t)
	}
	return fmt.Sprintf(`//*[contains(normalize-space(.), '%s')]`, t)
}

func (p *Page) runtimeNode(ctx context.Context, expr string) (int64, error) {
	raw, err := p.send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"returnByValue": false,
	})
	if err != nil {
		return 0, err
	}
	var resp struct {
		Result struct {
			Type     string `json:"type"`
			Value    string `json:"value"`
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, err
	}
	if resp.Result.Type == "string" && resp.Result.Value == "AMBIGUOUS" {
		return 0, fmt.Errorf("ambiguous_locator")
	}
	if resp.Result.ObjectID == "" {
		return 0, fmt.Errorf("not_found")
	}
	rawD, err := p.send(ctx, "DOM.requestNode", map[string]any{"objectId": resp.Result.ObjectID})
	if err != nil {
		return 0, err
	}
	var d struct {
		NodeID int64 `json:"nodeId"`
	}
	if err := json.Unmarshal(rawD, &d); err != nil {
		return 0, err
	}
	rawDesc, _ := p.send(ctx, "DOM.describeNode", map[string]any{"nodeId": d.NodeID})
	var desc struct {
		Node struct {
			BackendNodeID int64 `json:"backendNodeId"`
		} `json:"node"`
	}
	_ = json.Unmarshal(rawDesc, &desc)
	return desc.Node.BackendNodeID, nil
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/browser/ -v`. Expected PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/browser/locator.go internal/browser/locator_test.go
git commit -m "browser: add Locator resolver (snapshot_id, role/name, text, css, xpath)"
```

---

## Task 9: Click + Fill

**Files:**
- Create: `internal/browser/interact.go`
- Create: `internal/browser/interact_test.go`

`Click` calls `DOM.scrollIntoViewIfNeeded` then dispatches `Input.dispatchMouseEvent` (mousePressed + mouseReleased) at the element's bounding box center.
`Fill` focuses the element, clears existing value via `Runtime.callFunctionOn` setting `.value=""`, then dispatches `Input.insertText`.

- [ ] **Step 1: Failing test**

`internal/browser/interact_test.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestClickResolvesAndDispatches(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"DOM.getBoxModel":  json.RawMessage(`{"model":{"content":[10,20,30,20,30,40,10,40]}}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	p.snapshot = &Snapshot{byID: map[string]*SnapshotNode{
		"#a1": {ID: "#a1", Role: "button", BackendNodeID: 1},
	}}

	if err := p.Click(context.Background(), Locator{SnapshotID: "#a1"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"DOM.scrollIntoViewIfNeeded": false,
		"DOM.getBoxModel":            false,
		"Input.dispatchMouseEvent":   false,
	}
	for _, c := range f.calls {
		if _, ok := want[c.method]; ok {
			want[c.method] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("missing %s", k)
		}
	}
}

func TestFillResolvesAndInsertsText(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	p.snapshot = &Snapshot{byID: map[string]*SnapshotNode{
		"#a2": {ID: "#a2", Role: "textbox", BackendNodeID: 2},
	}}

	if err := p.Fill(context.Background(), Locator{SnapshotID: "#a2"}, "hello@example.com"); err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, c := range f.calls {
		if c.method == "Input.insertText" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("Input.insertText not sent")
	}
}
```

- [ ] **Step 2: Failure run**

Run: `go test ./internal/browser/ -run "Click|Fill" -v`. Expected FAIL.

- [ ] **Step 3: Implement**

`internal/browser/interact.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

func (p *Page) Click(ctx context.Context, l Locator) error {
	id, err := p.Resolve(ctx, l)
	if err != nil {
		return err
	}
	// Translate backendNodeId to nodeId via DOM.requestChildNodes? Simpler: pushNodesByBackendIdsToFrontend.
	nodeID, err := p.backendToNodeID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := p.send(ctx, "DOM.scrollIntoViewIfNeeded", map[string]any{"nodeId": nodeID}); err != nil {
		return err
	}
	raw, err := p.send(ctx, "DOM.getBoxModel", map[string]any{"nodeId": nodeID})
	if err != nil {
		return err
	}
	var box struct {
		Model struct {
			Content []float64 `json:"content"`
		} `json:"model"`
	}
	if err := json.Unmarshal(raw, &box); err != nil {
		return err
	}
	if len(box.Model.Content) < 8 {
		return fmt.Errorf("box model too small")
	}
	x := (box.Model.Content[0] + box.Model.Content[2] + box.Model.Content[4] + box.Model.Content[6]) / 4
	y := (box.Model.Content[1] + box.Model.Content[3] + box.Model.Content[5] + box.Model.Content[7]) / 4
	for _, t := range []string{"mousePressed", "mouseReleased"} {
		if _, err := p.send(ctx, "Input.dispatchMouseEvent", map[string]any{
			"type": t, "x": x, "y": y, "button": "left", "clickCount": 1,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *Page) Fill(ctx context.Context, l Locator, value string) error {
	id, err := p.Resolve(ctx, l)
	if err != nil {
		return err
	}
	nodeID, err := p.backendToNodeID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := p.send(ctx, "DOM.focus", map[string]any{"nodeId": nodeID}); err != nil {
		return err
	}
	// Clear via JS.
	if _, err := p.send(ctx, "Runtime.evaluate", map[string]any{
		"expression": "document.activeElement && (document.activeElement.value = '')",
	}); err != nil {
		return err
	}
	if _, err := p.send(ctx, "Input.insertText", map[string]any{"text": value}); err != nil {
		return err
	}
	return nil
}

func (p *Page) backendToNodeID(ctx context.Context, backendID int64) (int64, error) {
	raw, err := p.send(ctx, "DOM.pushNodesByBackendIdsToFrontend", map[string]any{
		"backendNodeIds": []int64{backendID},
	})
	if err != nil {
		return 0, err
	}
	var resp struct {
		NodeIDs []int64 `json:"nodeIds"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, err
	}
	if len(resp.NodeIDs) == 0 || resp.NodeIDs[0] == 0 {
		return 0, fmt.Errorf("not_found: backend node %d not in document", backendID)
	}
	return resp.NodeIDs[0], nil
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/browser/ -v`. Expected PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/browser/interact.go internal/browser/interact_test.go
git commit -m "browser: add Click and Fill"
```

---

## Task 10: Hover, SelectOption, PressKey

**Files:**
- Modify: `internal/browser/interact.go`
- Modify: `internal/browser/interact_test.go`

- [ ] **Step 1: Add tests**

For each of `Hover`, `SelectOption(values []string)`, `PressKey(key string)`. Use the same fake sender pattern. Verify the right CDP methods are sent.

- [ ] **Step 2: Run, expect failure**

- [ ] **Step 3: Implement**

```go
func (p *Page) Hover(ctx context.Context, l Locator) error {
	id, err := p.Resolve(ctx, l)
	if err != nil {
		return err
	}
	nodeID, err := p.backendToNodeID(ctx, id)
	if err != nil {
		return err
	}
	raw, err := p.send(ctx, "DOM.getBoxModel", map[string]any{"nodeId": nodeID})
	if err != nil {
		return err
	}
	var box struct{ Model struct{ Content []float64 `json:"content"` } `json:"model"` }
	_ = json.Unmarshal(raw, &box)
	if len(box.Model.Content) < 8 {
		return fmt.Errorf("box model too small")
	}
	x := (box.Model.Content[0] + box.Model.Content[4]) / 2
	y := (box.Model.Content[1] + box.Model.Content[5]) / 2
	_, err = p.send(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type": "mouseMoved", "x": x, "y": y, "button": "none",
	})
	return err
}

func (p *Page) SelectOption(ctx context.Context, l Locator, values []string) error {
	id, err := p.Resolve(ctx, l)
	if err != nil {
		return err
	}
	nodeID, err := p.backendToNodeID(ctx, id)
	if err != nil {
		return err
	}
	// Use Runtime.callFunctionOn with the element's objectId from DOM.resolveNode.
	rawObj, err := p.send(ctx, "DOM.resolveNode", map[string]any{"nodeId": nodeID})
	if err != nil {
		return err
	}
	var obj struct{ Object struct{ ObjectID string `json:"objectId"` } `json:"object"` }
	if err := json.Unmarshal(rawObj, &obj); err != nil {
		return err
	}
	args := make([]map[string]any, 0, len(values))
	for _, v := range values {
		args = append(args, map[string]any{"value": v})
	}
	fn := `function(values) {
		const opts = Array.from(this.options);
		opts.forEach(o => o.selected = values.includes(o.value));
		this.dispatchEvent(new Event('input', {bubbles: true}));
		this.dispatchEvent(new Event('change', {bubbles: true}));
	}`
	_, err = p.send(ctx, "Runtime.callFunctionOn", map[string]any{
		"objectId":            obj.Object.ObjectID,
		"functionDeclaration": fn,
		"arguments":           []map[string]any{{"value": values}},
	})
	return err
}

func (p *Page) PressKey(ctx context.Context, key string) error {
	for _, t := range []string{"keyDown", "keyUp"} {
		if _, err := p.send(ctx, "Input.dispatchKeyEvent", map[string]any{
			"type": t, "key": key,
		}); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/browser/ -v`. Expected PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/browser/interact.go internal/browser/interact_test.go
git commit -m "browser: add Hover, SelectOption, PressKey"
```

---

## Task 11: UploadFile

**Files:**
- Modify: `internal/browser/interact.go`
- Modify: `internal/browser/interact_test.go`

`DOM.setFileInputFiles{nodeId, files: [absolute_paths]}`. The file path is sent as-is — Chrome reads the file from its own filesystem (same machine since we're attached locally).

- [ ] **Step 1: Test (verify CDP method dispatch)**

```go
func TestUploadFile(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	p.snapshot = &Snapshot{byID: map[string]*SnapshotNode{
		"#a3": {ID: "#a3", Role: "textbox", BackendNodeID: 3},
	}}
	if err := p.UploadFile(context.Background(), Locator{SnapshotID: "#a3"}, "/tmp/x.png"); err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, c := range f.calls {
		if c.method == "DOM.setFileInputFiles" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("DOM.setFileInputFiles not sent")
	}
}
```

- [ ] **Step 2: Implement**

```go
func (p *Page) UploadFile(ctx context.Context, l Locator, path string) error {
	id, err := p.Resolve(ctx, l)
	if err != nil {
		return err
	}
	nodeID, err := p.backendToNodeID(ctx, id)
	if err != nil {
		return err
	}
	_, err = p.send(ctx, "DOM.setFileInputFiles", map[string]any{
		"nodeId": nodeID, "files": []string{path},
	})
	return err
}
```

- [ ] **Step 3: Run + Commit**

```bash
git add internal/browser/interact.go internal/browser/interact_test.go
git commit -m "browser: add UploadFile"
```

---

## Task 12: Wire tools — navigate / go_back / go_forward / reload

**Files:**
- Create: `internal/mcp/tools/browser_nav.go`
- Create: `internal/mcp/tools/browser_nav_test.go`
- Modify: `internal/mcp/session.go` — add a `Pages` cache so each tool call reuses the per-target Page.

The session cache holds `map[targetID]*browser.Page`. First touch attaches; subsequent touches reuse. Tool needs `target_id` argument; if missing, falls back to `sess.ActiveTarget()`.

- [ ] **Step 1: Modify Session for Page cache**

Add to `internal/mcp/session.go`:

```go
import (
	// ...
	"github.com/pavankumar2138/netra-browser/internal/browser"
)

// in Session struct:
pages map[string]*browser.Page

// in NewSession():
return &Session{pages: map[string]*browser.Page{}}

// New methods:
func (s *Session) Page(ctx context.Context, targetID string) (*browser.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil, fmt.Errorf("not attached")
	}
	if p, ok := s.pages[targetID]; ok {
		return p, nil
	}
	// Need a Sender that satisfies browser.Sender. The existing CDPSender is method+close only.
	// Add a second narrow interface assertion.
	bs, ok := s.client.(browser.Sender)
	if !ok {
		return nil, fmt.Errorf("CDP client missing browser.Sender capabilities")
	}
	p, err := browser.NewPage(ctx, bs, targetID)
	if err != nil {
		return nil, err
	}
	s.pages[targetID] = p
	return p, nil
}

func (s *Session) DropPage(targetID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pages, targetID)
}
```

Note: `*cdp.Client` already has `Send`, `SendOnTarget`, `AttachToTarget`, `SubscribeOnTarget` after Plan B Task 1, so it satisfies `browser.Sender`.

Also update `Session.Clear()` to clear `s.pages`.

- [ ] **Step 2: Tests for navigate tool**

`internal/mcp/tools/browser_nav_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

// fakeFullSender is the test double satisfying browser.Sender.
type fakeFullSender struct {
	calls   []string
	results map[string]json.RawMessage
}
func (f *fakeFullSender) Send(ctx context.Context, m string, _ any) (json.RawMessage, error) {
	f.calls = append(f.calls, m)
	if r, ok := f.results[m]; ok { return r, nil }
	return json.RawMessage(`{}`), nil
}
func (f *fakeFullSender) SendOnTarget(ctx context.Context, _, m string, _ any) (json.RawMessage, error) {
	f.calls = append(f.calls, m)
	if r, ok := f.results[m]; ok { return r, nil }
	return json.RawMessage(`{}`), nil
}
func (f *fakeFullSender) AttachToTarget(ctx context.Context, t string) (string, error) {
	return "S-" + t, nil
}
func (f *fakeFullSender) SubscribeOnTarget(_, _ string) chan cdp.BufferedEvent {
	ch := make(chan cdp.BufferedEvent, 1)
	ch <- cdp.BufferedEvent{Method: "Page.loadEventFired"}
	return ch
}
func (f *fakeFullSender) Close() error { return nil }

func TestNavigateTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Page.navigate": json.RawMessage(`{"frameId":"F1"}`),
		},
	})
	sess.SetActiveTarget("T1")

	reg := mcp.NewRegistry()
	RegisterBrowserNav(reg, sess)

	out, err := reg.Invoke(context.Background(), "browser_navigate", json.RawMessage(`{"url":"https://example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"url":"https://example.com"`) {
		t.Fatalf("missing url: %s", b)
	}
}
```

- [ ] **Step 3: Implement**

`internal/mcp/tools/browser_nav.go`:

```go
package tools

import (
	"context"
	"encoding/json"

	"github.com/pavankumar2138/netra-browser/internal/browser"
	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

func RegisterBrowserNav(reg *mcp.Registry, sess *mcp.Session) {
	type navArgs struct {
		URL       string `json:"url"`
		TargetID  string `json:"target_id"`
		WaitUntil string `json:"wait_until"`
	}
	reg.Register("browser_navigate", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a navArgs
		if len(params) > 0 {
			if err := json.Unmarshal(params, &a); err != nil {
				return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
			}
		}
		if a.URL == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "url required"}.AsResult(), nil
		}
		tid := a.TargetID
		if tid == "" {
			tid = sess.ActiveTarget()
		}
		if tid == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target_id and no active target"}.AsResult(), nil
		}
		page, err := sess.Page(ctx, tid)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult(), nil
		}
		opts := browser.NavigateOpts{URL: a.URL, WaitUntil: browser.WaitUntil(a.WaitUntil)}
		res, err := page.Navigate(ctx, opts)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		// Always return snapshot for navigate.
		snap, _ := page.Snapshot(ctx, browser.SnapshotAccessibility)
		return map[string]any{
			"ok":       true,
			"url":      res.URL,
			"title":    "", // titles fetched from snapshot in Plan C
			"snapshot": snap.Nodes,
		}, nil
	})

	for _, name := range []string{"browser_go_back", "browser_go_forward", "browser_reload"} {
		name := name
		reg.Register(name, func(ctx context.Context, params json.RawMessage) (any, error) {
			var a struct {
				TargetID       string `json:"target_id"`
				ReturnSnapshot bool   `json:"return_snapshot"`
			}
			if len(params) > 0 {
				_ = json.Unmarshal(params, &a)
			}
			tid := a.TargetID
			if tid == "" {
				tid = sess.ActiveTarget()
			}
			if tid == "" {
				return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target"}.AsResult(), nil
			}
			page, err := sess.Page(ctx, tid)
			if err != nil {
				return mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult(), nil
			}
			switch name {
			case "browser_go_back":
				err = page.GoBack(ctx)
			case "browser_go_forward":
				err = page.GoForward(ctx)
			case "browser_reload":
				err = page.Reload(ctx)
			}
			if err != nil {
				return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
			}
			out := map[string]any{"ok": true}
			if a.ReturnSnapshot {
				snap, _ := page.Snapshot(ctx, browser.SnapshotAccessibility)
				out["snapshot"] = snap.Nodes
			}
			return out, nil
		})
	}
}
```

- [ ] **Step 4: Wire main.go**

Modify `cmd/netra-browser/main.go` — add `tools.RegisterBrowserNav(reg, sess)` after the existing registrations.

- [ ] **Step 5: Run + commit**

Run: `go test ./...`. Expected: all PASS.

```bash
git add internal/mcp/session.go internal/mcp/tools/browser_nav.go internal/mcp/tools/browser_nav_test.go cmd/netra-browser/main.go
git commit -m "tools: add browser_navigate/go_back/go_forward/reload + Page cache"
```

---

## Task 13: Wire tools — snapshot + interact

**Files:**
- Create: `internal/mcp/tools/browser_inspect.go` (snapshot only — screenshot/eval go in Plan C)
- Create: `internal/mcp/tools/browser_interact.go`
- Create: `internal/mcp/tools/browser_inspect_test.go`
- Create: `internal/mcp/tools/browser_interact_test.go`

`browser_snapshot` returns `{ok, snapshot: <tree>}`. `browser_click/fill/hover/select_option/press_key/upload_file` resolve via session.Page and call the matching method.

- [ ] **Step 1: Tests for snapshot tool**

Test that `browser_snapshot` returns a non-empty `snapshot` array when an Accessibility.getFullAXTree response is mocked.

- [ ] **Step 2: Tests for interact tools**

Each tool takes a `locator` field. Test that it accepts `{"locator":{"snapshot_id":"#a1"}}` and dispatches the right CDP method on the fake sender.

- [ ] **Step 3: Implement**

`internal/mcp/tools/browser_inspect.go`:

```go
package tools

import (
	"context"
	"encoding/json"

	"github.com/pavankumar2138/netra-browser/internal/browser"
	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

func RegisterBrowserInspect(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_snapshot", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TargetID string `json:"target_id"`
			Mode     string `json:"mode"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &a)
		}
		tid := a.TargetID
		if tid == "" {
			tid = sess.ActiveTarget()
		}
		if tid == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target"}.AsResult(), nil
		}
		page, err := sess.Page(ctx, tid)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult(), nil
		}
		mode := browser.SnapshotMode(a.Mode)
		if mode == "" {
			mode = browser.SnapshotAccessibility
		}
		snap, err := page.Snapshot(ctx, mode)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "snapshot": snap.Nodes}, nil
	})
}
```

`internal/mcp/tools/browser_interact.go`:

```go
package tools

import (
	"context"
	"encoding/json"

	"github.com/pavankumar2138/netra-browser/internal/browser"
	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

func RegisterBrowserInteract(reg *mcp.Registry, sess *mcp.Session) {
	type baseArgs struct {
		Locator        browser.Locator `json:"locator"`
		TargetID       string          `json:"target_id"`
		ReturnSnapshot bool            `json:"return_snapshot"`
	}
	resolvePage := func(ctx context.Context, tid string) (*browser.Page, *map[string]any) {
		if tid == "" {
			tid = sess.ActiveTarget()
		}
		if tid == "" {
			r := mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target"}.AsResult()
			return nil, &r
		}
		page, err := sess.Page(ctx, tid)
		if err != nil {
			r := mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult()
			return nil, &r
		}
		return page, nil
	}
	wrap := func(page *browser.Page, ctx context.Context, ret bool, err error) any {
		if err != nil {
			ec := mcp.ErrChromeDisconnected
			msg := err.Error()
			if msg == "ambiguous_locator" || (len(msg) >= 17 && msg[:17] == "ambiguous_locator") {
				ec = mcp.ErrAmbiguousLocator
			} else if msg == "not_found" || (len(msg) >= 9 && msg[:9] == "not_found") {
				ec = mcp.ErrNotFound
			}
			return mcp.ToolError{Code: ec, Message: msg}.AsResult()
		}
		out := map[string]any{"ok": true}
		if ret {
			snap, _ := page.Snapshot(ctx, browser.SnapshotAccessibility)
			out["snapshot"] = snap.Nodes
		}
		return out
	}

	reg.Register("browser_click", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a baseArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil { return *errR, nil }
		return wrap(page, ctx, a.ReturnSnapshot, page.Click(ctx, a.Locator)), nil
	})

	type fillArgs struct {
		baseArgs
		Value string `json:"value"`
	}
	reg.Register("browser_fill", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a fillArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil { return *errR, nil }
		return wrap(page, ctx, a.ReturnSnapshot, page.Fill(ctx, a.Locator, a.Value)), nil
	})

	reg.Register("browser_hover", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a baseArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil { return *errR, nil }
		return wrap(page, ctx, false, page.Hover(ctx, a.Locator)), nil
	})

	type selArgs struct {
		baseArgs
		Values []string `json:"values"`
	}
	reg.Register("browser_select_option", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a selArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil { return *errR, nil }
		return wrap(page, ctx, false, page.SelectOption(ctx, a.Locator, a.Values)), nil
	})

	type keyArgs struct {
		Key      string `json:"key"`
		TargetID string `json:"target_id"`
	}
	reg.Register("browser_press_key", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a keyArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil { return *errR, nil }
		return wrap(page, ctx, false, page.PressKey(ctx, a.Key)), nil
	})

	type uploadArgs struct {
		baseArgs
		FilePath string `json:"file_path"`
	}
	reg.Register("browser_upload_file", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a uploadArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil { return *errR, nil }
		return wrap(page, ctx, false, page.UploadFile(ctx, a.Locator, a.FilePath)), nil
	})
}
```

- [ ] **Step 4: Wire main.go**

Add `tools.RegisterBrowserInspect(reg, sess)` and `tools.RegisterBrowserInteract(reg, sess)` to `cmd/netra-browser/main.go`.

- [ ] **Step 5: Run + commit**

Run: `go test ./...`. Expected: all PASS.

```bash
git add internal/mcp/tools/browser_inspect.go internal/mcp/tools/browser_interact.go internal/mcp/tools/browser_inspect_test.go internal/mcp/tools/browser_interact_test.go cmd/netra-browser/main.go
git commit -m "tools: add browser_snapshot + interaction tools (click/fill/hover/select/key/upload)"
```

---

## Task 14: E2E forms test

**Files:**
- Create: `e2e/forms_test.go` (build tag `e2e`)
- Create: `e2e/testdata/form.html`

The test serves a simple HTML form via `httptest`, attaches the bridge, navigates to the form, snapshots, fills fields, clicks submit, verifies the page navigates to a `/submitted` path with the right query string.

- [ ] **Step 1: Create fixture**

`e2e/testdata/form.html`:

```html
<!doctype html>
<html><body>
  <h1>Login</h1>
  <form action="/submitted" method="get">
    <label>Email <input name="email" type="text" id="email"></label>
    <label>Password <input name="password" type="password" id="password"></label>
    <button type="submit" id="submit">Sign in</button>
  </form>
</body></html>
```

- [ ] **Step 2: Test**

`e2e/forms_test.go`:

```go
//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestE2E_FormSubmission(t *testing.T) {
	chrome := findChrome(t)
	port := freePort(t)
	userDir := t.TempDir()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "testdata/form.html")
	})
	mux.HandleFunc("/submitted", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "OK email=%s pw=%s", r.URL.Query().Get("email"), r.URL.Query().Get("password"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromeCmd := exec.Command(chrome,
		"--headless=new", "--no-sandbox", "--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+userDir, "about:blank",
	)
	chromeCmd.Stderr = os.Stderr
	if err := chromeCmd.Start(); err != nil { t.Fatal(err) }
	defer chromeCmd.Process.Kill()
	waitForChrome(t, port, 10*time.Second)

	lockPath := filepath.Join(userDir, "active.lock")
	bin := exec.Command("go", "run", "../cmd/netra-browser",
		"--lock", lockPath,
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	bin.Stderr = os.Stderr
	if err := bin.Start(); err != nil { t.Fatal(err) }
	defer bin.Process.Kill()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	send := func(id int, method string, params any) map[string]any {
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil { req["params"] = params }
		b, _ := json.Marshal(req)
		_, _ = io.WriteString(stdin, string(b)+"\n")
		if !scanner.Scan() { t.Fatal("no resp") }
		var r map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &r)
		return r
	}

	// attach
	r := send(1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	if r["result"].(map[string]any)["ok"] != true { t.Fatalf("attach: %v", r) }

	// new tab on the form URL
	r = send(2, "browser_new_tab", map[string]any{"url": srv.URL + "/"})
	tid := r["result"].(map[string]any)["target_id"].(string)

	// navigate (idempotent — tab opened with url already, but re-navigate to anchor session)
	r = send(3, "browser_navigate", map[string]any{"target_id": tid, "url": srv.URL + "/", "wait_until": "load"})
	res := r["result"].(map[string]any)
	if res["ok"] != true { t.Fatalf("navigate: %v", r) }

	// fill email
	r = send(4, "browser_fill", map[string]any{
		"target_id": tid,
		"locator":   map[string]any{"css": "#email"},
		"value":     "alice@example.com",
	})
	if r["result"].(map[string]any)["ok"] != true { t.Fatalf("fill email: %v", r) }

	// fill password
	r = send(5, "browser_fill", map[string]any{
		"target_id": tid,
		"locator":   map[string]any{"css": "#password"},
		"value":     "hunter2",
	})
	if r["result"].(map[string]any)["ok"] != true { t.Fatalf("fill pw: %v", r) }

	// click submit
	r = send(6, "browser_click", map[string]any{
		"target_id": tid,
		"locator":   map[string]any{"css": "#submit"},
	})
	if r["result"].(map[string]any)["ok"] != true { t.Fatalf("click: %v", r) }

	// give navigation time then snapshot to confirm page changed
	time.Sleep(500 * time.Millisecond)
	r = send(7, "browser_snapshot", map[string]any{"target_id": tid, "mode": "dom_text"})
	res = r["result"].(map[string]any)
	snap := res["snapshot"].([]any)
	if len(snap) == 0 { t.Fatal("empty snapshot") }
	body := snap[0].(map[string]any)
	if body["value"] == nil || !contains(body["value"].(string), "alice@example.com") {
		t.Fatalf("submission body missing email: %+v", body)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle { return true }
	}
	return false
}
```

- [ ] **Step 3: Run**

Run: `go test -tags e2e ./e2e/... -v -timeout 180s`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add e2e/forms_test.go e2e/testdata/form.html
git commit -m "e2e: add form submission flow (navigate + fill + click)"
```

---

## Task 15: Final verify + tag

- [ ] **Step 1: Full test suite**

```bash
cd /home/mrwhitehat/ClaudePlaywright
go test ./...
go test -tags e2e ./e2e/... -timeout 180s
go vet ./...
gofmt -l . | grep -v '^docs/' | grep -v '^BRAINSTORM' || echo "all formatted"
```

Expected: everything passes.

- [ ] **Step 2: Tag**

```bash
git tag plan-b-core-browsing
```

- [ ] **Step 3: Verify tools live**

Plan B adds 11 tools to the 7 from Plan A:

- `browser_navigate`, `browser_go_back`, `browser_go_forward`, `browser_reload`
- `browser_snapshot`
- `browser_click`, `browser_fill`, `browser_hover`, `browser_select_option`, `browser_press_key`, `browser_upload_file`

Total live: **18 tools**.

---

## Self-Review Notes

**Spec coverage:**
- ✅ Per-target sessions (Task 1).
- ✅ Page abstraction with domain enable (Task 2).
- ✅ Navigate + wait_until=load/domcontentloaded (Tasks 3-4).
- ✅ Navigate + wait_until=networkidle (Task 5).
- ✅ go_back/go_forward/reload (Task 6).
- ✅ Snapshot (accessibility + dom_text modes) (Task 7).
- ✅ Locator system (snapshot_id, role/name, text, css, xpath) (Task 8).
- ✅ Click, Fill, Hover, SelectOption, PressKey, UploadFile (Tasks 9-11).
- ✅ All 11 MCP tools wired (Tasks 12-13).
- ✅ E2E proof of forms flow (Task 14).
- ⏭ wait_for / get_recent_events / dialogs / screenshot / eval / cookies → Plan C.
- ⏭ HTTP-SSE, launch mode, sessions → Plan D.
- ⏭ High-level tasks → Plan E.

**Placeholder scan:** No TBD/TODO in actionable steps. The `WaitNetworkIdle` branch in `Navigate` is fully wired, not a stub.

**Type consistency check:**
- `browser.Sender` interface has `Send/SendOnTarget/AttachToTarget/SubscribeOnTarget`. `*cdp.Client` satisfies all four after Plan B Task 1 lands `AttachToTarget`/`SendOnTarget`/`SubscribeOnTarget`.
- `Locator` shape is consistent across `browser.Locator` (Task 8), tool input parsing (Task 13), and the resolution functions (Task 8).
- `SnapshotNode` fields and `Snapshot.byID` table are populated in Task 7 and consumed in Task 8.
- `mcp.Session.Page(ctx, targetID)` (Task 12) is consumed by Tasks 12 and 13 tools.
- Error codes in Task 13's `wrap` helper map to existing `mcp.Err*` constants from Plan A.

**Known limitation deferred to Plan C:**
- Tasks 12/13 don't implement timeout per call. Plan C's wait_for + dialog work is the right place to add a `timeout_ms` argument across all tools.

**Open ends to flag in commit messages or follow-ups:**
- `Snapshot.byID` is populated but the lookup race with `Page.Snapshot` writes isn't addressed. Acceptable single-threaded MCP tool flow.
- `SubscribeOnTarget` channel never closes. `Page` instances are long-lived, so this leaks event-source goroutines. Cleanup in Plan D when launch-mode adds proper teardown.
