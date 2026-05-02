package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func TestScreenshotTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Page.captureScreenshot": json.RawMessage(`{"data":"iVBORw0KGgoAAA"}`),
		},
	})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserInspect(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_screenshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"png_base64":`) {
		t.Fatalf("missing png: %s", b)
	}
}

func TestSnapshotTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Accessibility.getFullAXTree": json.RawMessage(`{"nodes":[
				{"nodeId":"1","role":{"value":"WebArea"},"name":{"value":"Test"},"childIds":[],"backendDOMNodeId":1}
			]}`),
		},
	})
	sess.SetActiveTarget("T1")

	reg := mcp.NewRegistry()
	RegisterBrowserInspect(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"role":"WebArea"`) {
		t.Fatalf("missing snapshot: %s", b)
	}
}
