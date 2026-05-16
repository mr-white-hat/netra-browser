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
		// Hold briefly so the test goroutine has time to call Subscribe
		// before readPump dispatches the event past an empty subscriber list.
		time.Sleep(100 * time.Millisecond)
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

	ch, cleanup := c.Subscribe("Page.frameNavigated")
	defer cleanup()
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
	ch, cleanup := c.Subscribe("Page.x")
	cleanup()
	// drain a bit; should remain empty
	select {
	case e, ok := <-ch:
		if ok {
			t.Fatalf("expected no events after cleanup, got %v", e)
		}
	case <-time.After(300 * time.Millisecond):
	}
}
