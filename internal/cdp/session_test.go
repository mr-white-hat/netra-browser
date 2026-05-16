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
		_ = c.WriteJSON(Response{ID: m.ID, Result: json.RawMessage(`{"sessionId":"S1"}`)})
		// Hold the connection open briefly so readPump delivers the response
		// before the deferred Close races ahead and the client sees an
		// abnormal closure instead.
		time.Sleep(50 * time.Millisecond)
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
		time.Sleep(50 * time.Millisecond) // let response deliver before deferred Close
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
		// Hold briefly so the test goroutine registers its sub before
		// readPump races past an empty subscriber list ("event missing"
		// flake under -race).
		time.Sleep(100 * time.Millisecond)
		_ = c.WriteJSON(Event{Method: "Page.frameNavigated", Params: json.RawMessage(`{}`), SessionID: "S1"})
		_ = c.WriteJSON(Event{Method: "Page.frameNavigated", Params: json.RawMessage(`{}`), SessionID: "S2"})
		time.Sleep(200 * time.Millisecond)
	})
	defer stop()

	c, _ := Dial(context.Background(), url)
	defer c.Close()
	ch, cleanup := c.SubscribeOnTarget("S1", "Page.frameNavigated")
	defer cleanup()
	select {
	case e := <-ch:
		if e.SessionID != "S1" {
			t.Fatalf("got session %q", e.SessionID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("event missing")
	}
	select {
	case e := <-ch:
		t.Fatalf("unexpected second event: %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}
