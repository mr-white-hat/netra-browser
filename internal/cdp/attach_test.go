package cdp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseDebugURL(t *testing.T) {
	cases := []struct {
		in   string
		host string
		port int
	}{
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	if _, _, _, err := Attach(context.Background(), addr); err == nil {
		t.Fatal("expected attach error against 404 endpoint")
	}
}
