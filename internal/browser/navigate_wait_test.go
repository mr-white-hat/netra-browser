package browser

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
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

// Override SubscribeOnTarget so it returns the per-method channel from the map.
func (p *pumpableSender) SubscribeOnTarget(_, method string) chan cdp.BufferedEvent {
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
