package browser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
)

type fakeSender struct {
	calls   []call
	results map[string]json.RawMessage
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
func (f *fakeSender) SubscribeOnTarget(_, _ string) (<-chan cdp.BufferedEvent, func()) {
	ch := make(chan cdp.BufferedEvent, 1)
	ch <- cdp.BufferedEvent{} // immediately satisfies the wait
	return ch, func() {}
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
	if p.TargetID() != "T1" {
		t.Fatalf("target: %q", p.TargetID())
	}
}

// TestPageCloseIsIdempotentAndStopsCollectors verifies that closing a Page
// runs every collector cleanup exactly once and is safe to call repeatedly.
// This is the unit-level guard for the close-tab → DropPage → Page.Close path.
func TestPageCloseIsIdempotentAndStopsCollectors(t *testing.T) {
	var cleaned int
	f := &cleanupCountingSender{onCleanup: func() { cleaned++ }}
	p, err := NewPage(context.Background(), f, "T1")
	if err != nil {
		t.Fatal(err)
	}
	expected := len(collectedEvents)
	p.Close()
	if cleaned != expected {
		t.Fatalf("expected %d cleanups after first Close, got %d", expected, cleaned)
	}
	p.Close() // idempotent
	if cleaned != expected {
		t.Fatalf("Close should be idempotent; cleaned now %d", cleaned)
	}
}

type cleanupCountingSender struct {
	fakeSender
	onCleanup func()
}

func (c *cleanupCountingSender) SubscribeOnTarget(_, _ string) (<-chan cdp.BufferedEvent, func()) {
	ch := make(chan cdp.BufferedEvent, 1)
	return ch, c.onCleanup
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
