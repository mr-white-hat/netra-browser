package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func TestClickAtTool(t *testing.T) {
	f := &fakeFullSender{}
	sess := mcp.NewSession()
	sess.SetClient(f)
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserInteract(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_click_at", json.RawMessage(`{"x":100,"y":200}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("not ok: %s", b)
	}
	// Press + release should both fire.
	pressN := 0
	for _, c := range f.calls {
		if c == "Input.dispatchMouseEvent" {
			pressN++
		}
	}
	if pressN < 2 {
		t.Fatalf("expected >=2 mouse events, got %d", pressN)
	}
}

func TestHoverAtTool(t *testing.T) {
	f := &fakeFullSender{}
	sess := mcp.NewSession()
	sess.SetClient(f)
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserInteract(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_hover_at", json.RawMessage(`{"x":50,"y":75}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("not ok: %s", b)
	}
}

func TestDragInterpolatesMoves(t *testing.T) {
	f := &fakeFullSender{}
	sess := mcp.NewSession()
	sess.SetClient(f)
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterBrowserInteract(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_drag", json.RawMessage(`{"from":{"x":0,"y":0},"to":{"x":100,"y":100},"steps":5}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"ok":true`) {
		t.Fatalf("not ok: %s", b)
	}
	// Press + 5 moves + release = 7 mouse events.
	mouseN := 0
	for _, c := range f.calls {
		if c == "Input.dispatchMouseEvent" {
			mouseN++
		}
	}
	if mouseN != 7 {
		t.Fatalf("expected 7 mouse events for steps=5 drag, got %d", mouseN)
	}
}
