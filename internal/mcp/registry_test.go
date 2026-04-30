package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRegistryRegisterAndInvoke(t *testing.T) {
	reg := NewRegistry()
	reg.Register("echo", func(ctx context.Context, params json.RawMessage) (any, error) {
		return map[string]any{"got": string(params)}, nil
	})

	out, err := reg.Invoke(context.Background(), "echo", json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["got"] != `{"a":1}` {
		t.Fatalf("got %v", m)
	}
}

func TestRegistryUnknownTool(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Invoke(context.Background(), "nope", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown tool error, got %v", err)
	}
}
