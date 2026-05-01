package browser

import (
	"context"
	"strings"
	"testing"
)

func TestHandleDialogAccept(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.HandleDialog(context.Background(), "accept", ""); err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, c := range f.calls {
		if c.method == "Page.handleJavaScriptDialog" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("not sent")
	}
}

func TestHandleDialogDismiss(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.HandleDialog(context.Background(), "dismiss", ""); err != nil {
		t.Fatal(err)
	}
}

func TestHandleDialogInvalidAction(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	err := p.HandleDialog(context.Background(), "weird", "")
	if err == nil || !strings.Contains(err.Error(), "action") {
		t.Fatalf("expected action error, got %v", err)
	}
}
