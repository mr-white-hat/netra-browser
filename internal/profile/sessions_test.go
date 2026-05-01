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
	calls   []string
}

func (f *fakeBrowserClient) Send(ctx context.Context, method string, _ any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	switch method {
	case "Storage.getCookies":
		b, _ := json.Marshal(map[string]any{"cookies": f.cookies})
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
