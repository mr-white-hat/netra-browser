package browser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveBySnapshotID(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	p.snapshot = &Snapshot{
		byID: map[string]*SnapshotNode{
			"#a3": {ID: "#a3", Role: "button", Name: "Submit", BackendNodeID: 999},
		},
	}
	id, err := p.Resolve(context.Background(), Locator{SnapshotID: "#a3"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 999 {
		t.Fatalf("got %d", id)
	}
}

func TestResolveByRoleName(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	p.snapshot = &Snapshot{
		Nodes: []SnapshotNode{
			{ID: "#a0", Role: "WebArea", Children: []SnapshotNode{
				{ID: "#a1", Role: "button", Name: "Sign in", BackendNodeID: 42},
				{ID: "#a2", Role: "button", Name: "Cancel", BackendNodeID: 43},
			}},
		},
	}
	id, err := p.Resolve(context.Background(), Locator{Role: "button", Name: "Sign in"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 42 {
		t.Fatalf("got %d", id)
	}
}

func TestResolveByRoleNameAmbiguous(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	p.snapshot = &Snapshot{
		Nodes: []SnapshotNode{
			{ID: "#a0", Role: "WebArea", Children: []SnapshotNode{
				{ID: "#a1", Role: "button", Name: "Submit", BackendNodeID: 1},
				{ID: "#a2", Role: "button", Name: "Submit", BackendNodeID: 2},
			}},
		},
	}
	_, err := p.Resolve(context.Background(), Locator{Role: "button", Name: "Submit"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous, got %v", err)
	}
}

func TestResolveByCSS(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"DOM.getDocument":   json.RawMessage(`{"root":{"nodeId":1}}`),
			"DOM.querySelector": json.RawMessage(`{"nodeId":77}`),
			"DOM.describeNode":  json.RawMessage(`{"node":{"backendNodeId":777}}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	id, err := p.Resolve(context.Background(), Locator{CSS: "#submit"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 777 {
		t.Fatalf("got %d", id)
	}
}

func TestResolveCSSNotFound(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"DOM.getDocument":   json.RawMessage(`{"root":{"nodeId":1}}`),
			"DOM.querySelector": json.RawMessage(`{"nodeId":0}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	_, err := p.Resolve(context.Background(), Locator{CSS: "#nope"})
	if err == nil || !strings.Contains(err.Error(), "not_found") {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestResolveEmptyLocator(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	_, err := p.Resolve(context.Background(), Locator{})
	if err == nil {
		t.Fatal("expected error on empty locator")
	}
}
