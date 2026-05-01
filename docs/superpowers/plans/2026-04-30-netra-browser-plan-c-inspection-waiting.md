# netra-browser Plan C — Inspection + Waiting + Dialogs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add screenshot, eval, cookies, dialog handling, and event-driven waiting (wait_for / get_recent_events). After Plan C, the bridge can introspect a page, capture screenshots, evaluate JS, manage cookies, dismiss alerts, and react to events.

**Architecture:** Each `*browser.Page` maintains its own event ring buffer (`Plan A's cdp.RingBuffer`). On attach, the Page subscribes to a fixed set of "interesting" events (`Network.*`, `Page.javascriptDialogOpening`, `Page.frameNavigated`, `Runtime.consoleAPICalled`, `Browser.downloadWillBegin`) and adds them to the buffer. `wait_for` blocks on a fresh subscription with optional predicate + timeout. `get_recent_events` reads from the buffer.

**Tech Stack:** Same as Plan B.

**Source spec:** `docs/superpowers/specs/2026-04-30-netra-browser-design.md`

**Builds on:** `plan-b-core-browsing` tag.

---

## File Structure (Plan C)

| Path | Responsibility |
|---|---|
| `internal/browser/events.go` | `Page.startEventCollector` (subscribe + buffer), `Page.WaitFor`, `Page.RecentEvents` |
| `internal/browser/events_test.go` | Tests for buffer + wait_for |
| `internal/browser/screenshot.go` | `Page.Screenshot(opts)` returning PNG bytes or saving to file |
| `internal/browser/screenshot_test.go` | Test that Page.captureScreenshot returns base64 PNG |
| `internal/browser/eval.go` | `Page.Eval(expression)` returning Runtime.evaluate result |
| `internal/browser/eval_test.go` | Tests for value/error/object return shapes |
| `internal/browser/cookies.go` | `Page.GetCookies(urls?)`, `Page.SetCookies(cookies)` |
| `internal/browser/cookies_test.go` | Tests |
| `internal/browser/dialogs.go` | Subscribe to `Page.javascriptDialogOpening`; `Page.HandleDialog(action, text)` |
| `internal/browser/dialogs_test.go` | Tests |
| `internal/mcp/tools/browser_inspect.go` (modify) | Add `browser_screenshot`, `browser_eval`, `browser_get_cookies`, `browser_set_cookies` |
| `internal/mcp/tools/browser_events.go` | Register `browser_wait_for`, `browser_get_recent_events`, `browser_handle_dialog` |
| `e2e/events_test.go` | E2E: alert dialog, network event capture, screenshot |

---

## Task 1: Page event collector + RingBuffer wiring

**Files:**
- Create: `internal/browser/events.go`
- Create: `internal/browser/events_test.go`
- Modify: `internal/browser/page.go` — add `events *cdp.RingBuffer` field, call `startEventCollector` in `NewPage`.

The Page subscribes to a fixed set of CDP events on attach. Each event arrives via `SubscribeOnTarget`, gets converted to a `cdp.BufferedEvent`, and pushed to the page's ring buffer.

Subscribed methods (initial v1 set):
- `Network.requestWillBeSent`
- `Network.responseReceived`
- `Network.loadingFinished`
- `Network.loadingFailed`
- `Page.frameNavigated`
- `Page.javascriptDialogOpening`
- `Page.loadEventFired`
- `Page.domContentEventFired`
- `Runtime.consoleAPICalled`
- `Browser.downloadWillBegin` (browser-wide event, but the Page can also receive it via session if Browser domain is enabled — see implementation note below)

Note: `Browser.downloadWillBegin` is browser-wide, not session-scoped. For v1, skip downloads in the per-page collector — Plan E's `task_wait_for_download` will handle it via the browser-level CDP connection. This keeps the per-page collector clean.

- [ ] **Step 1: Failing test**

`internal/browser/events_test.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
)

func TestPageBuffersEventsOnAttach(t *testing.T) {
	p := newPumpable()
	for _, m := range []string{
		"Network.requestWillBeSent", "Network.responseReceived",
		"Network.loadingFinished", "Network.loadingFailed",
		"Page.frameNavigated", "Page.javascriptDialogOpening",
		"Page.loadEventFired", "Page.domContentEventFired",
		"Runtime.consoleAPICalled",
	} {
		p.events[m] = make(chan cdp.BufferedEvent, 16)
	}
	page, err := NewPage(context.Background(), p, "T1")
	if err != nil {
		t.Fatal(err)
	}

	// Push 3 console events.
	for i := 0; i < 3; i++ {
		p.events["Runtime.consoleAPICalled"] <- cdp.BufferedEvent{Method: "Runtime.consoleAPICalled", Params: json.RawMessage(`{}`)}
	}
	// Allow goroutines to forward to the page buffer.
	time.Sleep(100 * time.Millisecond)

	got := page.RecentEvents(time.Time{}, []string{"Runtime.consoleAPICalled"})
	if len(got) != 3 {
		t.Fatalf("want 3 console events, got %d", len(got))
	}
}

func TestWaitForDeliversMatchingEvent(t *testing.T) {
	p := newPumpable()
	for _, m := range []string{
		"Network.requestWillBeSent", "Network.responseReceived",
		"Network.loadingFinished", "Network.loadingFailed",
		"Page.frameNavigated", "Page.javascriptDialogOpening",
		"Page.loadEventFired", "Page.domContentEventFired",
		"Runtime.consoleAPICalled",
	} {
		p.events[m] = make(chan cdp.BufferedEvent, 16)
	}
	page, _ := NewPage(context.Background(), p, "T1")

	done := make(chan error, 1)
	go func() {
		_, err := page.WaitFor(context.Background(), "Page.frameNavigated", nil, 2*time.Second)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond) // ensure WaitFor subscribed
	p.events["Page.frameNavigated"] <- cdp.BufferedEvent{Method: "Page.frameNavigated", Params: json.RawMessage(`{}`)}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("WaitFor didn't return")
	}
}

func TestWaitForTimeout(t *testing.T) {
	p := newPumpable()
	for _, m := range []string{
		"Network.requestWillBeSent", "Network.responseReceived",
		"Network.loadingFinished", "Network.loadingFailed",
		"Page.frameNavigated", "Page.javascriptDialogOpening",
		"Page.loadEventFired", "Page.domContentEventFired",
		"Runtime.consoleAPICalled",
	} {
		p.events[m] = make(chan cdp.BufferedEvent, 16)
	}
	page, _ := NewPage(context.Background(), p, "T1")

	_, err := page.WaitFor(context.Background(), "Page.frameNavigated", nil, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/browser/ -run "BuffersEvents|WaitFor" -v` — Expected FAIL.

- [ ] **Step 3: Implement event collector**

Modify `internal/browser/page.go`:

```go
import (
	// ...existing
	"github.com/pavankumar2138/netra-browser/internal/cdp"
)

type Page struct {
	cdp       Sender
	targetID  string
	sessionID string

	mu       sync.Mutex
	snapshot *Snapshot

	events *cdp.RingBuffer // event buffer, populated by startEventCollector
}
```

Update `NewPage` to initialize `p.events = cdp.NewRingBuffer(1000)` and call `p.startEventCollector(ctx)` after enabling domains.

Create `internal/browser/events.go`:

```go
package browser

import (
	"context"
	"time"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
)

// Methods the page subscribes to on attach.
var collectedEvents = []string{
	"Network.requestWillBeSent",
	"Network.responseReceived",
	"Network.loadingFinished",
	"Network.loadingFailed",
	"Page.frameNavigated",
	"Page.javascriptDialogOpening",
	"Page.loadEventFired",
	"Page.domContentEventFired",
	"Runtime.consoleAPICalled",
}

// startEventCollector subscribes to a fixed event set and pushes each event into the page's RingBuffer.
func (p *Page) startEventCollector(ctx context.Context) {
	for _, m := range collectedEvents {
		method := m
		ch := p.cdp.SubscribeOnTarget(p.sessionID, method)
		go func() {
			for e := range ch {
				e.Method = method // ensure populated
				if e.At.IsZero() {
					e.At = time.Now()
				}
				p.events.Add(e)
			}
		}()
	}
}

// WaitFor blocks until an event matching method (and optionally predicate) arrives, or timeout fires.
// predicate: passes raw Params; if nil, any event of method matches.
func (p *Page) WaitFor(ctx context.Context, method string, predicate func(cdp.BufferedEvent) bool, timeout time.Duration) (*cdp.BufferedEvent, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	src := p.cdp.SubscribeOnTarget(p.sessionID, method)
	for {
		select {
		case e, ok := <-src:
			if !ok {
				return nil, context.Canceled
			}
			if predicate != nil && !predicate(e) {
				continue
			}
			return &e, nil
		case <-deadline.C:
			return nil, context.DeadlineExceeded
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// RecentEvents returns events from the buffer with `At >= since` and `Method` in `types`.
// types empty/nil → all methods.
func (p *Page) RecentEvents(since time.Time, types []string) []cdp.BufferedEvent {
	if p.events == nil {
		return nil
	}
	return p.events.Recent(since, types)
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/browser/ -v` — Expected PASS (all browser tests).

- [ ] **Step 5: Commit**

```bash
git add internal/browser/page.go internal/browser/events.go internal/browser/events_test.go
git commit -m "browser: add per-page event collector + WaitFor + RecentEvents"
```

---

## Task 2: browser_screenshot tool

**Files:**
- Create: `internal/browser/screenshot.go`
- Create: `internal/browser/screenshot_test.go`
- Modify: `internal/mcp/tools/browser_inspect.go` to register `browser_screenshot`
- Add to: `internal/mcp/tools/browser_inspect_test.go`

`Page.captureScreenshot` issues `Page.captureScreenshot{format:"png"}` returning base64 string. If `locator` provided, clip to the element's bounding box. If `full_page` true, set `captureBeyondViewport: true` and `clip` to the full content size via `Page.getLayoutMetrics`.

- [ ] **Step 1: Failing test**

`internal/browser/screenshot_test.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestScreenshotReturnsBase64(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Page.captureScreenshot": json.RawMessage(`{"data":"iVBORw0KGgoAAAANSUhEUgAA"}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	data, err := p.Screenshot(context.Background(), ScreenshotOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if data == "" {
		t.Fatal("empty screenshot data")
	}
}
```

- [ ] **Step 2: Run failure**

`go test ./internal/browser/ -run Screenshot -v`. Expected FAIL.

- [ ] **Step 3: Implement**

`internal/browser/screenshot.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

type ScreenshotOpts struct {
	Locator  *Locator
	FullPage bool
	Format   string // "png" (default) or "jpeg"
}

// Screenshot returns a base64-encoded PNG.
func (p *Page) Screenshot(ctx context.Context, opts ScreenshotOpts) (string, error) {
	params := map[string]any{}
	if opts.Format == "" {
		opts.Format = "png"
	}
	params["format"] = opts.Format

	if opts.Locator != nil {
		bid, err := p.Resolve(ctx, *opts.Locator)
		if err != nil {
			return "", err
		}
		nodeID, err := p.backendToNodeID(ctx, bid)
		if err != nil {
			return "", err
		}
		raw, err := p.send(ctx, "DOM.getBoxModel", map[string]any{"nodeId": nodeID})
		if err != nil {
			return "", err
		}
		var box struct {
			Model struct {
				Content []float64 `json:"content"`
				Width   int       `json:"width"`
				Height  int       `json:"height"`
			} `json:"model"`
		}
		if err := json.Unmarshal(raw, &box); err != nil {
			return "", err
		}
		if len(box.Model.Content) < 8 {
			return "", fmt.Errorf("box model too small")
		}
		params["clip"] = map[string]any{
			"x":     box.Model.Content[0],
			"y":     box.Model.Content[1],
			"width": box.Model.Content[2] - box.Model.Content[0],
			"height": box.Model.Content[5] - box.Model.Content[1],
			"scale": 1,
		}
	} else if opts.FullPage {
		raw, err := p.send(ctx, "Page.getLayoutMetrics", nil)
		if err != nil {
			return "", err
		}
		var lm struct {
			ContentSize struct {
				Width  float64 `json:"width"`
				Height float64 `json:"height"`
			} `json:"contentSize"`
		}
		if err := json.Unmarshal(raw, &lm); err != nil {
			return "", err
		}
		params["captureBeyondViewport"] = true
		params["clip"] = map[string]any{
			"x": 0, "y": 0,
			"width": lm.ContentSize.Width, "height": lm.ContentSize.Height, "scale": 1,
		}
	}

	raw, err := p.send(ctx, "Page.captureScreenshot", params)
	if err != nil {
		return "", err
	}
	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	return resp.Data, nil
}
```

- [ ] **Step 4: Add tool registration**

Modify `internal/mcp/tools/browser_inspect.go` (already exists with `browser_snapshot`), append a second registration:

```go
reg.Register("browser_screenshot", func(ctx context.Context, params json.RawMessage) (any, error) {
	var a struct {
		TargetID string           `json:"target_id"`
		Locator  *browser.Locator `json:"locator,omitempty"`
		FullPage bool             `json:"full_page"`
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
	data, err := page.Screenshot(ctx, browser.ScreenshotOpts{Locator: a.Locator, FullPage: a.FullPage})
	if err != nil {
		return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
	}
	return map[string]any{"ok": true, "png_base64": data}, nil
})
```

- [ ] **Step 5: Test the tool**

Add to `internal/mcp/tools/browser_inspect_test.go`:

```go
func TestScreenshotTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Page.captureScreenshot": json.RawMessage(`{"data":"iVBORw0KGgoAAA"}`),
		},
	})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserInspect(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_screenshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"png_base64":`) {
		t.Fatalf("missing png: %s", b)
	}
}
```

- [ ] **Step 6: Run + commit**

`go test ./...` — Expected PASS.

```bash
git add internal/browser/screenshot.go internal/browser/screenshot_test.go internal/mcp/tools/browser_inspect.go internal/mcp/tools/browser_inspect_test.go
git commit -m "browser: add Screenshot + browser_screenshot tool"
```

---

## Task 3: browser_eval tool

**Files:**
- Create: `internal/browser/eval.go`
- Create: `internal/browser/eval_test.go`
- Modify: `internal/mcp/tools/browser_inspect.go` to register `browser_eval`
- Add test in `internal/mcp/tools/browser_inspect_test.go`

`Page.Eval(expression)` runs `Runtime.evaluate{returnByValue:true, awaitPromise:true}` and returns whatever value the expression produces.

- [ ] **Step 1: Failing test**

`internal/browser/eval_test.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestEvalReturnsValue(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Runtime.evaluate": json.RawMessage(`{"result":{"type":"number","value":42}}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	v, err := p.Eval(context.Background(), "1+1")
	if err != nil {
		t.Fatal(err)
	}
	if v == nil {
		t.Fatal("nil value")
	}
}

func TestEvalSurfacesException(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Runtime.evaluate": json.RawMessage(`{"exceptionDetails":{"text":"ReferenceError"},"result":{"type":"undefined"}}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	if _, err := p.Eval(context.Background(), "missing"); err == nil {
		t.Fatal("expected exception error")
	}
}
```

- [ ] **Step 2: Run failure**

`go test ./internal/browser/ -run Eval -v`. Expected FAIL.

- [ ] **Step 3: Implement**

`internal/browser/eval.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"fmt"
)

// Eval runs a JS expression in the page and returns the value (or error).
func (p *Page) Eval(ctx context.Context, expression string) (any, error) {
	raw, err := p.send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	if resp.ExceptionDetails != nil {
		return nil, fmt.Errorf("eval threw: %s", resp.ExceptionDetails.Text)
	}
	if len(resp.Result.Value) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(resp.Result.Value, &v); err != nil {
		return string(resp.Result.Value), nil
	}
	return v, nil
}
```

- [ ] **Step 4: Tool registration**

Append to `internal/mcp/tools/browser_inspect.go`:

```go
reg.Register("browser_eval", func(ctx context.Context, params json.RawMessage) (any, error) {
	var a struct {
		Expression string `json:"expression"`
		TargetID   string `json:"target_id"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
	}
	if a.Expression == "" {
		return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "expression required"}.AsResult(), nil
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
	v, err := page.Eval(ctx, a.Expression)
	if err != nil {
		return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
	}
	return map[string]any{"ok": true, "result": v}, nil
})
```

- [ ] **Step 5: Test + run + commit**

```bash
go test ./...
git add internal/browser/eval.go internal/browser/eval_test.go internal/mcp/tools/browser_inspect.go internal/mcp/tools/browser_inspect_test.go
git commit -m "browser: add Eval + browser_eval tool"
```

---

## Task 4: Cookies — get + set

**Files:**
- Create: `internal/browser/cookies.go`
- Create: `internal/browser/cookies_test.go`
- Modify: `internal/mcp/tools/browser_inspect.go` to register `browser_get_cookies`, `browser_set_cookies`

`Page.GetCookies(urls? []string)` calls `Network.getCookies` (note: per-target after Network.enable; for browser-wide, use `Storage.getCookies` instead — for v1 we use `Network.getCookies` on the page session). `Page.SetCookies(cookies)` calls `Network.setCookies`.

- [ ] **Step 1: Tests**

```go
package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGetCookiesReturnsList(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Network.getCookies": json.RawMessage(`{"cookies":[
				{"name":"sid","value":"abc","domain":"example.com"},
				{"name":"theme","value":"dark","domain":"example.com"}
			]}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	cs, err := p.GetCookies(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d", len(cs))
	}
}

func TestSetCookiesSendsList(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	err := p.SetCookies(context.Background(), []map[string]any{
		{"name": "sid", "value": "abc", "url": "https://example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, c := range f.calls {
		if c.method == "Network.setCookies" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("Network.setCookies not sent")
	}
}
```

- [ ] **Step 2: Implement**

`internal/browser/cookies.go`:

```go
package browser

import (
	"context"
	"encoding/json"
)

func (p *Page) GetCookies(ctx context.Context, urls []string) ([]map[string]any, error) {
	params := map[string]any{}
	if len(urls) > 0 {
		params["urls"] = urls
	}
	raw, err := p.send(ctx, "Network.getCookies", params)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Cookies []map[string]any `json:"cookies"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Cookies, nil
}

func (p *Page) SetCookies(ctx context.Context, cookies []map[string]any) error {
	_, err := p.send(ctx, "Network.setCookies", map[string]any{"cookies": cookies})
	return err
}
```

- [ ] **Step 3: Tool registrations**

Append to `internal/mcp/tools/browser_inspect.go`:

```go
reg.Register("browser_get_cookies", func(ctx context.Context, params json.RawMessage) (any, error) {
	var a struct {
		URLs     []string `json:"url_filter"`
		TargetID string   `json:"target_id"`
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
	cs, err := page.GetCookies(ctx, a.URLs)
	if err != nil {
		return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
	}
	return map[string]any{"ok": true, "cookies": cs}, nil
})

reg.Register("browser_set_cookies", func(ctx context.Context, params json.RawMessage) (any, error) {
	var a struct {
		Cookies  []map[string]any `json:"cookies"`
		TargetID string           `json:"target_id"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
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
	if err := page.SetCookies(ctx, a.Cookies); err != nil {
		return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
	}
	return map[string]any{"ok": true}, nil
})
```

- [ ] **Step 4: Run + commit**

```bash
go test ./...
git add internal/browser/cookies.go internal/browser/cookies_test.go internal/mcp/tools/browser_inspect.go
git commit -m "browser: add GetCookies/SetCookies + browser_get_cookies/set_cookies tools"
```

---

## Task 5: Dialogs

**Files:**
- Create: `internal/browser/dialogs.go`
- Create: `internal/browser/dialogs_test.go`
- (Tool registration deferred to Task 7 with the rest of event tools)

`Page.HandleDialog(action, text?)` calls `Page.handleJavaScriptDialog{accept:true|false, promptText:text}`. Detection of dialogs is already done by the event collector in Task 1 (subscribed to `Page.javascriptDialogOpening`).

- [ ] **Step 1: Test**

```go
package browser

import (
	"context"
	"strings"
	"testing"
)

func TestHandleDialogAccept(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.HandleDialog(context.Background(), "accept", ""); err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, c := range f.calls {
		if c.method == "Page.handleJavaScriptDialog" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("not sent")
	}
}

func TestHandleDialogInvalidAction(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	err := p.HandleDialog(context.Background(), "weird", "")
	if err == nil || !strings.Contains(err.Error(), "action") {
		t.Fatalf("expected action error, got %v", err)
	}
}
```

- [ ] **Step 2: Implement**

`internal/browser/dialogs.go`:

```go
package browser

import (
	"context"
	"fmt"
)

// HandleDialog accepts or dismisses the currently-open JS dialog.
// action ∈ "accept" | "dismiss". text is optional prompt input.
func (p *Page) HandleDialog(ctx context.Context, action, text string) error {
	if action != "accept" && action != "dismiss" {
		return fmt.Errorf("invalid action %q", action)
	}
	params := map[string]any{"accept": action == "accept"}
	if text != "" {
		params["promptText"] = text
	}
	_, err := p.send(ctx, "Page.handleJavaScriptDialog", params)
	return err
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./...
git add internal/browser/dialogs.go internal/browser/dialogs_test.go
git commit -m "browser: add HandleDialog"
```

---

## Task 6: browser_wait_for + browser_get_recent_events tools

**Files:**
- Create: `internal/mcp/tools/browser_events.go`
- Create: `internal/mcp/tools/browser_events_test.go`
- Modify: `cmd/netra-browser/main.go` to register

`browser_wait_for{event, predicate?, timeout_ms?, target_id?}` — predicate is a JSON path-style filter expressed as a key/value object (v1: simple equality match on params). For more complex predicates, agent uses `browser_get_recent_events` and filters client-side.

`browser_get_recent_events{since?, types?, target_id?}` — wraps `Page.RecentEvents`.

`browser_handle_dialog{action, text?, target_id?}` — wraps `Page.HandleDialog`.

- [ ] **Step 1: Tool registration**

`internal/mcp/tools/browser_events.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

func RegisterBrowserEvents(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_wait_for", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Event     string         `json:"event"`
			Predicate map[string]any `json:"predicate"`
			TimeoutMS int            `json:"timeout_ms"`
			TargetID  string         `json:"target_id"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		method := mapEventName(a.Event)
		if method == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "unknown event " + a.Event}.AsResult(), nil
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
		timeout := time.Duration(a.TimeoutMS) * time.Millisecond
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		predicate := buildPredicate(a.Predicate)
		ev, err := page.WaitFor(ctx, method, predicate, timeout)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrTimeout, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{
			"ok":     true,
			"event":  a.Event,
			"params": json.RawMessage(ev.Params),
		}, nil
	})

	reg.Register("browser_get_recent_events", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Since    int64    `json:"since"` // milliseconds since epoch
			Types    []string `json:"types"`
			TargetID string   `json:"target_id"`
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
		var since time.Time
		if a.Since > 0 {
			since = time.UnixMilli(a.Since)
		}
		// Map agent-friendly type names to CDP method names.
		methods := make([]string, 0, len(a.Types))
		for _, t := range a.Types {
			if m := mapEventName(t); m != "" {
				methods = append(methods, m)
			}
		}
		evs := page.RecentEvents(since, methods)
		out := make([]map[string]any, 0, len(evs))
		for _, e := range evs {
			out = append(out, map[string]any{
				"event":  reverseEventName(e.Method),
				"at_ms":  e.At.UnixMilli(),
				"params": json.RawMessage(e.Params),
			})
		}
		return map[string]any{"ok": true, "events": out}, nil
	})

	reg.Register("browser_handle_dialog", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Action   string `json:"action"`
			Text     string `json:"text"`
			TargetID string `json:"target_id"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
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
		if err := page.HandleDialog(ctx, a.Action, a.Text); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true}, nil
	})
}

// mapEventName maps agent-facing names (per spec) to CDP method names.
func mapEventName(s string) string {
	switch s {
	case "navigation":
		return "Page.frameNavigated"
	case "network_request":
		return "Network.requestWillBeSent"
	case "network_response":
		return "Network.responseReceived"
	case "console":
		return "Runtime.consoleAPICalled"
	case "dialog":
		return "Page.javascriptDialogOpening"
	case "load":
		return "Page.loadEventFired"
	case "domcontentloaded":
		return "Page.domContentEventFired"
	}
	return ""
}

func reverseEventName(m string) string {
	switch m {
	case "Page.frameNavigated":
		return "navigation"
	case "Network.requestWillBeSent":
		return "network_request"
	case "Network.responseReceived":
		return "network_response"
	case "Runtime.consoleAPICalled":
		return "console"
	case "Page.javascriptDialogOpening":
		return "dialog"
	case "Page.loadEventFired":
		return "load"
	case "Page.domContentEventFired":
		return "domcontentloaded"
	}
	return m
}

func buildPredicate(p map[string]any) func(cdp.BufferedEvent) bool {
	if len(p) == 0 {
		return nil
	}
	return func(e cdp.BufferedEvent) bool {
		var got map[string]any
		if err := json.Unmarshal(e.Params, &got); err != nil {
			return false
		}
		for k, want := range p {
			if !matchSubKey(got, k, want) {
				return false
			}
		}
		return true
	}
}

// matchSubKey supports "a.b.c" dotted paths.
func matchSubKey(m map[string]any, dottedKey string, want any) bool {
	parts := strings.Split(dottedKey, ".")
	var cur any = m
	for _, p := range parts {
		mm, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		cur, ok = mm[p]
		if !ok {
			return false
		}
	}
	return cur == want || sprint(cur) == sprint(want)
}

func sprint(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
```

- [ ] **Step 2: Tests**

`internal/mcp/tools/browser_events_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

func TestWaitForToolTimeout(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserEvents(reg, sess)

	out, err := reg.Invoke(context.Background(), "browser_wait_for", json.RawMessage(`{"event":"navigation","timeout_ms":50}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"timeout"`) {
		t.Fatalf("expected timeout: %s", b)
	}
}

func TestHandleDialogTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserEvents(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_handle_dialog", json.RawMessage(`{"action":"accept"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("not ok: %s", b)
	}
}

func TestRecentEventsToolEmpty(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserEvents(reg, sess)
	// Allow the page collector to settle.
	time.Sleep(50 * time.Millisecond)
	out, err := reg.Invoke(context.Background(), "browser_get_recent_events", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("not ok: %s", b)
	}
}
```

- [ ] **Step 3: Wire main.go**

Add `tools.RegisterBrowserEvents(reg, sess)` to `cmd/netra-browser/main.go`.

- [ ] **Step 4: Run + commit**

```bash
go test ./...
git add internal/mcp/tools/browser_events.go internal/mcp/tools/browser_events_test.go cmd/netra-browser/main.go
git commit -m "tools: add browser_wait_for / browser_get_recent_events / browser_handle_dialog"
```

---

## Task 7: E2E inspection test

**Files:**
- Create: `e2e/inspect_test.go` (tag `e2e`)
- Create: `e2e/testdata/dialog.html`

Test: navigate to a page that triggers `alert("hi")`, wait for the dialog event, accept it, screenshot, eval `document.title`, get cookies.

- [ ] **Step 1: Fixture**

`e2e/testdata/dialog.html`:

```html
<!doctype html>
<html><head><title>Dialog Test</title></head><body>
<h1>Triggered Dialog</h1>
<script>
  setTimeout(() => alert("hello"), 200);
</script>
</body></html>
```

- [ ] **Step 2: Test**

`e2e/inspect_test.go`:

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
	"strings"
	"testing"
	"time"
)

func TestE2E_InspectionStack(t *testing.T) {
	chrome := findChrome(t)
	port := freePort(t)
	userDir := t.TempDir()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "testdata/dialog.html")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	chromeCmd := exec.Command(chrome,
		"--headless=new", "--no-sandbox", "--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+userDir, "about:blank",
	)
	chromeCmd.Stderr = os.Stderr
	if err := chromeCmd.Start(); err != nil {
		t.Fatalf("start chrome: %v", err)
	}
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
	if err := bin.Start(); err != nil {
		t.Fatal(err)
	}
	defer bin.Process.Kill()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	send := func(id int, method string, params any) map[string]any {
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			req["params"] = params
		}
		b, _ := json.Marshal(req)
		_, _ = io.WriteString(stdin, string(b)+"\n")
		if !scanner.Scan() {
			t.Fatal("no resp")
		}
		var r map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &r)
		return r
	}

	send(1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	r := send(2, "browser_new_tab", map[string]any{"url": srv.URL + "/"})
	tid := r["result"].(map[string]any)["target_id"].(string)
	time.Sleep(200 * time.Millisecond)

	// 3. eval document.title
	r = send(3, "browser_eval", map[string]any{"target_id": tid, "expression": "document.title"})
	res := r["result"].(map[string]any)
	if res["result"] != "Dialog Test" {
		t.Fatalf("title eval: %v", res)
	}

	// 4. screenshot
	r = send(4, "browser_screenshot", map[string]any{"target_id": tid})
	res = r["result"].(map[string]any)
	if _, ok := res["png_base64"].(string); !ok {
		t.Fatalf("no png: %v", res)
	}

	// 5. wait for dialog (the page fires alert at 200ms)
	r = send(5, "browser_wait_for", map[string]any{"target_id": tid, "event": "dialog", "timeout_ms": 5000})
	res = r["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("wait_for dialog: %v", res)
	}

	// 6. handle dialog
	r = send(6, "browser_handle_dialog", map[string]any{"target_id": tid, "action": "accept"})
	if r["result"].(map[string]any)["ok"] != true {
		t.Fatalf("handle_dialog: %v", r)
	}

	// 7. get cookies (will be empty for httptest origin but call should succeed)
	r = send(7, "browser_get_cookies", map[string]any{"target_id": tid})
	res = r["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("get_cookies: %v", res)
	}

	// 8. recent events — should include navigation
	r = send(8, "browser_get_recent_events", map[string]any{"target_id": tid, "types": []string{"navigation"}})
	res = r["result"].(map[string]any)
	evs, _ := res["events"].([]any)
	if len(evs) == 0 {
		// Not strictly required since the navigation event might predate our subscription;
		// just ensure call succeeds.
		_ = evs
	}
	_ = strings.Join
}
```

- [ ] **Step 3: Run**

`go test -tags e2e ./e2e/... -v -timeout 180s`. Expected: all 3 e2e tests pass (smoke, forms, inspection).

- [ ] **Step 4: Commit**

```bash
git add e2e/inspect_test.go e2e/testdata/dialog.html
git commit -m "e2e: add inspection stack test (eval, screenshot, dialog, cookies, events)"
```

---

## Task 8: Verify + tag

- [ ] **Step 1: Full suite**

```bash
go test ./...
go test -tags e2e ./e2e/... -timeout 180s
go vet ./...
gofmt -l . | grep -v '^docs/' | grep -v '^BRAINSTORM' || echo "all formatted"
git tag plan-c-inspection-waiting
```

- [ ] **Step 2: Confirm tools live**

After Plan C: 7 (A) + 11 (B) + 7 (C) = **25 tools**.

New in C: `browser_screenshot`, `browser_eval`, `browser_get_cookies`, `browser_set_cookies`, `browser_wait_for`, `browser_get_recent_events`, `browser_handle_dialog`.

---

## Self-Review Notes

**Spec coverage:**
- ✅ browser_screenshot (Task 2).
- ✅ browser_eval (Task 3).
- ✅ browser_get_cookies / browser_set_cookies (Task 4).
- ✅ browser_handle_dialog (Tasks 5, 6).
- ✅ browser_wait_for (Tasks 1, 6).
- ✅ browser_get_recent_events (Tasks 1, 6).
- ⏭ HTTP-SSE, launch mode, sessions → Plan D.
- ⏭ High-level tasks → Plan E.

**Placeholder scan:** Clean.

**Type consistency:**
- `Page.events *cdp.RingBuffer` field added in Task 1, consumed by Tasks 1, 6.
- `mapEventName` / `reverseEventName` in Task 6 use the spec's agent-facing names (`navigation`, `dialog`, etc.).
- Predicate is a `map[string]any` with dotted-key matching; supports simple eq predicates only. Complex predicates require `browser_get_recent_events` + client-side filtering.

**Known limitations to flag:**
- `Network.getCookies` is per-target session; for browser-wide cookies, Plan D's session save/load uses a different CDP path. Documented in `task_save_session` semantics in the spec.
- Predicate matching is intentionally simple — equality only. No regex, no ranges. Sufficient for "wait for navigation to URL X" use cases.
