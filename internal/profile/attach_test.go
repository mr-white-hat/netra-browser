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
	for _, c := range u[idx+1:] {
		if c < '0' || c > '9' {
			break
		}
		p = p*10 + int(c-'0')
	}
	return p
}
