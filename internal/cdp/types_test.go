package cdp

import (
	"encoding/json"
	"testing"
)

func TestMethodSerializationRoundTrip(t *testing.T) {
	m := Method{ID: 42, Name: "Page.navigate", Params: map[string]any{"url": "https://example.com"}}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Method
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != 42 || got.Name != "Page.navigate" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestResponseDecode(t *testing.T) {
	raw := []byte(`{"id":7,"result":{"frameId":"abc"}}`)
	var r Response
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.ID != 7 || r.Error != nil {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestErrorResponseDecode(t *testing.T) {
	raw := []byte(`{"id":7,"error":{"code":-32000,"message":"boom"}}`)
	var r Response
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Error == nil || r.Error.Code != -32000 {
		t.Fatalf("expected error: %+v", r)
	}
}

func TestEventDecode(t *testing.T) {
	raw := []byte(`{"method":"Page.frameNavigated","params":{"frame":{"id":"abc"}},"sessionId":"S1"}`)
	var e Event
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.Method != "Page.frameNavigated" || e.SessionID != "S1" {
		t.Fatalf("unexpected: %+v", e)
	}
}
