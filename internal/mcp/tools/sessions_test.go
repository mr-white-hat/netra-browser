package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func TestSessionToolsNotAttached(t *testing.T) {
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	RegisterSessionTasks(reg, sess)
	out, _ := reg.Invoke(context.Background(), "task_save_session", json.RawMessage(`{"name":"x"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"not_attached"`) {
		t.Fatalf("expected not_attached: %s", b)
	}
}

func TestSessionToolsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Storage.getCookies": json.RawMessage(`{"cookies":[{"name":"sid","value":"abc"}]}`),
		},
	})
	reg := mcp.NewRegistry()
	RegisterSessionTasks(reg, sess)

	out, _ := reg.Invoke(context.Background(), "task_save_session", json.RawMessage(`{"name":"r1"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("save not ok: %s", b)
	}

	out, _ = reg.Invoke(context.Background(), "task_load_session", json.RawMessage(`{"name":"r1"}`))
	b, _ = json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("load not ok: %s", b)
	}
	// session file exists
	if _, err := os.Stat(dir + "/.config/netra-browser/sessions/r1.json"); err != nil {
		t.Fatal(err)
	}
}
