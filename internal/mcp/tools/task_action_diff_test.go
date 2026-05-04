package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func TestActionDiffURLChange(t *testing.T) {
	// Each call to Runtime.evaluate("location.href") returns a different URL —
	// so the before/after captures see a change.
	hits := 0
	urls := []string{"https://before.example", "https://after.example"}
	customSend := func(ctx context.Context, m string, _ any) (json.RawMessage, error) {
		switch m {
		case "Runtime.evaluate":
			i := hits
			if i >= len(urls) {
				i = len(urls) - 1
			}
			hits++
			return json.RawMessage(`{"result":{"type":"string","value":"` + urls[i] + `"}}`), nil
		case "Network.getCookies":
			return json.RawMessage(`{"cookies":[]}`), nil
		}
		return json.RawMessage(`{}`), nil
	}
	sess := mcp.NewSession()
	sess.SetClient(&programmableSender{send: customSend})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	// Register a no-op test action that doesn't wait on CDP events.
	reg.Register("noop", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	RegisterTaskActionDiff(reg, sess)
	out, err := reg.Invoke(context.Background(), "task_action_diff",
		json.RawMessage(`{"action":{"tool":"noop","args":{}},"capture":["url","cookies"]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"url_changed":true`) {
		t.Fatalf("expected url_changed: %s", b)
	}
	if !strings.Contains(string(b), `"url_before":"https://before.example"`) {
		t.Fatalf("missing url_before: %s", b)
	}
}

func TestActionDiffPropagatesActionResult(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&programmableSender{send: func(ctx context.Context, m string, _ any) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	reg.Register("noop", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true, "marker": "MARKER42"}, nil
	})
	RegisterTaskActionDiff(reg, sess)
	out, err := reg.Invoke(context.Background(), "task_action_diff",
		json.RawMessage(`{"action":{"tool":"noop"},"capture":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `MARKER42`) {
		t.Fatalf("action result not propagated: %s", b)
	}
}

// programmableSender lets tests fully control Send behavior.
type programmableSender struct {
	send func(ctx context.Context, method string, params any) (json.RawMessage, error)
}

func (p *programmableSender) Send(ctx context.Context, m string, params any) (json.RawMessage, error) {
	return p.send(ctx, m, params)
}
func (p *programmableSender) SendOnTarget(ctx context.Context, _, m string, params any) (json.RawMessage, error) {
	return p.send(ctx, m, params)
}
func (p *programmableSender) AttachToTarget(ctx context.Context, t string) (string, error) {
	return "S-" + t, nil
}
func (p *programmableSender) SubscribeOnTarget(_, _ string) chan cdp.BufferedEvent {
	ch := make(chan cdp.BufferedEvent, 1)
	ch <- cdp.BufferedEvent{Method: "Page.loadEventFired"}
	return ch
}
func (p *programmableSender) Close() error { return nil }
