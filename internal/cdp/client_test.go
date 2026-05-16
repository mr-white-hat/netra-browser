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
		// Give the client's readPump a chance to deliver the response before
		// the deferred Close races ahead and the client sees an abnormal
		// closure instead of the typed error.
		time.Sleep(50 * time.Millisecond)
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
