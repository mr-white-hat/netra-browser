package profile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type fakeBrowserClient struct {
	cookies []map[string]any
	targets []map[string]any
	// origin → entries; if non-nil, getDOMStorageItems answers per origin.
	storageByOrigin map[string][][2]string
	// recorded calls (method only) and full param payloads (paramCalls).
	calls      []string
	paramCalls []paramCall
}

type paramCall struct {
	method string
	params any
}

func (f *fakeBrowserClient) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	f.paramCalls = append(f.paramCalls, paramCall{method, params})
	switch method {
	case "Storage.getCookies":
		b, _ := json.Marshal(map[string]any{"cookies": f.cookies})
		return json.RawMessage(b), nil
	case "Target.getTargets":
		b, _ := json.Marshal(map[string]any{"targetInfos": f.targets})
		return json.RawMessage(b), nil
	case "Storage.getDOMStorageItems":
		// Pull origin from params.
		p, _ := params.(map[string]any)
		sid, _ := p["storageId"].(map[string]any)
		origin, _ := sid["securityOrigin"].(string)
		entries := f.storageByOrigin[origin]
		out := [][]string{}
		for _, kv := range entries {
			out = append(out, []string{kv[0], kv[1]})
		}
		b, _ := json.Marshal(map[string]any{"entries": out})
		return json.RawMessage(b), nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeBrowserClient) Close() error { return nil }

func TestSaveLoadSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	c := &fakeBrowserClient{
		cookies: []map[string]any{{"name": "sid", "value": "abc", "domain": "example.com"}},
	}

	if err := SaveSession(context.Background(), c, "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".config", "netra-browser", "sessions", "alpha.json")); err != nil {
		t.Fatalf("session file not created: %v", err)
	}

	c2 := &fakeBrowserClient{}
	if err := LoadSession(context.Background(), c2, "alpha"); err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, m := range c2.calls {
		if m == "Storage.setCookies" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("Storage.setCookies not sent on load")
	}
}

func TestLoadSessionMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	c := &fakeBrowserClient{}
	if err := LoadSession(context.Background(), c, "nonexistent"); err == nil {
		t.Fatal("expected error on missing session")
	}
}

func TestSaveSessionRequiresName(t *testing.T) {
	c := &fakeBrowserClient{}
	if err := SaveSession(context.Background(), c, ""); err == nil {
		t.Fatal("expected name-required error")
	}
}

func TestOriginFromURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://example.com/dash", "https://example.com"},
		{"http://example.com:8080/x", "http://example.com:8080"},
		{"about:blank", ""},
		{"data:text/html,<h1>", ""},
		{"chrome://settings", ""},
		{"file:///etc/passwd", ""},
		{"https://api.example.com", "https://api.example.com"},
	}
	for _, c := range cases {
		if got := OriginFromURL(c.in); got != c.want {
			t.Errorf("OriginFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWriteReadRoundTripWithLocalStorage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	sf := SessionFile{
		Name:    "ls",
		Cookies: []map[string]any{{"name": "sid", "value": "v"}},
		LocalStorage: map[string]map[string]string{
			"https://example.com": {"k1": "v1", "k2": "v2"},
		},
	}
	if err := WriteSessionFile("ls", sf); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSessionFile("ls")
	if err != nil {
		t.Fatal(err)
	}
	if got.LocalStorage["https://example.com"]["k1"] != "v1" {
		t.Fatalf("LS roundtrip mismatch: %v", got.LocalStorage)
	}
}

func TestOldSessionFileLoadsWithoutLocalStorage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	sd := filepath.Join(dir, ".config", "netra-browser", "sessions")
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"name":"old","saved_at":"2026-01-01T00:00:00Z","cookies":[{"name":"sid","value":"v","domain":"x"}]}`
	if err := os.WriteFile(filepath.Join(sd, "old.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSessionFile("old")
	if err != nil {
		t.Fatalf("legacy session read failed: %v", err)
	}
	if got.LocalStorage != nil {
		t.Fatal("LocalStorage should be nil for legacy file")
	}
}
