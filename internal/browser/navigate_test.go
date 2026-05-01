package browser

import (
	"context"
	"testing"
)

func TestNavigateLoadWaitUntil(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")

	res, err := p.Navigate(context.Background(), NavigateOpts{URL: "https://example.com", WaitUntil: WaitLoad})
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	saw := false
	for _, c := range f.calls {
		if c.method == "Page.navigate" && c.session == "S-T1" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("Page.navigate not sent: %+v", f.calls)
	}
	if res.URL != "https://example.com" {
		t.Fatalf("res.URL: %s", res.URL)
	}
}

func TestNavigateInvalidWaitUntilDefaults(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	_, err := p.Navigate(context.Background(), NavigateOpts{URL: "https://example.com", WaitUntil: ""})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNavigateMissingURLErrors(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	if _, err := p.Navigate(context.Background(), NavigateOpts{}); err == nil {
		t.Fatal("expected error on empty URL")
	}
}
