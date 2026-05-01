package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestClickResolvesAndDispatches(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"DOM.pushNodesByBackendIdsToFrontend": json.RawMessage(`{"nodeIds":[42]}`),
			"DOM.getBoxModel":                     json.RawMessage(`{"model":{"content":[10,20,30,20,30,40,10,40]}}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	p.snapshot = &Snapshot{byID: map[string]*SnapshotNode{
		"#a1": {ID: "#a1", Role: "button", BackendNodeID: 1},
	}}

	if err := p.Click(context.Background(), Locator{SnapshotID: "#a1"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"DOM.pushNodesByBackendIdsToFrontend": false,
		"DOM.scrollIntoViewIfNeeded":          false,
		"DOM.getBoxModel":                     false,
		"Input.dispatchMouseEvent":            false,
	}
	for _, c := range f.calls {
		if _, ok := want[c.method]; ok {
			want[c.method] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("missing %s in calls: %+v", k, f.calls)
		}
	}
}

func TestFillResolvesAndInsertsText(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"DOM.pushNodesByBackendIdsToFrontend": json.RawMessage(`{"nodeIds":[55]}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	p.snapshot = &Snapshot{byID: map[string]*SnapshotNode{
		"#a2": {ID: "#a2", Role: "textbox", BackendNodeID: 2},
	}}

	if err := p.Fill(context.Background(), Locator{SnapshotID: "#a2"}, "hello@example.com"); err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, c := range f.calls {
		if c.method == "Input.insertText" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("Input.insertText not sent")
	}
}

func TestClickFailsWhenBackendNotInDocument(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"DOM.pushNodesByBackendIdsToFrontend": json.RawMessage(`{"nodeIds":[0]}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	p.snapshot = &Snapshot{byID: map[string]*SnapshotNode{
		"#a1": {ID: "#a1", Role: "button", BackendNodeID: 1},
	}}
	err := p.Click(context.Background(), Locator{SnapshotID: "#a1"})
	if err == nil {
		t.Fatal("expected not_found error")
	}
}
