package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

// fakeBodyEventsSender backs a Page that already has events buffered, plus
// answers Network.getResponseBody / getRequestPostData per the test fixtures.
type fakeBodyEventsSender struct {
	calls []string
	resp  map[string]json.RawMessage
}

func (f *fakeBodyEventsSender) Send(ctx context.Context, m string, _ any) (json.RawMessage, error) {
	f.calls = append(f.calls, m)
	if r, ok := f.resp[m]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeBodyEventsSender) SendOnTarget(ctx context.Context, _, m string, _ any) (json.RawMessage, error) {
	f.calls = append(f.calls, m)
	if r, ok := f.resp[m]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}
func (f *fakeBodyEventsSender) AttachToTarget(ctx context.Context, t string) (string, error) {
	return "S-" + t, nil
}
func (f *fakeBodyEventsSender) SubscribeOnTarget(_, _ string) (<-chan cdp.BufferedEvent, func()) {
	ch := make(chan cdp.BufferedEvent, 1)
	return ch, func() {}
}
func (f *fakeBodyEventsSender) Close() error { return nil }

func TestRecentEventsIncludeBodies(t *testing.T) {
	bodyB64 := base64.StdEncoding.EncodeToString([]byte("hello world"))
	f := &fakeBodyEventsSender{resp: map[string]json.RawMessage{
		"Network.getResponseBody":    json.RawMessage(`{"body":"` + bodyB64 + `","base64Encoded":true}`),
		"Network.getRequestPostData": json.RawMessage(`{"postData":"big payload"}`),
	}}
	sess := mcp.NewSession()
	sess.SetClient(f)
	sess.SetActiveTarget("T1")
	page, err := sess.Page(context.Background(), "T1")
	if err != nil {
		t.Fatal(err)
	}
	// Inject buffered events directly.
	page.InjectEventForTest(cdp.BufferedEvent{
		Method: "Network.requestWillBeSent",
		Params: json.RawMessage(`{"requestId":"R1","request":{"hasPostData":true}}`),
	})
	page.InjectEventForTest(cdp.BufferedEvent{
		Method: "Network.responseReceived",
		Params: json.RawMessage(`{"requestId":"R1"}`),
	})

	reg := mcp.NewRegistry()
	RegisterBrowserEvents(reg, sess)
	out, err := reg.Invoke(context.Background(), "browser_get_recent_events", json.RawMessage(`{"include_bodies":true}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"body":"big payload"`) {
		t.Fatalf("missing request body: %s", b)
	}
	if !strings.Contains(string(b), `"body":"hello world"`) {
		t.Fatalf("missing response body: %s", b)
	}
}

func TestRecentEventsBodyTruncation(t *testing.T) {
	long := strings.Repeat("x", 200)
	f := &fakeBodyEventsSender{resp: map[string]json.RawMessage{
		"Network.getResponseBody": json.RawMessage(`{"body":"` + long + `"}`),
	}}
	sess := mcp.NewSession()
	sess.SetClient(f)
	sess.SetActiveTarget("T1")
	page, err := sess.Page(context.Background(), "T1")
	if err != nil {
		t.Fatal(err)
	}
	page.InjectEventForTest(cdp.BufferedEvent{
		Method: "Network.responseReceived",
		Params: json.RawMessage(`{"requestId":"R1"}`),
	})
	reg := mcp.NewRegistry()
	RegisterBrowserEvents(reg, sess)
	out, _ := reg.Invoke(context.Background(), "browser_get_recent_events", json.RawMessage(`{"include_bodies":true,"body_max_size":50}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"truncated":true`) {
		t.Fatalf("expected truncated flag: %s", b)
	}
}
