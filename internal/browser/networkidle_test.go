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
	// Pre-create the channels so push() succeeds.
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

func TestNetworkIdleHonorsContextCancel(t *testing.T) {
	p := newPumpable()
	p.events["Network.requestWillBeSent"] = make(chan cdp.BufferedEvent, 8)
	p.events["Network.loadingFinished"] = make(chan cdp.BufferedEvent, 8)
	p.events["Network.loadingFailed"] = make(chan cdp.BufferedEvent, 8)

	page, _ := NewPage(context.Background(), p, "T1")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- page.WaitNetworkIdle(ctx, 5*time.Second)
	}()

	// Push a never-finished request so the timer never fires.
	p.events["Network.requestWillBeSent"] <- cdp.BufferedEvent{Method: "Network.requestWillBeSent", Params: json.RawMessage(`{"requestId":"R1"}`)}
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected ctx.Err()")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not honor cancel")
	}
}
