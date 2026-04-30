package mcp

import (
	"encoding/json"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	r := Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "browser_navigate", Params: json.RawMessage(`{"url":"https://example.com"}`)}
	b, _ := json.Marshal(r)
	var got Request
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Method != "browser_navigate" {
		t.Fatalf("method: %s", got.Method)
	}
}

func TestErrorEnvelope(t *testing.T) {
	e := ToolError{Code: "chrome_disconnected", Message: "ws closed", TargetID: "T1"}
	b, _ := json.Marshal(e.AsResult())
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["ok"] != false || m["error_code"] != "chrome_disconnected" {
		t.Fatalf("envelope: %s", b)
	}
}
