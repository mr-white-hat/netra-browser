package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPSSE_RPC(t *testing.T) {
	reg := NewRegistry()
	reg.Register("ping", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	srv := httptest.NewServer(NewHTTPHandler(reg, HTTPOpts{}))
	defer srv.Close()

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	resp, err := http.Post(srv.URL+"/rpc", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	res, ok := out["result"].(map[string]any)
	if !ok || res["ok"] != true {
		t.Fatalf("unexpected result: %+v", out)
	}
}

func TestHTTPSSE_TokenRequired(t *testing.T) {
	reg := NewRegistry()
	srv := httptest.NewServer(NewHTTPHandler(reg, HTTPOpts{Token: "secret"}))
	defer srv.Close()
	resp, _ := http.Post(srv.URL+"/rpc", "application/json", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHTTPSSE_TokenAccepted(t *testing.T) {
	reg := NewRegistry()
	reg.Register("ping", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	srv := httptest.NewServer(NewHTTPHandler(reg, HTTPOpts{Token: "secret"}))
	defer srv.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHTTPSSE_EventsEndpointReserved(t *testing.T) {
	reg := NewRegistry()
	srv := httptest.NewServer(NewHTTPHandler(reg, HTTPOpts{}))
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/events")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHTTPSSE_GetMethodOnRPCRejected(t *testing.T) {
	reg := NewRegistry()
	srv := httptest.NewServer(NewHTTPHandler(reg, HTTPOpts{}))
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/rpc")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
