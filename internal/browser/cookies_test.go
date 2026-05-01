package browser

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGetCookiesReturnsList(t *testing.T) {
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Network.getCookies": json.RawMessage(`{"cookies":[
				{"name":"sid","value":"abc","domain":"example.com"},
				{"name":"theme","value":"dark","domain":"example.com"}
			]}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	cs, err := p.GetCookies(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d", len(cs))
	}
}

func TestSetCookiesSendsList(t *testing.T) {
	f := &fakeSender{}
	p, _ := NewPage(context.Background(), f, "T1")
	err := p.SetCookies(context.Background(), []map[string]any{
		{"name": "sid", "value": "abc", "url": "https://example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, c := range f.calls {
		if c.method == "Network.setCookies" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("Network.setCookies not sent")
	}
}
