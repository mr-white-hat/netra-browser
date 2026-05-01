package browser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeSender struct {
	calls   []call
	results map[string]json.RawMessage
}
type call struct {
	method  string
	session string
	params  any
}

func (f *fakeSender) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, call{method: method, params: params})
	if r, ok := f.results[method]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeSender) SendOnTarget(ctx context.Context, session, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, call{method: method, session: session, params: params})
	if r, ok := f.results[method]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeSender) AttachToTarget(ctx context.Context, targetID string) (string, error) {
	return "S-" + targetID, nil
}

func TestNewPageAttaches(t *testing.T) {
	f := &fakeSender{}
	p, err := NewPage(context.Background(), f, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if p.SessionID() != "S-T1" {
		t.Fatalf("session: %q", p.SessionID())
	}
	if p.TargetID() != "T1" {
		t.Fatalf("target: %q", p.TargetID())
	}
}

func TestPageEnableDomainsCalled(t *testing.T) {
	f := &fakeSender{}
	if _, err := NewPage(context.Background(), f, "T1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"Page.enable", "DOM.enable", "Runtime.enable", "Accessibility.enable"}
	for _, m := range want {
		found := false
		for _, c := range f.calls {
			if c.method == m && strings.HasPrefix(c.session, "S-") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s on session", m)
		}
	}
}
