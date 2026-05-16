package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
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
func (f *fakeFullSender) SubscribeOnTarget(_, _ string) (<-chan cdp.BufferedEvent, func()) {
	// Continuously refill so any addEventSub registered AFTER the collector
	// starts pumping still gets dispatched — closes the pre-existing race
	// where a single buffered event could be drained before a sub registered.
	return continuouslyEmptyEvents()
}
func (f *fakeFullSender) Close() error { return nil }

func TestNavigateTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Target.getTargets":           json.RawMessage(`{"targetInfos":[{"targetId":"T1","type":"page"}]}`),
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
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Target.getTargets": json.RawMessage(`{"targetInfos":[{"targetId":"T1","type":"page"}]}`),
		},
	})
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

// Stale active target: SetActiveTarget points at "T-stale" but Target.getTargets
// returns no such id. Navigate should reject with invalid_args / "no active target".
func TestNavigateRejectsStaleActiveTarget(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Target.getTargets": json.RawMessage(`{"targetInfos":[{"targetId":"T-other","type":"page"}]}`),
		},
	})
	sess.SetActiveTarget("T-stale")
	reg := mcp.NewRegistry()
	RegisterBrowserNav(reg, sess)
	out, _ := reg.Invoke(context.Background(), "browser_navigate", json.RawMessage(`{"url":"https://x"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"invalid_args"`) {
		t.Fatalf("expected invalid_args for stale active target: %s", b)
	}
	if !strings.Contains(string(b), `no active target`) {
		t.Fatalf("expected message 'no active target': %s", b)
	}
}

// Explicit target_id that does not exist should also reject.
func TestNavigateRejectsNonexistentTargetID(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Target.getTargets": json.RawMessage(`{"targetInfos":[]}`),
		},
	})
	reg := mcp.NewRegistry()
	RegisterBrowserNav(reg, sess)
	out, _ := reg.Invoke(context.Background(), "browser_navigate", json.RawMessage(`{"url":"https://x","target_id":"T-bogus"}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"invalid_args"`) {
		t.Fatalf("expected invalid_args for nonexistent target_id: %s", b)
	}
}
