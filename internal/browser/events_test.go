package browser

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
)

// helper to pre-create event channels for the pumpable sender so push() works.
func ensureCollectedEvents(p *pumpableSender) {
	for _, m := range collectedEvents {
		if _, ok := p.events[m]; !ok {
			p.events[m] = make(chan cdp.BufferedEvent, 16)
		}
	}
}

func TestPageBuffersEventsOnAttach(t *testing.T) {
	p := newPumpable()
	ensureCollectedEvents(p)
	page, err := NewPage(context.Background(), p, "T1")
	if err != nil {
		t.Fatal(err)
	}

	// Push 3 console events.
	for i := 0; i < 3; i++ {
		p.events["Runtime.consoleAPICalled"] <- cdp.BufferedEvent{
			Method: "Runtime.consoleAPICalled",
			Params: json.RawMessage(`{}`),
		}
	}
	time.Sleep(150 * time.Millisecond)

	got := page.RecentEvents(time.Time{}, []string{"Runtime.consoleAPICalled"})
	if len(got) != 3 {
		t.Fatalf("want 3 console events, got %d", len(got))
	}
}

func TestWaitForDeliversMatchingEvent(t *testing.T) {
	p := newPumpable()
	ensureCollectedEvents(p)
	page, _ := NewPage(context.Background(), p, "T1")

	done := make(chan error, 1)
	go func() {
		_, err := page.WaitFor(context.Background(), "Page.frameNavigated", nil, 2*time.Second)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	p.events["Page.frameNavigated"] <- cdp.BufferedEvent{
		Method: "Page.frameNavigated",
		Params: json.RawMessage(`{}`),
	}

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
	ensureCollectedEvents(p)
	page, _ := NewPage(context.Background(), p, "T1")

	_, err := page.WaitFor(context.Background(), "Page.frameNavigated", nil, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
