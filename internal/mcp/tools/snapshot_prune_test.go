package tools

import (
	"reflect"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/browser"
)

func TestPruneEmptyContainers(t *testing.T) {
	in := []browser.SnapshotNode{
		{
			Role: "WebArea",
			Children: []browser.SnapshotNode{
				{Role: "generic", Children: []browser.SnapshotNode{
					{Role: "button", Name: "Sign in"},
				}},
				{Role: "group"}, // empty all the way down
				{Role: "heading", Name: "Welcome"},
			},
		},
	}
	out := pruneEmptyContainers(in)
	// Expect: button + heading hoisted up; empty group dropped.
	if len(out) != 2 {
		t.Fatalf("expected 2 hoisted nodes, got %d: %+v", len(out), out)
	}
	roles := []string{out[0].Role, out[1].Role}
	want := []string{"button", "heading"}
	if !reflect.DeepEqual(roles, want) {
		t.Fatalf("roles mismatch: got %v, want %v", roles, want)
	}
}

func TestPruneKeepsNamedNodes(t *testing.T) {
	in := []browser.SnapshotNode{
		{Role: "main", Name: "primary"},
	}
	out := pruneEmptyContainers(in)
	if len(out) != 1 || out[0].Name != "primary" {
		t.Fatalf("expected named node kept: %+v", out)
	}
}

func TestPruneTotallyEmptyDropped(t *testing.T) {
	in := []browser.SnapshotNode{
		{Role: "generic", Children: []browser.SnapshotNode{
			{Role: "group"},
		}},
	}
	out := pruneEmptyContainers(in)
	if len(out) != 0 {
		t.Fatalf("expected drop: %+v", out)
	}
}
