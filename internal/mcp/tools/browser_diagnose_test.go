package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func TestDiagnoseDetached(t *testing.T) {
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	RegisterBrowserDiagnose(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_diagnose", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"chrome_alive":false`) {
		t.Fatalf("expected chrome_alive false: %s", b)
	}
}

func TestDiagnoseLiveTargetExists(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Browser.getVersion":          json.RawMessage(`{"product":"Chrome/123"}`),
			"Target.getTargets":           json.RawMessage(`{"targetInfos":[{"targetId":"T1","type":"page"}]}`),
			"Page.captureScreenshot":      json.RawMessage(`{"data":"BASE64"}`),
			"Accessibility.getFullAXTree": json.RawMessage(`{"nodes":[]}`),
		},
	})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserDiagnose(reg, sess)
	out, _ := reg.Invoke(context.Background(), "browser_diagnose", nil)
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"chrome_alive":true`) {
		t.Fatalf("expected alive: %s", b)
	}
	if !strings.Contains(string(b), `"target_exists":true`) {
		t.Fatalf("expected target_exists: %s", b)
	}
	if !strings.Contains(string(b), `"recent_events":`) {
		t.Fatalf("missing recent_events: %s", b)
	}
}
