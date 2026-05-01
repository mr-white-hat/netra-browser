package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

// fakeNoEventSender is like fakeFullSender but SubscribeOnTarget returns an empty channel
// so WaitFor will time out rather than immediately receive a phantom event.
type fakeNoEventSender struct{}

func (f *fakeNoEventSender) Send(ctx context.Context, m string, _ any) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (f *fakeNoEventSender) SendOnTarget(ctx context.Context, _, m string, _ any) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (f *fakeNoEventSender) AttachToTarget(ctx context.Context, t string) (string, error) {
	return "S-" + t, nil
}
func (f *fakeNoEventSender) SubscribeOnTarget(_, _ string) chan cdp.BufferedEvent {
	return make(chan cdp.BufferedEvent) // no events — caller will timeout
}
func (f *fakeNoEventSender) Close() error { return nil }

func TestWaitForToolTimeout(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeNoEventSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserEvents(reg, sess)

	out, err := reg.Invoke(context.Background(), "browser_wait_for", json.RawMessage(`{"event":"navigation","timeout_ms":50}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"timeout"`) {
		t.Fatalf("expected timeout: %s", b)
	}
}

func TestHandleDialogTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserEvents(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_handle_dialog", json.RawMessage(`{"action":"accept"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("not ok: %s", b)
	}
}

func TestRecentEventsToolReturnsArray(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserEvents(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_get_recent_events", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("not ok: %s", b)
	}
	if !strings.Contains(string(b), `"events":`) {
		t.Fatalf("missing events array: %s", b)
	}
}

func TestUnknownEventNameRejected(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserEvents(reg, sess)
	out, _ := reg.Invoke(context.Background(), "browser_wait_for", json.RawMessage(`{"event":"bogus"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"invalid_args"`) {
		t.Fatalf("expected invalid_args: %s", b)
	}
}
