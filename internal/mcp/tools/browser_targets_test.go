package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

type fakeCDPSender struct {
	responses map[string]json.RawMessage
	calls     []string
}

func (f *fakeCDPSender) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	if r, ok := f.responses[method]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeCDPSender) Close() error { return nil }

func TestBrowserListTabsReturnsArray(t *testing.T) {
	sess := mcp.NewSession()
	cdp := &fakeCDPSender{responses: map[string]json.RawMessage{
		"Target.getTargets": json.RawMessage(`{"targetInfos":[
			{"targetId":"T1","url":"https://a.example","title":"A","attached":true,"type":"page"},
			{"targetId":"T2","url":"https://b.example","title":"B","attached":false,"type":"page"},
			{"targetId":"X1","url":"chrome://extensions","title":"x","attached":false,"type":"background_page"}
		]}`),
	}}
	sess.SetClient(cdp)

	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	out, err := reg.Invoke(context.Background(), "browser_list_tabs", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	got := string(b)
	if !strings.Contains(got, `"target_id":"T1"`) || !strings.Contains(got, `"target_id":"T2"`) {
		t.Fatalf("missing pages: %s", got)
	}
	if strings.Contains(got, `"target_id":"X1"`) {
		t.Fatalf("non-page leaked: %s", got)
	}
}

func TestBrowserNewTabSendsCreateTarget(t *testing.T) {
	sess := mcp.NewSession()
	cdp := &fakeCDPSender{responses: map[string]json.RawMessage{
		"Target.createTarget": json.RawMessage(`{"targetId":"NEW"}`),
	}}
	sess.SetClient(cdp)
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	out, err := reg.Invoke(context.Background(), "browser_new_tab", json.RawMessage(`{"url":"about:blank"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"target_id":"NEW"`) {
		t.Fatalf("expected target_id=NEW: %s", b)
	}
	if sess.ActiveTarget() != "NEW" {
		t.Fatalf("expected active=NEW, got %s", sess.ActiveTarget())
	}
}

func TestBrowserSelectTabUpdatesSession(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeCDPSender{})
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	_, err := reg.Invoke(context.Background(), "browser_select_tab", json.RawMessage(`{"target_id":"T2"}`))
	if err != nil {
		t.Fatal(err)
	}
	if sess.ActiveTarget() != "T2" {
		t.Fatalf("got %s", sess.ActiveTarget())
	}
}

func TestBrowserCloseTabClearsActiveIfNeeded(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeCDPSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	_, err := reg.Invoke(context.Background(), "browser_close_tab", json.RawMessage(`{"target_id":"T1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if sess.ActiveTarget() != "" {
		t.Fatalf("expected active cleared, got %s", sess.ActiveTarget())
	}
}

func TestBrowserListTabsErrorWhenNotAttached(t *testing.T) {
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	RegisterBrowserTargets(reg, sess)

	out, err := reg.Invoke(context.Background(), "browser_list_tabs", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"not_attached"`) {
		t.Fatalf("expected not_attached: %s", b)
	}
}
