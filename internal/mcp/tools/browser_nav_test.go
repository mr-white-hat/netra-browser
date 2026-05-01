package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

// fakeFullSender satisfies both mcp.CDPSender and browser.Sender so Session.Page works.
type fakeFullSender struct {
	calls   []string
	results map[string]json.RawMessage
}

func (f *fakeFullSender) Send(ctx context.Context, m string, _ any) (json.RawMessage, error) {
	f.calls = append(f.calls, m)
	if r, ok := f.results[m]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeFullSender) SendOnTarget(ctx context.Context, _, m string, _ any) (json.RawMessage, error) {
	f.calls = append(f.calls, m)
	if r, ok := f.results[m]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeFullSender) AttachToTarget(ctx context.Context, t string) (string, error) {
	return "S-" + t, nil
}
func (f *fakeFullSender) SubscribeOnTarget(_, _ string) chan cdp.BufferedEvent {
	ch := make(chan cdp.BufferedEvent, 1)
	ch <- cdp.BufferedEvent{}
	return ch
}
func (f *fakeFullSender) Close() error { return nil }

func TestNavigateTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Page.navigate":               json.RawMessage(`{"frameId":"F1"}`),
			"Accessibility.getFullAXTree": json.RawMessage(`{"nodes":[]}`),
		},
	})
	sess.SetActiveTarget("T1")

	reg := mcp.NewRegistry()
	RegisterBrowserNav(reg, sess)

	out, err := reg.Invoke(context.Background(), "browser_navigate", json.RawMessage(`{"url":"https://example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"url":"https://example.com"`) {
		t.Fatalf("missing url: %s", b)
	}
}

func TestReloadTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserNav(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_reload", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("not ok: %s", b)
	}
}

func TestNavigateRequiresTargetOrActive(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{})
	// no active target set
	reg := mcp.NewRegistry()
	RegisterBrowserNav(reg, sess)
	out, _ := reg.Invoke(context.Background(), "browser_navigate", json.RawMessage(`{"url":"https://x"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"invalid_args"`) {
		t.Fatalf("expected invalid_args: %s", b)
	}
}
