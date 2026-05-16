package cdp

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestSubscribeCleanupExitsGoroutine confirms that calling the cleanup func
// returned by Subscribe drains the subscription list. Without the cleanup
// path, the leaked channel stays in the eventSubList and the dispatcher
// keeps fanning out to it forever.
func TestSubscribeCleanupExitsGoroutine(t *testing.T) {
	url, stop := fakeChrome(t, func(c *websocket.Conn) {
		defer c.Close()
		// Hold the connection open for the test duration.
		time.Sleep(2 * time.Second)
	})
	defer stop()

	c, err := Dial(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, cleanup := c.Subscribe("Page.x")
	cleanup()
	cleanup() // idempotent

	// The subscription list for "Page.x" must now be empty.
	v, ok := c.subs.Load("Page.x")
	if !ok {
		return // never stored — fine
	}
	list := v.(*eventSubList)
	list.mu.Lock()
	n := len(list.chans)
	list.mu.Unlock()
	if n != 0 {
		t.Fatalf("subscription list not drained after cleanup: %d entries remain", n)
	}
}

// TestSubscribeOnTargetCleanupExitsGoroutine is the regression test for the
// long-standing leak documented as a TODO in session.go: each call to
// SubscribeOnTarget used to spawn an internal goroutine reading from a source
// channel that was never closed. Per-tab startEventCollector calls it 9
// times, so hours of use produced thousands of zombie goroutines.
func TestSubscribeOnTargetCleanupExitsGoroutine(t *testing.T) {
	url, stop := fakeChrome(t, func(c *websocket.Conn) {
		defer c.Close()
		for {
			if err := c.WriteJSON(Event{
				Method:    "Page.frameNavigated",
				SessionID: "S1",
				Params:    json.RawMessage(`{}`),
			}); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
	defer stop()

	c, err := Dial(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// Let readPump and stdlib goroutines settle.
	time.Sleep(100 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	const N = 50
	for i := 0; i < N; i++ {
		_, cleanup := c.SubscribeOnTarget("S1", "Page.frameNavigated")
		cleanup()
	}

	// Goroutines may take a tick to wind down; poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		now := runtime.NumGoroutine()
		if now <= baseline+5 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: baseline=%d after=%d (subscribed/cleaned-up %d times)",
		baseline, runtime.NumGoroutine(), N)
}
