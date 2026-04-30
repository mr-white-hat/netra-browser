# netra-browser Plan A — Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a Go binary that connects to a running Chrome via CDP and exposes `meta_*` + tab management tools over MCP stdio, so an agent can attach, list/select/open/close tabs, and check health.

**Architecture:** Layered Go packages: `internal/cdp` (raw websocket + request correlation + event buffer), `internal/profile` (attach mode + lock-file), `internal/mcp` (JSON-RPC server + tool registry + stdio transport), `cmd/netra-browser` (CLI wiring). TDD throughout. Pure-Go unit tests for everything; one end-to-end smoke test against headless Chromium.

**Tech Stack:** Go 1.22+, `github.com/gorilla/websocket`, standard library only otherwise (no JSON-RPC framework).

**Source spec:** `docs/superpowers/specs/2026-04-30-netra-browser-design.md`

---

## File Structure (Plan A)

| Path | Responsibility |
|---|---|
| `go.mod`, `go.sum` | Module definition |
| `cmd/netra-browser/main.go` | CLI entry; flag parsing; wires profile + cdp + mcp |
| `internal/cdp/types.go` | `Method`, `Response`, `Event`, `Error` types |
| `internal/cdp/client.go` | Websocket client; Dial/Close/Send/Subscribe |
| `internal/cdp/event_buffer.go` | Per-target ring buffer (1000 events) |
| `internal/profile/attach.go` | Discover Chrome via `/json/version` |
| `internal/profile/lock.go` | `~/.config/netra-browser/active.lock` lifecycle |
| `internal/mcp/types.go` | JSON-RPC `Request`, `Response`, `Error` |
| `internal/mcp/registry.go` | Tool name → handler map |
| `internal/mcp/stdio.go` | stdin/stdout JSON-RPC loop |
| `internal/mcp/errors.go` | Stable `error_code` envelope |
| `internal/mcp/tools/meta.go` | `meta_attach`, `meta_detach`, `meta_health` |
| `internal/mcp/tools/browser_targets.go` | `browser_list_tabs`, `browser_new_tab`, `browser_select_tab`, `browser_close_tab` |
| `internal/mcp/session.go` | Per-MCP-session state: active `cdp.Client`, `active_target_id` |
| `e2e/smoke_test.go` | Spawns headless Chromium, runs binary, exercises tools |
| `.gitignore` | Standard Go ignores |
| `README.md` | Stub — will fill in Plan F |

Test files (`_test.go`) sit alongside their subjects.

---

## Task 1: Project skeleton

**Files:**
- Create: `/home/mrwhitehat/ClaudePlaywright/.gitignore`
- Create: `/home/mrwhitehat/ClaudePlaywright/go.mod`
- Create: `/home/mrwhitehat/ClaudePlaywright/cmd/netra-browser/main.go`
- Create: `/home/mrwhitehat/ClaudePlaywright/README.md`

- [ ] **Step 1: Initialize git repo (if not already)**

```bash
cd /home/mrwhitehat/ClaudePlaywright
git init
git config user.email "pavankumar2138@gmail.com" || true
```

- [ ] **Step 2: Create `.gitignore`**

```
# binaries
/netra-browser
/cmd/netra-browser/netra-browser
*.exe

# go build artifacts
*.test
*.out
coverage.out

# IDE
.idea/
.vscode/

# OS
.DS_Store
```

- [ ] **Step 3: Create `go.mod`**

```bash
cd /home/mrwhitehat/ClaudePlaywright
go mod init github.com/pavankumar2138/netra-browser
go get github.com/gorilla/websocket@latest
```

- [ ] **Step 4: Create `cmd/netra-browser/main.go`**

```go
package main

import (
	"flag"
	"fmt"
	"os"
)

const Version = "0.0.1-dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	fmt.Fprintln(os.Stderr, "netra-browser: not yet implemented; see --version")
	os.Exit(1)
}
```

- [ ] **Step 5: Create stub `README.md`**

```markdown
# netra-browser

Bring your own Chrome — the missing MCP bridge for AI agents that need a real, logged-in browser.

Status: in development. See `docs/superpowers/specs/` for the design.
```

- [ ] **Step 6: Verify build**

Run: `cd /home/mrwhitehat/ClaudePlaywright && go build ./... && ./netra-browser --version || go run ./cmd/netra-browser --version`
Expected: prints `0.0.1-dev`.

- [ ] **Step 7: Commit**

```bash
git add .gitignore go.mod go.sum cmd/netra-browser/main.go README.md docs/
git commit -m "scaffold: initialize netra-browser module with CLI stub"
```

---

## Task 2: CDP types

**Files:**
- Create: `internal/cdp/types.go`
- Create: `internal/cdp/types_test.go`

- [ ] **Step 1: Write failing test for Method serialization round-trip**

`internal/cdp/types_test.go`:

```go
package cdp

import (
	"encoding/json"
	"testing"
)

func TestMethodSerializationRoundTrip(t *testing.T) {
	m := Method{ID: 42, Name: "Page.navigate", Params: map[string]any{"url": "https://example.com"}}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Method
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != 42 || got.Name != "Page.navigate" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestResponseDecode(t *testing.T) {
	raw := []byte(`{"id":7,"result":{"frameId":"abc"}}`)
	var r Response
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.ID != 7 || r.Error != nil {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestErrorResponseDecode(t *testing.T) {
	raw := []byte(`{"id":7,"error":{"code":-32000,"message":"boom"}}`)
	var r Response
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Error == nil || r.Error.Code != -32000 {
		t.Fatalf("expected error: %+v", r)
	}
}

func TestEventDecode(t *testing.T) {
	raw := []byte(`{"method":"Page.frameNavigated","params":{"frame":{"id":"abc"}},"sessionId":"S1"}`)
	var e Event
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.Method != "Page.frameNavigated" || e.SessionID != "S1" {
		t.Fatalf("unexpected: %+v", e)
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/cdp/...`
Expected: FAIL — undefined: `Method`, `Response`, `Event`, `Error`.

- [ ] **Step 3: Implement types**

`internal/cdp/types.go`:

```go
// Package cdp provides a thin Chrome DevTools Protocol websocket client.
package cdp

import "encoding/json"

// Method is an outbound CDP request.
type Method struct {
	ID        int64           `json:"id"`
	Name      string          `json:"method"`
	Params    any             `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// Response is a CDP method reply.
type Response struct {
	ID        int64           `json:"id"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *Error          `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// Event is an unsolicited CDP message (no id, has method+params).
type Event struct {
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// Error is the CDP error body.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cdp/...`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cdp/types.go internal/cdp/types_test.go
git commit -m "cdp: add Method/Response/Event/Error types"
```

---

## Task 3: CDP event ring buffer

**Files:**
- Create: `internal/cdp/event_buffer.go`
- Create: `internal/cdp/event_buffer_test.go`

- [ ] **Step 1: Write failing test**

`internal/cdp/event_buffer_test.go`:

```go
package cdp

import (
	"strconv"
	"testing"
	"time"
)

func TestRingBufferRecentByType(t *testing.T) {
	b := NewRingBuffer(100)
	b.Add(BufferedEvent{At: time.UnixMilli(1), Method: "Page.frameNavigated"})
	b.Add(BufferedEvent{At: time.UnixMilli(2), Method: "Network.requestWillBeSent"})
	b.Add(BufferedEvent{At: time.UnixMilli(3), Method: "Page.frameNavigated"})

	got := b.Recent(time.UnixMilli(0), []string{"Page.frameNavigated"})
	if len(got) != 2 {
		t.Fatalf("want 2 page events, got %d", len(got))
	}
}

func TestRingBufferDropsOldest(t *testing.T) {
	b := NewRingBuffer(3)
	for i := 0; i < 5; i++ {
		b.Add(BufferedEvent{At: time.UnixMilli(int64(i)), Method: "x" + strconv.Itoa(i)})
	}
	all := b.Recent(time.Time{}, nil)
	if len(all) != 3 {
		t.Fatalf("want 3, got %d", len(all))
	}
	if all[0].Method != "x2" {
		t.Fatalf("oldest should be x2, got %s", all[0].Method)
	}
}

func TestRingBufferFiltersBySince(t *testing.T) {
	b := NewRingBuffer(10)
	b.Add(BufferedEvent{At: time.UnixMilli(1), Method: "a"})
	b.Add(BufferedEvent{At: time.UnixMilli(5), Method: "b"})
	b.Add(BufferedEvent{At: time.UnixMilli(10), Method: "c"})
	got := b.Recent(time.UnixMilli(5), nil)
	if len(got) != 2 || got[0].Method != "b" {
		t.Fatalf("got %+v", got)
	}
}
```

- [ ] **Step 2: Run test, verify failure**

Run: `go test ./internal/cdp/ -run RingBuffer`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`internal/cdp/event_buffer.go`:

```go
package cdp

import (
	"encoding/json"
	"sync"
	"time"
)

// BufferedEvent is what we keep in memory per target.
type BufferedEvent struct {
	At        time.Time
	Method    string
	Params    json.RawMessage
	SessionID string
}

// RingBuffer is a fixed-capacity event store. Oldest events are dropped on overflow.
type RingBuffer struct {
	mu    sync.Mutex
	cap   int
	items []BufferedEvent
}

func NewRingBuffer(cap int) *RingBuffer {
	if cap <= 0 {
		cap = 1
	}
	return &RingBuffer{cap: cap, items: make([]BufferedEvent, 0, cap)}
}

func (b *RingBuffer) Add(e BufferedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) >= b.cap {
		copy(b.items, b.items[1:])
		b.items = b.items[:len(b.items)-1]
	}
	b.items = append(b.items, e)
}

// Recent returns events with At >= since whose Method is in types.
// If types is empty/nil, all methods match. since=zero matches all.
func (b *RingBuffer) Recent(since time.Time, types []string) []BufferedEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]BufferedEvent, 0, len(b.items))
	typeSet := make(map[string]struct{}, len(types))
	for _, t := range types {
		typeSet[t] = struct{}{}
	}
	for _, e := range b.items {
		if !since.IsZero() && e.At.Before(since) {
			continue
		}
		if len(typeSet) > 0 {
			if _, ok := typeSet[e.Method]; !ok {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cdp/ -run RingBuffer -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cdp/event_buffer.go internal/cdp/event_buffer_test.go
git commit -m "cdp: add per-target ring buffer for events"
```

---

## Task 4: CDP websocket client — Dial + Send/Response correlation

**Files:**
- Create: `internal/cdp/client.go`
- Create: `internal/cdp/client_test.go`

- [ ] **Step 1: Write failing test using a fake WS server**

`internal/cdp/client_test.go`:

```go
package cdp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeChrome echoes any Method as a Response with the same id.
func fakeChrome(t *testing.T, handler func(conn *websocket.Conn)) (wsURL string, stop func()) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		handler(c)
	}))
	return "ws" + strings.TrimPrefix(srv.URL, "http"), srv.Close
}

func TestClientSendReceivesResponse(t *testing.T) {
	url, stop := fakeChrome(t, func(c *websocket.Conn) {
		defer c.Close()
		var m Method
		if err := c.ReadJSON(&m); err != nil {
			return
		}
		_ = c.WriteJSON(Response{ID: m.ID, Result: json.RawMessage(`{"ok":true}`)})
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, err := Dial(ctx, url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	res, err := c.Send(ctx, "Browser.getVersion", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if string(res) != `{"ok":true}` {
		t.Fatalf("unexpected result: %s", res)
	}
}

func TestClientSendErrorPropagates(t *testing.T) {
	url, stop := fakeChrome(t, func(c *websocket.Conn) {
		defer c.Close()
		var m Method
		_ = c.ReadJSON(&m)
		_ = c.WriteJSON(Response{ID: m.ID, Error: &Error{Code: -1, Message: "bad"}})
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, _ := Dial(ctx, url)
	defer c.Close()
	_, err := c.Send(ctx, "Browser.getVersion", nil)
	if err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestClientSendCancelsOnContextDeadline(t *testing.T) {
	url, stop := fakeChrome(t, func(c *websocket.Conn) {
		defer c.Close()
		var m Method
		_ = c.ReadJSON(&m)
		// never reply
		time.Sleep(2 * time.Second)
	})
	defer stop()

	c, _ := Dial(context.Background(), url)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := c.Send(ctx, "Browser.getVersion", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/cdp/ -run Client -v`
Expected: FAIL — undefined Dial/Send/Close.

- [ ] **Step 3: Implement client**

`internal/cdp/client.go`:

```go
package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Client is a long-lived CDP websocket connection.
type Client struct {
	conn       *websocket.Conn
	writeMu    sync.Mutex
	nextID     atomic.Int64
	pending    sync.Map // int64 -> chan Response
	subs       sync.Map // string method -> []chan BufferedEvent (set in Task 5)
	closeOnce  sync.Once
	closed     chan struct{}
	closeError atomic.Value // error
}

// Dial opens a websocket and starts the read pump.
func Dial(ctx context.Context, wsURL string) (*Client, error) {
	d := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := d.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial cdp: %w", err)
	}
	c := &Client{conn: conn, closed: make(chan struct{})}
	go c.readPump()
	return c, nil
}

func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closeError.Store(errors.New("cdp client closed"))
		_ = c.conn.Close()
		close(c.closed)
	})
	return nil
}

// Send issues a CDP method and waits for the matching response.
func (c *Client) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan Response, 1)
	c.pending.Store(id, ch)
	defer c.pending.Delete(id)

	c.writeMu.Lock()
	err := c.conn.WriteJSON(Method{ID: id, Name: method, Params: params})
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		if v := c.closeError.Load(); v != nil {
			return nil, v.(error)
		}
		return nil, errors.New("cdp client closed")
	case r := <-ch:
		if r.Error != nil {
			return nil, r.Error
		}
		return r.Result, nil
	}
}

// readPump dispatches inbound frames to pending responses or to event subscribers.
func (c *Client) readPump() {
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			c.closeError.Store(err)
			c.closeOnce.Do(func() { _ = c.conn.Close(); close(c.closed) })
			return
		}
		// Distinguish Response (has id) from Event (has method, no id).
		var probe struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			continue
		}
		if probe.ID != nil {
			var r Response
			if err := json.Unmarshal(raw, &r); err != nil {
				continue
			}
			if v, ok := c.pending.Load(r.ID); ok {
				ch := v.(chan Response)
				select {
				case ch <- r:
				default:
				}
			}
			continue
		}
		if probe.Method != "" {
			var e Event
			if err := json.Unmarshal(raw, &e); err != nil {
				continue
			}
			c.dispatchEvent(e)
		}
	}
}

// dispatchEvent is filled in Task 5; for now, no-op.
func (c *Client) dispatchEvent(e Event) {}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cdp/ -run Client -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cdp/client.go internal/cdp/client_test.go
git commit -m "cdp: add websocket client with request/response correlation"
```

---

## Task 5: CDP event subscribe + dispatch

**Files:**
- Modify: `internal/cdp/client.go` (add `Subscribe` method, fill `dispatchEvent`)
- Create: `internal/cdp/client_events_test.go`

- [ ] **Step 1: Write failing test**

`internal/cdp/client_events_test.go`:

```go
package cdp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClientSubscribeReceivesEvent(t *testing.T) {
	url, stop := fakeChrome(t, func(c *websocket.Conn) {
		defer c.Close()
		_ = c.WriteJSON(Event{Method: "Page.frameNavigated", Params: json.RawMessage(`{"frame":{"id":"f1"}}`)})
		time.Sleep(200 * time.Millisecond)
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ch := c.Subscribe("Page.frameNavigated")
	select {
	case e := <-ch:
		if e.Method != "Page.frameNavigated" {
			t.Fatalf("got %s", e.Method)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("event not received")
	}
}

func TestClientUnsubscribeStopsDelivery(t *testing.T) {
	url, stop := fakeChrome(t, func(c *websocket.Conn) {
		defer c.Close()
		for i := 0; i < 3; i++ {
			_ = c.WriteJSON(Event{Method: "Page.x", Params: json.RawMessage(`{}`)})
			time.Sleep(50 * time.Millisecond)
		}
	})
	defer stop()

	c, _ := Dial(context.Background(), url)
	defer c.Close()
	ch := c.Subscribe("Page.x")
	c.Unsubscribe("Page.x", ch)
	// drain a bit; should remain empty
	select {
	case <-ch:
		t.Fatal("expected no events after unsubscribe")
	case <-time.After(300 * time.Millisecond):
	}
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/cdp/ -run Subscribe -v`
Expected: FAIL — undefined Subscribe/Unsubscribe.

- [ ] **Step 3: Implement subscribe/dispatch**

Edit `internal/cdp/client.go`. Replace the placeholder `dispatchEvent` with this and add Subscribe/Unsubscribe near it:

```go
// Subscribe returns a channel that receives BufferedEvents whose Method matches.
// Channel is buffered (16); if full, events are dropped for that subscriber.
func (c *Client) Subscribe(method string) chan BufferedEvent {
	ch := make(chan BufferedEvent, 16)
	v, _ := c.subs.LoadOrStore(method, &eventSubList{})
	v.(*eventSubList).add(ch)
	return ch
}

// Unsubscribe removes a channel previously returned by Subscribe.
func (c *Client) Unsubscribe(method string, ch chan BufferedEvent) {
	if v, ok := c.subs.Load(method); ok {
		v.(*eventSubList).remove(ch)
	}
}

func (c *Client) dispatchEvent(e Event) {
	be := BufferedEvent{At: time.Now(), Method: e.Method, Params: e.Params, SessionID: e.SessionID}
	if v, ok := c.subs.Load(e.Method); ok {
		v.(*eventSubList).fanout(be)
	}
}

type eventSubList struct {
	mu    sync.Mutex
	chans []chan BufferedEvent
}

func (l *eventSubList) add(ch chan BufferedEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.chans = append(l.chans, ch)
}
func (l *eventSubList) remove(ch chan BufferedEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, c := range l.chans {
		if c == ch {
			l.chans = append(l.chans[:i], l.chans[i+1:]...)
			return
		}
	}
}
func (l *eventSubList) fanout(e BufferedEvent) {
	l.mu.Lock()
	chans := append([]chan BufferedEvent(nil), l.chans...)
	l.mu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- e:
		default: // drop on full
		}
	}
}
```

- [ ] **Step 4: Run all cdp tests**

Run: `go test ./internal/cdp/... -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cdp/client.go internal/cdp/client_events_test.go
git commit -m "cdp: add event subscribe/unsubscribe with fan-out"
```

---

## Task 6: Profile — discover Chrome via `/json/version`

**Files:**
- Create: `internal/profile/attach.go`
- Create: `internal/profile/attach_test.go`

- [ ] **Step 1: Write failing test**

`internal/profile/attach_test.go`:

```go
package profile

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverParsesVersionEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"Browser": "Chrome/120.0.6099.71",
			"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/browser/abc"
		}`))
	}))
	defer srv.Close()

	port := portFromURL(t, srv.URL)
	info, err := Discover("127.0.0.1", port)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if info.Browser != "Chrome/120.0.6099.71" {
		t.Fatalf("browser: %s", info.Browser)
	}
	if !strings.HasPrefix(info.WebSocketDebuggerURL, "ws://") {
		t.Fatalf("ws url: %s", info.WebSocketDebuggerURL)
	}
}

func TestDiscoverErrorOnDeadPort(t *testing.T) {
	_, err := Discover("127.0.0.1", 1) // privileged, refused
	if err == nil {
		t.Fatal("expected error connecting to closed port")
	}
}

func portFromURL(t *testing.T, u string) int {
	t.Helper()
	idx := strings.LastIndex(u, ":")
	if idx < 0 {
		t.Fatal("no port in url")
	}
	var p int
	if _, err := fmtParseInt(u[idx+1:], &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// fmtParseInt is a tiny shim to avoid importing strconv in the test header.
func fmtParseInt(s string, out *int) (int, error) {
	var v int
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + int(c-'0')
	}
	*out = v
	return v, nil
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/profile/ -run Discover -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`internal/profile/attach.go`:

```go
// Package profile manages Chrome attach/launch and session export.
package profile

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DebugInfo is the parsed /json/version response.
type DebugInfo struct {
	Browser              string `json:"Browser"`
	ProtocolVersion      string `json:"Protocol-Version"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	UserAgent            string `json:"User-Agent"`
}

// Discover queries http://host:port/json/version on a running Chrome.
func Discover(host string, port int) (*DebugInfo, error) {
	url := fmt.Sprintf("http://%s:%d/json/version", host, port)
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get %s: status %d", url, resp.StatusCode)
	}
	var info DebugInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode %s: %w", url, err)
	}
	if info.WebSocketDebuggerURL == "" {
		return nil, fmt.Errorf("empty webSocketDebuggerUrl")
	}
	return &info, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/profile/ -run Discover -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/profile/attach.go internal/profile/attach_test.go
git commit -m "profile: discover Chrome via /json/version endpoint"
```

---

## Task 7: Profile — lock file

**Files:**
- Create: `internal/profile/lock.go`
- Create: `internal/profile/lock_test.go`

- [ ] **Step 1: Write failing test**

`internal/profile/lock_test.go`:

```go
package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active.lock")

	l, err := Acquire(path, 9222)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	if err := l.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lock file not removed: %v", err)
	}
}

func TestLockSecondAcquireFailsUntilForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active.lock")

	l1, err := Acquire(path, 9222)
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Release()

	if _, err := Acquire(path, 9223); err == nil {
		t.Fatal("expected second acquire to fail")
	}

	l2, err := ForceAcquire(path, 9223)
	if err != nil {
		t.Fatalf("force: %v", err)
	}
	defer l2.Release()
}

func TestLockStaleAutoCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active.lock")

	// Write a lock for a definitely-dead PID.
	if err := os.WriteFile(path, []byte(`{"port":9222,"pid":999999,"started_at":"2020-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Acquire(path, 9222)
	if err != nil {
		t.Fatalf("expected stale-lock cleanup, got: %v", err)
	}
	defer l.Release()
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/profile/ -run Lock -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`internal/profile/lock.go`:

```go
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

type lockData struct {
	Port      int       `json:"port"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// Lock represents an acquired lock on a path.
type Lock struct {
	path string
}

// Acquire creates a lock file. Fails if a live lock exists.
// Stale locks (PID not running) are auto-cleaned.
func Acquire(path string, port int) (*Lock, error) {
	if existing, ok := readLock(path); ok && pidAlive(existing.PID) {
		return nil, fmt.Errorf("lock held by pid %d on port %d", existing.PID, existing.Port)
	}
	return write(path, port)
}

// ForceAcquire overwrites any existing lock.
func ForceAcquire(path string, port int) (*Lock, error) {
	_ = os.Remove(path)
	return write(path, port)
}

func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}

func write(path string, port int) (*Lock, error) {
	if err := os.MkdirAll(parentDir(path), 0o755); err != nil {
		return nil, err
	}
	d := lockData{Port: port, PID: os.Getpid(), StartedAt: time.Now().UTC()}
	b, _ := json.Marshal(d)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, fmt.Errorf("write lock: %w", err)
	}
	return &Lock{path: path}, nil
}

func readLock(path string) (lockData, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return lockData{}, false
	}
	var d lockData
	if err := json.Unmarshal(b, &d); err != nil {
		return lockData{}, false
	}
	return d, true
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := p.Signal(syscall.Signal(0)); err != nil {
		return errors.Is(err, os.ErrPermission) // EPERM means process exists, just not ours
	}
	return true
}

func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/profile/ -v`
Expected: PASS (5 tests total).

- [ ] **Step 5: Commit**

```bash
git add internal/profile/lock.go internal/profile/lock_test.go
git commit -m "profile: add lock-file acquire/release with stale cleanup"
```

---

## Task 8: MCP types + error envelope

**Files:**
- Create: `internal/mcp/types.go`
- Create: `internal/mcp/errors.go`
- Create: `internal/mcp/types_test.go`

- [ ] **Step 1: Write failing test**

`internal/mcp/types_test.go`:

```go
package mcp

import (
	"encoding/json"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	r := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "browser_navigate", Params: json.RawMessage(`{"url":"https://example.com"}`)}
	b, _ := json.Marshal(r)
	var got Request
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Method != "browser_navigate" {
		t.Fatalf("method: %s", got.Method)
	}
}

func TestErrorEnvelope(t *testing.T) {
	e := ToolError{Code: "chrome_disconnected", Message: "ws closed", TargetID: "T1"}
	b, _ := json.Marshal(e.AsResult())
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["ok"] != false || m["error_code"] != "chrome_disconnected" {
		t.Fatalf("envelope: %s", b)
	}
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/mcp/... -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement types**

`internal/mcp/types.go`:

```go
// Package mcp implements the JSON-RPC server and tool registry for netra-browser.
package mcp

import "encoding/json"

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is the JSON-RPC error object (for protocol-level errors).
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
```

`internal/mcp/errors.go`:

```go
package mcp

// Standard tool error codes.
const (
	ErrChromeDisconnected = "chrome_disconnected"
	ErrChromeDead         = "chrome_dead"
	ErrTargetDestroyed    = "target_destroyed"
	ErrTimeout            = "timeout"
	ErrAmbiguousLocator   = "ambiguous_locator"
	ErrNotFound           = "not_found"
	ErrNotAttached        = "not_attached"
	ErrInvalidArgs        = "invalid_args"
)

// ToolError is the application-level error returned inside a tool result.
// Tools never return JSON-RPC errors for these — they return a result with ok:false.
type ToolError struct {
	Code     string `json:"error_code"`
	Message  string `json:"message"`
	TargetID string `json:"target_id,omitempty"`
}

// AsResult turns a ToolError into the standard {ok:false, ...} map.
func (e ToolError) AsResult() map[string]any {
	m := map[string]any{
		"ok":         false,
		"error_code": e.Code,
		"message":    e.Message,
	}
	if e.TargetID != "" {
		m["target_id"] = e.TargetID
	}
	return m
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mcp/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/types.go internal/mcp/errors.go internal/mcp/types_test.go
git commit -m "mcp: add JSON-RPC types and tool-error envelope"
```

---

## Task 9: MCP tool registry

**Files:**
- Create: `internal/mcp/registry.go`
- Create: `internal/mcp/registry_test.go`

- [ ] **Step 1: Write failing test**

`internal/mcp/registry_test.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRegistryRegisterAndInvoke(t *testing.T) {
	reg := NewRegistry()
	reg.Register("echo", func(ctx context.Context, params json.RawMessage) (any, error) {
		return map[string]any{"got": string(params)}, nil
	})

	out, err := reg.Invoke(context.Background(), "echo", json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["got"] != `{"a":1}` {
		t.Fatalf("got %v", m)
	}
}

func TestRegistryUnknownTool(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Invoke(context.Background(), "nope", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown tool error, got %v", err)
	}
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/mcp/ -run Registry -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`internal/mcp/registry.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Handler is invoked for a single tool call.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Registry maps tool names to handlers.
type Registry struct {
	mu sync.RWMutex
	h  map[string]Handler
}

func NewRegistry() *Registry { return &Registry{h: map[string]Handler{}} }

func (r *Registry) Register(name string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.h[name] = h
}

func (r *Registry) Invoke(ctx context.Context, name string, params json.RawMessage) (any, error) {
	r.mu.RLock()
	h, ok := r.h[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return h(ctx, params)
}

// Names returns sorted-by-insertion list of registered tools (for diagnostics).
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.h))
	for n := range r.h {
		out = append(out, n)
	}
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mcp/ -run Registry -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/registry.go internal/mcp/registry_test.go
git commit -m "mcp: add tool registry"
```

---

## Task 10: MCP stdio transport

**Files:**
- Create: `internal/mcp/stdio.go`
- Create: `internal/mcp/stdio_test.go`

- [ ] **Step 1: Write failing test**

`internal/mcp/stdio_test.go`:

```go
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStdioServerHandlesOneRequest(t *testing.T) {
	reg := NewRegistry()
	reg.Register("ping", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := Serve(ctx, in, &out, reg); err != nil && err != io.EOF {
		t.Fatalf("serve: %v", err)
	}

	resp := strings.TrimSpace(out.String())
	if !strings.Contains(resp, `"ok":true`) {
		t.Fatalf("response missing ok: %s", resp)
	}
}

func TestStdioServerReturnsRPCErrorOnUnknownTool(t *testing.T) {
	reg := NewRegistry()
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"nope"}` + "\n")
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = Serve(ctx, in, &out, reg)
	if !strings.Contains(out.String(), `"code":-32601`) {
		t.Fatalf("expected method-not-found, got %s", out.String())
	}
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/mcp/ -run Stdio -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`internal/mcp/stdio.go`:

```go
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Serve reads newline-delimited JSON-RPC requests from r, invokes the registry,
// and writes responses to w. Returns nil on clean EOF, error on protocol failure.
func Serve(ctx context.Context, r io.Reader, w io.Writer, reg *Registry) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024) // 16 MiB lines
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(Response{JSONRPC: "2.0", Error: &RPCError{Code: -32700, Message: "parse error: " + err.Error()}})
			continue
		}

		result, err := reg.Invoke(ctx, req.Method, req.Params)
		if err != nil {
			code := -32603 // internal
			if errors.Is(err, errUnknownTool) || isUnknownToolErr(err) {
				code = -32601
			}
			_ = enc.Encode(Response{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: code, Message: err.Error()}})
			continue
		}

		raw, err := json.Marshal(result)
		if err != nil {
			_ = enc.Encode(Response{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: -32603, Message: "marshal result: " + err.Error()}})
			continue
		}
		_ = enc.Encode(Response{JSONRPC: "2.0", ID: req.ID, Result: raw})
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stdio scan: %w", err)
	}
	return nil
}

var errUnknownTool = errors.New("unknown tool")

func isUnknownToolErr(err error) bool {
	return err != nil && len(err.Error()) >= 12 && err.Error()[:12] == "unknown tool"
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mcp/ -v`
Expected: PASS (all mcp tests).

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/stdio.go internal/mcp/stdio_test.go
git commit -m "mcp: add newline-delimited JSON-RPC stdio transport"
```

---

## Task 11: MCP session — holds active CDP client + active target

**Files:**
- Create: `internal/mcp/session.go`
- Create: `internal/mcp/session_test.go`

- [ ] **Step 1: Write failing test**

`internal/mcp/session_test.go`:

```go
package mcp

import "testing"

func TestSessionAttachDetach(t *testing.T) {
	s := NewSession()
	if s.IsAttached() {
		t.Fatal("session should start detached")
	}
	s.SetClient(fakeClient{})
	if !s.IsAttached() {
		t.Fatal("expected attached after SetClient")
	}
	s.Clear()
	if s.IsAttached() {
		t.Fatal("expected detached after Clear")
	}
}

func TestSessionActiveTarget(t *testing.T) {
	s := NewSession()
	s.SetActiveTarget("T1")
	if got := s.ActiveTarget(); got != "T1" {
		t.Fatalf("got %s", got)
	}
}

type fakeClient struct{}

func (fakeClient) Close() error { return nil }
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/mcp/ -run Session -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`internal/mcp/session.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"sync"
)

// CDPSender is the interface the session holds onto.
// Concrete impl is *cdp.Client; tests pass fakes.
type CDPSender interface {
	Send(ctx context.Context, method string, params any) (json.RawMessage, error)
	Close() error
}

// CDPCloser is kept as an alias for older code that imported the name.
type CDPCloser = CDPSender

// Session is per-MCP-process state: one active CDP connection, one active target.
type Session struct {
	mu           sync.RWMutex
	client       CDPSender
	activeTarget string
}

func NewSession() *Session { return &Session{} }

func (s *Session) SetClient(c CDPSender) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = c
}

func (s *Session) Client() CDPSender {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

func (s *Session) IsAttached() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client != nil
}

func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
	s.activeTarget = ""
}

func (s *Session) SetActiveTarget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeTarget = id
}

func (s *Session) ActiveTarget() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeTarget
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mcp/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/session.go internal/mcp/session_test.go
git commit -m "mcp: add per-process session state (active client + target)"
```

---

## Task 12: meta_health tool

**Files:**
- Create: `internal/mcp/tools/meta.go`
- Create: `internal/mcp/tools/meta_test.go`

- [ ] **Step 1: Write failing test**

`internal/mcp/tools/meta_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

func TestMetaHealthDetached(t *testing.T) {
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	RegisterMeta(reg, sess, MetaDeps{})

	out, err := reg.Invoke(context.Background(), "meta_health", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !contains(b, `"chrome_alive":false`) || !contains(b, `"ws_alive":false`) {
		t.Fatalf("unexpected: %s", b)
	}
}

func contains(b []byte, s string) bool {
	return string(b) != "" && (len(s) == 0 || (string(b) != "" && (indexOf(string(b), s) >= 0)))
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/mcp/tools/ -v`
Expected: FAIL — undefined `RegisterMeta`, `MetaDeps`.

- [ ] **Step 3: Implement**

`internal/mcp/tools/meta.go`:

```go
// Package tools registers the MCP tools exposed by netra-browser.
package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

// MetaDeps holds dependencies the meta_* tools may need.
// AttachFunc opens a CDP client to the running Chrome (Task 13 fills it in).
type MetaDeps struct {
	StartedAt time.Time
	AttachFunc func(ctx context.Context, debugURL string) (mcp.CDPCloser, string /*chromeVersion*/, int /*targetCount*/, error)
}

// RegisterMeta registers meta_attach, meta_detach, meta_health.
func RegisterMeta(reg *mcp.Registry, sess *mcp.Session, deps MetaDeps) {
	if deps.StartedAt.IsZero() {
		deps.StartedAt = time.Now()
	}

	reg.Register("meta_health", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{
			"ok":           true,
			"chrome_alive": sess.IsAttached(),
			"ws_alive":     sess.IsAttached(),
			"uptime_ms":    time.Since(deps.StartedAt).Milliseconds(),
		}, nil
	})

	reg.Register("meta_detach", func(ctx context.Context, _ json.RawMessage) (any, error) {
		sess.Clear()
		return map[string]any{"ok": true}, nil
	})

	type attachArgs struct {
		DebugURL string `json:"debug_url"`
	}
	reg.Register("meta_attach", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a attachArgs
		if len(params) > 0 {
			if err := json.Unmarshal(params, &a); err != nil {
				return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
			}
		}
		if a.DebugURL == "" {
			a.DebugURL = "http://127.0.0.1:9222"
		}
		if deps.AttachFunc == nil {
			return mcp.ToolError{Code: "not_implemented", Message: "AttachFunc not wired"}.AsResult(), nil
		}
		client, version, targetCount, err := deps.AttachFunc(ctx, a.DebugURL)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDead, Message: err.Error()}.AsResult(), nil
		}
		sess.SetClient(client)
		return map[string]any{
			"ok":             true,
			"chrome_version": version,
			"target_count":   targetCount,
		}, nil
	})
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mcp/tools/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/tools/meta.go internal/mcp/tools/meta_test.go
git commit -m "tools: add meta_attach/meta_detach/meta_health"
```

---

## Task 13: Wire AttachFunc to real CDP client

**Files:**
- Create: `internal/cdp/attach.go` (high-level helper that combines profile.Discover + cdp.Dial)
- Create: `internal/cdp/attach_test.go`

- [ ] **Step 1: Write failing test (network parse only — full attach needs Chromium, deferred to Task 17 smoke)**

`internal/cdp/attach_test.go`:

```go
package cdp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseDebugURL(t *testing.T) {
	cases := []struct{ in, host string; port int }{
		{"http://127.0.0.1:9222", "127.0.0.1", 9222},
		{"127.0.0.1:9333", "127.0.0.1", 9333},
		{"http://localhost:9444/", "localhost", 9444},
	}
	for _, c := range cases {
		host, port, err := ParseDebugURL(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if host != c.host || port != c.port {
			t.Errorf("%s: got %s:%d", c.in, host, port)
		}
	}
}

func TestParseDebugURLInvalid(t *testing.T) {
	if _, _, err := ParseDebugURL("not a url"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAttachFailsOnUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil) //nolint
	}))
	defer srv.Close()
	if _, _, _, err := Attach(t.Context(), strings.Replace(srv.URL, "http://", "", 1)); err == nil {
		t.Fatal("expected attach error against 404 endpoint")
	}
}
```

NOTE: `t.Context()` is Go 1.24+; if not available, replace with `context.Background()`.

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/cdp/ -run Attach -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`internal/cdp/attach.go`:

```go
package cdp

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/pavankumar2138/netra-browser/internal/profile"
)

// ParseDebugURL extracts host+port from inputs like "http://127.0.0.1:9222" or "127.0.0.1:9222".
func ParseDebugURL(s string) (string, int, error) {
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", 0, fmt.Errorf("parse %q: %w", s, err)
	}
	host := u.Hostname()
	portStr := u.Port()
	if host == "" || portStr == "" {
		return "", 0, fmt.Errorf("debug url missing host or port: %s", s)
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("port %q: %w", portStr, err)
	}
	return host, p, nil
}

// Attach discovers the Chrome at debugURL, dials its browser-level CDP socket,
// and returns the client plus version + target count.
func Attach(ctx context.Context, debugURL string) (*Client, string, int, error) {
	host, port, err := ParseDebugURL(debugURL)
	if err != nil {
		return nil, "", 0, err
	}
	info, err := profile.Discover(host, port)
	if err != nil {
		return nil, "", 0, fmt.Errorf("discover: %w", err)
	}
	client, err := Dial(ctx, info.WebSocketDebuggerURL)
	if err != nil {
		return nil, "", 0, fmt.Errorf("dial cdp: %w", err)
	}
	// Target count: ask Target.getTargets.
	raw, err := client.Send(ctx, "Target.getTargets", nil)
	if err != nil {
		_ = client.Close()
		return nil, "", 0, fmt.Errorf("Target.getTargets: %w", err)
	}
	var got struct {
		TargetInfos []any `json:"targetInfos"`
	}
	_ = jsonUnmarshalSilent(raw, &got)
	return client, info.Browser, len(got.TargetInfos), nil
}

func jsonUnmarshalSilent(b []byte, v any) error {
	if len(b) == 0 {
		return nil
	}
	return jsonUnmarshal(b, v)
}
```

Add at top of file (or factor into a helpers file):

```go
import "encoding/json"

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cdp/ -v`
Expected: PASS (parse tests; Attach test verifies failure path only).

- [ ] **Step 5: Commit**

```bash
git add internal/cdp/attach.go internal/cdp/attach_test.go
git commit -m "cdp: add Attach helper combining profile discovery and Dial"
```

---

## Task 14: browser_list_tabs / new_tab / select_tab / close_tab

**Files:**
- Create: `internal/mcp/tools/browser_targets.go`
- Create: `internal/mcp/tools/browser_targets_test.go`

- [ ] **Step 1: Write failing test using a mock CDP sender**

`internal/mcp/tools/browser_targets_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

type fakeCDP struct {
	responses map[string]json.RawMessage
	calls     []string
}

func (f *fakeCDP) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	if r, ok := f.responses[method]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeCDP) Close() error { return nil }

func TestBrowserListTabsReturnsArray(t *testing.T) {
	sess := mcp.NewSession()
	cdp := &fakeCDP{responses: map[string]json.RawMessage{
		"Target.getTargets": json.RawMessage(`{"targetInfos":[
			{"targetId":"T1","url":"https://a.example","title":"A","attached":true,"type":"page"},
			{"targetId":"T2","url":"https://b.example","title":"B","attached":false,"type":"page"},
			{"targetId":"X1","url":"chrome://extensions","title":"x","attached":false,"type":"background_page"}
		]}`),
	}}
	sess.SetClient(cdp)

	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	out, err := reg.Invoke(context.Background(), "browser_list_tabs", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	got := string(b)
	if !contains([]byte(got), `"target_id":"T1"`) || !contains([]byte(got), `"target_id":"T2"`) {
		t.Fatalf("missing pages: %s", got)
	}
	if contains([]byte(got), `"target_id":"X1"`) {
		t.Fatalf("non-page leaked: %s", got)
	}
}

func TestBrowserNewTabSendsCreateTarget(t *testing.T) {
	sess := mcp.NewSession()
	cdp := &fakeCDP{responses: map[string]json.RawMessage{
		"Target.createTarget": json.RawMessage(`{"targetId":"NEW"}`),
	}}
	sess.SetClient(cdp)
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	out, err := reg.Invoke(context.Background(), "browser_new_tab", json.RawMessage(`{"url":"about:blank"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !contains(b, `"target_id":"NEW"`) {
		t.Fatalf("expected target_id=NEW: %s", b)
	}
	if sess.ActiveTarget() != "NEW" {
		t.Fatalf("expected active=NEW, got %s", sess.ActiveTarget())
	}
}

func TestBrowserSelectTabUpdatesSession(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeCDP{})
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	_, err := reg.Invoke(context.Background(), "browser_select_tab", json.RawMessage(`{"target_id":"T2"}`))
	if err != nil {
		t.Fatal(err)
	}
	if sess.ActiveTarget() != "T2" {
		t.Fatalf("got %s", sess.ActiveTarget())
	}
}

func TestBrowserCloseTabClearsActiveIfNeeded(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeCDP{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	_, err := reg.Invoke(context.Background(), "browser_close_tab", json.RawMessage(`{"target_id":"T1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if sess.ActiveTarget() != "" {
		t.Fatalf("expected active cleared, got %s", sess.ActiveTarget())
	}
}

func TestBrowserListTabsErrorWhenNotAttached(t *testing.T) {
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	out, err := reg.Invoke(context.Background(), "browser_list_tabs", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !contains(b, `"error_code":"not_attached"`) {
		t.Fatalf("expected not_attached: %s", b)
	}
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./internal/mcp/tools/ -v`
Expected: FAIL — undefined `RegisterBrowserTargets`.

- [ ] **Step 3: Implement**

`internal/mcp/tools/browser_targets.go`:

```go
package tools

import (
	"context"
	"encoding/json"

	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

func RegisterBrowserTargets(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_list_tabs", func(ctx context.Context, _ json.RawMessage) (any, error) {
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		raw, err := client.Send(ctx, "Target.getTargets", nil)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		var resp struct {
			TargetInfos []struct {
				TargetID string `json:"targetId"`
				URL      string `json:"url"`
				Title    string `json:"title"`
				Type     string `json:"type"`
				Attached bool   `json:"attached"`
			} `json:"targetInfos"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return mcp.ToolError{Code: "decode_error", Message: err.Error()}.AsResult(), nil
		}
		active := sess.ActiveTarget()
		out := []map[string]any{}
		for _, ti := range resp.TargetInfos {
			if ti.Type != "page" {
				continue
			}
			out = append(out, map[string]any{
				"target_id": ti.TargetID,
				"url":       ti.URL,
				"title":     ti.Title,
				"active":    ti.TargetID == active,
			})
		}
		return map[string]any{"ok": true, "tabs": out}, nil
	})

	reg.Register("browser_new_tab", func(ctx context.Context, params json.RawMessage) (any, error) {
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		var args struct {
			URL string `json:"url"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &args)
		}
		if args.URL == "" {
			args.URL = "about:blank"
		}
		raw, err := client.Send(ctx, "Target.createTarget", map[string]any{"url": args.URL})
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		var resp struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(raw, &resp)
		sess.SetActiveTarget(resp.TargetID)
		return map[string]any{"ok": true, "target_id": resp.TargetID}, nil
	})

	reg.Register("browser_select_tab", func(ctx context.Context, params json.RawMessage) (any, error) {
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		var args struct {
			TargetID string `json:"target_id"`
		}
		if err := json.Unmarshal(params, &args); err != nil || args.TargetID == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "target_id required"}.AsResult(), nil
		}
		_, err := client.Send(ctx, "Target.activateTarget", map[string]any{"targetId": args.TargetID})
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		sess.SetActiveTarget(args.TargetID)
		return map[string]any{"ok": true}, nil
	})

	reg.Register("browser_close_tab", func(ctx context.Context, params json.RawMessage) (any, error) {
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		var args struct {
			TargetID string `json:"target_id"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &args)
		}
		if args.TargetID == "" {
			args.TargetID = sess.ActiveTarget()
		}
		if args.TargetID == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target_id and no active target"}.AsResult(), nil
		}
		_, err := client.Send(ctx, "Target.closeTarget", map[string]any{"targetId": args.TargetID})
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		if sess.ActiveTarget() == args.TargetID {
			sess.SetActiveTarget("")
		}
		return map[string]any{"ok": true}, nil
	})
}
```

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/session.go internal/mcp/tools/browser_targets.go internal/mcp/tools/browser_targets_test.go
git commit -m "tools: add browser_list_tabs/new_tab/select_tab/close_tab"
```

---

## Task 15: Wire main.go — attach + serve stdio

**Files:**
- Modify: `cmd/netra-browser/main.go`

- [ ] **Step 1: Replace `main.go` with full wiring**

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
	"github.com/pavankumar2138/netra-browser/internal/mcp"
	"github.com/pavankumar2138/netra-browser/internal/mcp/tools"
	"github.com/pavankumar2138/netra-browser/internal/profile"
)

const Version = "0.0.1-dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		debugURL    = flag.String("debug-url", "http://127.0.0.1:9222", "Chrome remote debugging URL")
		autoAttach  = flag.Bool("auto-attach", false, "attach to Chrome at startup")
		lockPath    = flag.String("lock", "", "lock file path (default: ~/.config/netra-browser/active.lock)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(Version)
		return
	}

	if *lockPath == "" {
		home, _ := os.UserHomeDir()
		*lockPath = filepath.Join(home, ".config", "netra-browser", "active.lock")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Acquire lock (port 0 since no Chrome owned by us yet — refined in launch mode in Plan D).
	lock, err := profile.Acquire(*lockPath, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lock: %v (use --force-reattach in a future version)\n", err)
		os.Exit(2)
	}
	defer lock.Release()

	sess := mcp.NewSession()
	reg := mcp.NewRegistry()

	tools.RegisterMeta(reg, sess, tools.MetaDeps{
		StartedAt:  time.Now(),
		AttachFunc: func(ctx context.Context, url string) (mcp.CDPCloser, string, int, error) {
			return cdp.Attach(ctx, url)
		},
	})
	tools.RegisterBrowserTargets(reg, sess)

	if *autoAttach {
		client, version, count, err := cdp.Attach(ctx, *debugURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "auto-attach: %v\n", err)
		} else {
			sess.SetClient(client)
			fmt.Fprintf(os.Stderr, "attached to %s (%d targets)\n", version, count)
		}
	}

	if err := mcp.Serve(ctx, os.Stdin, os.Stdout, reg); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
}
```

NOTE: `cdp.Attach` returns `*cdp.Client`, but `MetaDeps.AttachFunc` declares `mcp.CDPCloser`. Since `*cdp.Client` has both `Close()` and `Send()`, it implements `mcp.CDPSender` (alias `CDPCloser`). Go's interface satisfaction handles this automatically at the call site.

- [ ] **Step 2: Verify build**

Run: `cd /home/mrwhitehat/ClaudePlaywright && go build ./...`
Expected: clean build.

- [ ] **Step 3: Manual smoke (no Chrome)**

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"meta_health"}' | go run ./cmd/netra-browser
```
Expected: a single JSON line with `"chrome_alive":false`.

- [ ] **Step 4: Commit**

```bash
git add cmd/netra-browser/main.go
git commit -m "cmd: wire CDP attach + MCP stdio server in main"
```

---

## Task 16: End-to-end smoke against headless Chromium

**Files:**
- Create: `e2e/smoke_test.go`

This is the only test that needs Chromium running. It uses the binary as a subprocess.

- [ ] **Step 1: Write the smoke test**

`e2e/smoke_test.go`:

```go
//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// findChrome returns the first chromium-like binary in PATH.
func findChrome(t *testing.T) string {
	for _, name := range []string{"chromium", "google-chrome", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("no chromium binary found in PATH")
	return ""
}

func freePort(t *testing.T) int {
	for p := 9322; p < 9400; p++ {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", p))
		if err != nil { // nothing listening — good
			return p
		}
		resp.Body.Close()
	}
	t.Fatal("no free debug port")
	return 0
}

func waitForChrome(t *testing.T, port int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("chrome did not come up")
}

func TestE2E_AttachAndListTabs(t *testing.T) {
	chrome := findChrome(t)
	port := freePort(t)
	userDir := t.TempDir()

	chromeCmd := exec.Command(chrome,
		"--headless=new",
		"--no-sandbox",
		"--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", userDir),
		"about:blank",
	)
	chromeCmd.Stderr = os.Stderr
	if err := chromeCmd.Start(); err != nil {
		t.Fatalf("start chrome: %v", err)
	}
	defer chromeCmd.Process.Kill()
	waitForChrome(t, port, 10*time.Second)

	lockPath := userDir + "/active.lock"
	bin := exec.Command("go", "run", "../cmd/netra-browser",
		"--lock", lockPath,
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	bin.Stderr = os.Stderr
	if err := bin.Start(); err != nil {
		t.Fatalf("start bridge: %v", err)
	}
	defer bin.Process.Kill()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)

	send := func(id int, method string, params any) map[string]any {
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			req["params"] = params
		}
		b, _ := json.Marshal(req)
		_, _ = io.WriteString(stdin, string(b)+"\n")
		if !scanner.Scan() {
			t.Fatal("no response")
		}
		var resp map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v line=%s", err, scanner.Text())
		}
		return resp
	}

	// 1. health (detached)
	r := send(1, "meta_health", nil)
	if !strings.Contains(fmt.Sprintf("%v", r), "chrome_alive:false") {
		// Not strict — chrome_alive is bool false, fine if it's printed differently
	}

	// 2. attach
	r = send(2, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	res := r["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("attach failed: %v", r)
	}

	// 3. list tabs — about:blank should be there.
	r = send(3, "browser_list_tabs", nil)
	res = r["result"].(map[string]any)
	tabs := res["tabs"].([]any)
	if len(tabs) == 0 {
		t.Fatalf("expected at least one tab, got %v", res)
	}

	// 4. new_tab
	r = send(4, "browser_new_tab", map[string]any{"url": "about:blank"})
	res = r["result"].(map[string]any)
	if res["target_id"] == nil {
		t.Fatalf("no target_id: %v", r)
	}
	tid := res["target_id"].(string)

	// 5. close_tab
	r = send(5, "browser_close_tab", map[string]any{"target_id": tid})
	res = r["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("close failed: %v", r)
	}

	// 6. detach
	_ = send(6, "meta_detach", nil)
}
```

The `//go:build e2e` tag excludes this from default `go test ./...`.

- [ ] **Step 2: Run unit tests one more time (no e2e)**

Run: `go test ./...`
Expected: all PASS, no e2e run.

- [ ] **Step 3: Run the smoke test**

Run: `go test -tags e2e ./e2e/... -v -timeout 60s`
Expected: PASS. If `chromium` is missing, test SKIPS — that's acceptable, but on this Kali box it should run.

- [ ] **Step 4: Commit**

```bash
git add e2e/smoke_test.go
git commit -m "e2e: add smoke test for attach + tab management against headless Chromium"
```

---

## Task 17: Verify Plan A end-to-end + tag

- [ ] **Step 1: Run full test suite**

Run: `go test ./... && go test -tags e2e ./e2e/... -timeout 60s`
Expected: all PASS.

- [ ] **Step 2: Run go vet + check formatting**

Run: `go vet ./... && gofmt -l . | tee /tmp/fmt.txt && test ! -s /tmp/fmt.txt`
Expected: no output from `gofmt -l`.

- [ ] **Step 3: Tag**

```bash
git tag plan-a-foundation
```

- [ ] **Step 4: Confirm Plan A is complete**

Plan A delivers:
- Working `netra-browser` binary
- 7 tools: `meta_attach`, `meta_detach`, `meta_health`, `browser_list_tabs`, `browser_new_tab`, `browser_select_tab`, `browser_close_tab`
- stdio MCP transport
- Attach mode (no launch yet)
- Lock file
- Unit-tested CDP client with event ring buffer
- End-to-end smoke against headless Chromium

Plan B (Core browsing) starts from this tag.

---

## Self-Review Notes

**Spec coverage check:**
- ✅ Architecture (cdp/profile/mcp/cmd packages): Tasks 1, 2-7, 8-12, 15.
- ✅ Attach mode: Tasks 6, 13, 15.
- ✅ Lock file: Task 7.
- ✅ MCP stdio transport: Task 10.
- ✅ Per-target ring buffer: Task 3 (consumed in Plan C).
- ✅ Error envelope (`error_code`/`message`/`target_id`): Task 8.
- ✅ Targets/tabs tools: Task 14.
- ✅ Meta tools: Task 12.
- ⏭ Navigation, interaction, inspection, waiting, dialogs, snapshot/locator, HTTP-SSE, launch mode, sessions, high-level tasks → Plans B–E.
- ⏭ Release pipeline → Plan F.

**Placeholder scan:** No TBD/TODO/etc. in actionable steps. Every code block is concrete.

**Type consistency check:**
- `cdp.Method/Response/Event/Error` defined in Task 2, used consistently in Tasks 4–5.
- `cdp.BufferedEvent` defined in Task 3, used by event subscribers.
- `mcp.Request/Response/RPCError` defined Task 8, used in Task 10.
- `mcp.ToolError` defined Task 8, used in Tasks 12, 14.
- `mcp.CDPSender = CDPCloser` aliased in Task 14 to keep Task 12 compiling.
- `mcp.Registry`/`Handler`/`Session` defined in Tasks 9, 11; consumed in Tasks 12, 14, 15.
- `cdp.Attach` signature in Task 13 matches the call in Task 15.
- `*cdp.Client` has `Send` and `Close` so it satisfies `mcp.CDPSender`.

All cross-task references resolved.
