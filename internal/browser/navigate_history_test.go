package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestReloadSendsPageReload(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, c := range f.calls {
		if c.method == "Page.reload" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("Page.reload not sent: %+v", f.calls)
	}
}

func TestGoBackSendsHistoryEntry(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Page.getNavigationHistory": json.RawMessage(`{"currentIndex":2,"entries":[{"id":10},{"id":11},{"id":12},{"id":13}]}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.GoBack(context.Background()); err != nil {
		t.Fatal(err)
	}
	sawHistory, sawNav := false, false
	for _, c := range f.calls {
		if c.method == "Page.getNavigationHistory" {
			sawHistory = true
		}
		if c.method == "Page.navigateToHistoryEntry" {
			sawNav = true
		}
	}
	if !sawHistory || !sawNav {
		t.Fatalf("expected both history+nav: %+v", f.calls)
	}
}

func TestGoBackOutOfRange(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Page.getNavigationHistory": json.RawMessage(`{"currentIndex":0,"entries":[{"id":10}]}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.GoBack(context.Background()); err == nil {
		t.Fatal("expected out-of-range error")
	}
}

func TestGoForwardSendsHistoryEntry(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Page.getNavigationHistory": json.RawMessage(`{"currentIndex":1,"entries":[{"id":10},{"id":11},{"id":12}]}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	if err := p.GoForward(context.Background()); err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, c := range f.calls {
		if c.method == "Page.navigateToHistoryEntry" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("Page.navigateToHistoryEntry not sent")
	}
}
