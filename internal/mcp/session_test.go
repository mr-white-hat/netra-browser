package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSessionAttachDetach(t *testing.T) {
	s := NewSession()
	if s.IsAttached() {
		t.Fatal("session should start detached")
	}
	s.SetClient(&fakeCDP{})
	if !s.IsAttached() {
		t.Fatal("expected attached after SetClient")
	}
	s.Clear()
	if s.IsAttached() {
		t.Fatal("expected detached after Clear")
	}
}

func TestSessionActiveTarget(t *testing.T) {
	s := NewSession()
	s.SetActiveTarget("T1")
	if got := s.ActiveTarget(); got != "T1" {
		t.Fatalf("got %s", got)
	}
}

type fakeCDP struct{}

func (*fakeCDP) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (*fakeCDP) Close() error { return nil }
