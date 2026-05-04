package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func methodCalls(f *fakeSender) []string {
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.method
	}
	return out
}

func lastParamsFor(f *fakeSender, method string) any {
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].method == method {
			return f.calls[i].params
		}
	}
	return nil
}

func TestDropFiles_HiddenInputFastPath(t *testing.T) {
	tmp := t.TempDir()
	pth := filepath.Join(tmp, "payload.txt")
	if err := os.WriteFile(pth, []byte("HELLO"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &fakeSender{
		results: map[string]json.RawMessage{
			"DOM.getDocument":                     json.RawMessage(`{"root":{"nodeId":1}}`),
			"DOM.querySelector":                   json.RawMessage(`{"nodeId":42}`),
			"DOM.describeNode":                    json.RawMessage(`{"node":{"nodeName":"DIV","backendNodeId":99,"attributes":[]}}`),
			"DOM.pushNodesByBackendIdsToFrontend": json.RawMessage(`{"nodeIds":[42]}`),
		},
	}
	p, err := NewPage(context.Background(), f, "T1")
	if err != nil {
		t.Fatal(err)
	}

	mode, err := p.DropFiles(context.Background(), Locator{CSS: "#dz"}, []string{pth})
	if err != nil {
		t.Fatal(err)
	}
	if mode != DropModeHiddenInput {
		t.Fatalf("expected hidden_input mode, got %q", mode)
	}
	saw := false
	for _, c := range methodCalls(f) {
		if c == "DOM.setFileInputFiles" {
			saw = true
		}
		if c == "Input.dispatchDragEvent" {
			t.Fatal("synthetic drag fired despite hidden-input fast path")
		}
	}
	if !saw {
		t.Fatal("setFileInputFiles not called")
	}
	last := lastParamsFor(f, "DOM.setFileInputFiles").(map[string]any)
	files := last["files"].([]string)
	if len(files) != 1 || !strings.HasSuffix(files[0], "payload.txt") {
		t.Fatalf("wrong files: %v", files)
	}
}

func TestDropFiles_SyntheticDragFallback(t *testing.T) {
	tmp := t.TempDir()
	pth := filepath.Join(tmp, "payload.txt")
	if err := os.WriteFile(pth, []byte("HELLO"), 0o644); err != nil {
		t.Fatal(err)
	}

	// DOM.querySelector is called twice — once to resolve the locator (#dz → 42),
	// once inside findFileInputIn looking for "input[type='file']" (→ 0). Route
	// by selector so each call gets the right response.
	qsCalls := 0
	f := &programmableSenderForDrop{
		fakeSender: &fakeSender{
			results: map[string]json.RawMessage{
				"DOM.getDocument":                     json.RawMessage(`{"root":{"nodeId":1}}`),
				"DOM.describeNode":                    json.RawMessage(`{"node":{"nodeName":"DIV","backendNodeId":99,"attributes":[]}}`),
				"DOM.pushNodesByBackendIdsToFrontend": json.RawMessage(`{"nodeIds":[42]}`),
				"DOM.getBoxModel":                     json.RawMessage(`{"model":{"content":[10,20,310,20,310,220,10,220]}}`),
			},
		},
		querySelector: func(params any) json.RawMessage {
			qsCalls++
			p := params.(map[string]any)
			sel, _ := p["selector"].(string)
			if strings.Contains(sel, "file") {
				return json.RawMessage(`{"nodeId":0}`) // no hidden input found
			}
			return json.RawMessage(`{"nodeId":42}`) // locator resolves to drop zone
		},
	}
	p, err := NewPage(context.Background(), f, "T1")
	if err != nil {
		t.Fatal(err)
	}

	mode, err := p.DropFiles(context.Background(), Locator{CSS: "#dz"}, []string{pth})
	if err != nil {
		t.Fatal(err)
	}
	if mode != DropModeSyntheticDrag {
		t.Fatalf("expected synthetic_drag mode, got %q", mode)
	}
	dragCount := 0
	for _, c := range methodCalls(f.fakeSender) {
		if c == "Input.dispatchDragEvent" {
			dragCount++
		}
		if c == "DOM.setFileInputFiles" {
			t.Fatal("setFileInputFiles fired despite no hidden input")
		}
	}
	if dragCount != 3 {
		t.Fatalf("expected 3 dispatchDragEvent calls (enter+over+drop), got %d", dragCount)
	}
	if qsCalls < 2 {
		t.Fatalf("expected ≥2 DOM.querySelector calls (locator + file-input search), got %d", qsCalls)
	}
}

// programmableSenderForDrop wraps fakeSender to dispatch DOM.querySelector by
// selector — needed because Resolve and findFileInputIn both call it with
// different selectors and the static results map can't differentiate them.
type programmableSenderForDrop struct {
	*fakeSender
	querySelector func(params any) json.RawMessage
}

func (p *programmableSenderForDrop) Send(ctx context.Context, m string, params any) (json.RawMessage, error) {
	if m == "DOM.querySelector" && p.querySelector != nil {
		p.fakeSender.calls = append(p.fakeSender.calls, call{method: m, params: params})
		return p.querySelector(params), nil
	}
	return p.fakeSender.Send(ctx, m, params)
}
func (p *programmableSenderForDrop) SendOnTarget(ctx context.Context, sid, m string, params any) (json.RawMessage, error) {
	if m == "DOM.querySelector" && p.querySelector != nil {
		p.fakeSender.calls = append(p.fakeSender.calls, call{method: m, session: sid, params: params})
		return p.querySelector(params), nil
	}
	return p.fakeSender.SendOnTarget(ctx, sid, m, params)
}

func TestDropFiles_RejectsMissingFile(t *testing.T) {
	f := &fakeSender{}
	p, err := NewPage(context.Background(), f, "T1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.DropFiles(context.Background(), Locator{CSS: "#dz"}, []string{"/no/such/path"})
	if err == nil || !strings.Contains(err.Error(), "no/such/path") {
		t.Fatalf("expected file-missing error, got %v", err)
	}
}

func TestDropFiles_RejectsEmptyPaths(t *testing.T) {
	f := &fakeSender{}
	p, err := NewPage(context.Background(), f, "T1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.DropFiles(context.Background(), Locator{CSS: "#dz"}, nil)
	if err == nil {
		t.Fatal("expected error for empty paths")
	}
}

func TestDropFiles_LocatorIsTheFileInput(t *testing.T) {
	// Edge case: caller points the locator at the <input type="file"> directly.
	tmp := t.TempDir()
	pth := filepath.Join(tmp, "payload.txt")
	if err := os.WriteFile(pth, []byte("HELLO"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &fakeSender{
		results: map[string]json.RawMessage{
			"DOM.getDocument":   json.RawMessage(`{"root":{"nodeId":1}}`),
			"DOM.querySelector": json.RawMessage(`{"nodeId":42}`),
			// describeNode sees the resolved node IS already an input[type=file].
			"DOM.describeNode":                    json.RawMessage(`{"node":{"nodeName":"INPUT","attributes":["type","file"]}}`),
			"DOM.pushNodesByBackendIdsToFrontend": json.RawMessage(`{"nodeIds":[42]}`),
		},
	}
	p, err := NewPage(context.Background(), f, "T1")
	if err != nil {
		t.Fatal(err)
	}
	mode, err := p.DropFiles(context.Background(), Locator{CSS: "input[type='file']"}, []string{pth})
	if err != nil {
		t.Fatal(err)
	}
	if mode != DropModeHiddenInput {
		t.Fatalf("expected hidden_input mode for direct file-input locator, got %q", mode)
	}
}
