package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

func TestClickTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"DOM.getDocument":                     json.RawMessage(`{"root":{"nodeId":1}}`),
			"DOM.querySelector":                   json.RawMessage(`{"nodeId":99}`),
			"DOM.describeNode":                    json.RawMessage(`{"node":{"backendNodeId":999}}`),
			"DOM.pushNodesByBackendIdsToFrontend": json.RawMessage(`{"nodeIds":[42]}`),
			"DOM.getBoxModel":                     json.RawMessage(`{"model":{"content":[10,20,30,20,30,40,10,40]}}`),
		},
	})
	sess.SetActiveTarget("T1")

	reg := mcp.NewRegistry()
	RegisterBrowserInteract(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_click", json.RawMessage(`{"locator":{"css":"#submit"}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("expected ok: %s", b)
	}
}

func TestFillTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"DOM.getDocument":                     json.RawMessage(`{"root":{"nodeId":1}}`),
			"DOM.querySelector":                   json.RawMessage(`{"nodeId":99}`),
			"DOM.describeNode":                    json.RawMessage(`{"node":{"backendNodeId":999}}`),
			"DOM.pushNodesByBackendIdsToFrontend": json.RawMessage(`{"nodeIds":[42]}`),
		},
	})
	sess.SetActiveTarget("T1")

	reg := mcp.NewRegistry()
	RegisterBrowserInteract(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_fill", json.RawMessage(`{"locator":{"css":"#email"},"value":"a@b.c"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("expected ok: %s", b)
	}
}

func TestPressKeyTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserInteract(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_press_key", json.RawMessage(`{"key":"Enter"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("expected ok: %s", b)
	}
}
