package browser

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestSnapshotRendersAXTree(t *testing.T) {
	raw, err := os.ReadFile("testdata/ax_simple.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Accessibility.getFullAXTree": raw,
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	snap, err := p.Snapshot(context.Background(), SnapshotAccessibility)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Nodes) != 1 {
		t.Fatalf("expected 1 root, got %d", len(snap.Nodes))
	}
	root := snap.Nodes[0]
	if root.Role != "WebArea" || len(root.Children) != 2 {
		t.Fatalf("root: %+v", root)
	}
	if root.Children[0].Role != "button" || root.Children[0].Name != "Click me" {
		t.Fatalf("button child wrong: %+v", root.Children[0])
	}
	if root.Children[1].Value != "a@b.c" {
		t.Fatalf("textbox value: %q", root.Children[1].Value)
	}
	if root.ID != "#a0" || root.Children[0].ID != "#a1" {
		t.Fatalf("ids: %s %s", root.ID, root.Children[0].ID)
	}
	if snap.byID["#a1"] == nil {
		t.Fatal("byID missing #a1")
	}
}

func TestSnapshotDOMTextMode(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Runtime.evaluate": json.RawMessage(`{"result":{"type":"string","value":"hello world"}}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	snap, err := p.Snapshot(context.Background(), SnapshotDOMText)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Nodes) != 1 || snap.Nodes[0].Value != "hello world" {
		t.Fatalf("unexpected: %+v", snap.Nodes)
	}
	if snap.byID["#t0"] == nil {
		t.Fatal("byID missing #t0")
	}
}
