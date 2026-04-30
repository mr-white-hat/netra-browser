package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStdioServerHandlesOneRequest(t *testing.T) {
	reg := NewRegistry()
	reg.Register("ping", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"ok": true}, nil
	})

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := Serve(ctx, in, &out, reg); err != nil && err != io.EOF {
		t.Fatalf("serve: %v", err)
	}

	resp := strings.TrimSpace(out.String())
	if !strings.Contains(resp, `"ok":true`) {
		t.Fatalf("response missing ok: %s", resp)
	}
}

func TestStdioServerReturnsRPCErrorOnUnknownTool(t *testing.T) {
	reg := NewRegistry()
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"nope"}` + "\n")
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = Serve(ctx, in, &out, reg)
	if !strings.Contains(out.String(), `"code":-32601`) {
		t.Fatalf("expected method-not-found, got %s", out.String())
	}
}
