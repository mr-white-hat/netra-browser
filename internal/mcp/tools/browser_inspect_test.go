package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

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
