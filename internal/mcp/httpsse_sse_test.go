package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/browser"
	"github.com/mr-white-hat/netra-browser/internal/cdp"
)

// fakeSSESender lets a Page run with no real CDP — just enough to call NewPage,
// run startEventCollector, and accept manual fanouts via FanoutEventForTest.
type fakeSSESender struct{}

func (f *fakeSSESender) Send(ctx context.Context, m string, _ any) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (f *fakeSSESender) SendOnTarget(ctx context.Context, _, m string, _ any) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (f *fakeSSESender) AttachToTarget(ctx context.Context, t string) (string, error) {
	return "S-" + t, nil
}
func (f *fakeSSESender) SubscribeOnTarget(_, _ string) chan cdp.BufferedEvent {
	// Return a channel that never delivers — startEventCollector will idle and
	// fanoutEvent (which we drive manually in the test) will reach our subs.
	return make(chan cdp.BufferedEvent)
}
func (f *fakeSSESender) Close() error { return nil }

func TestSSEStreamsEventsToClient(t *testing.T) {
	sess := NewSession()
	sess.SetClient(&fakeSSESender{})
	page, err := sess.Page(context.Background(), "T1")
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(NewHTTPHandler(NewRegistry(), HTTPOpts{Session: sess}))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/events?target_id=T1&types=console,navigation", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("wrong content-type: %s", ct)
	}

	// Read until we see ready, then push events from the test side and observe.
	reader := bufio.NewReader(resp.Body)
	if err := waitForLine(reader, "event: ready", 2*time.Second); err != nil {
		t.Fatal(err)
	}

	// Brief settle so the per-method subscriptions are installed before fanout.
	time.Sleep(100 * time.Millisecond)
	page.FanoutEventForTest(cdp.BufferedEvent{
		Method: "Runtime.consoleAPICalled",
		Params: json.RawMessage(`{"type":"log","args":[{"value":"hello"}]}`),
	})

	if err := waitForLine(reader, "event: console", 2*time.Second); err != nil {
		t.Fatal(err)
	}
	dataLine, err := readLineWithin(reader, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dataLine, "data: ") {
		t.Fatalf("expected data line, got %q", dataLine)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(dataLine, "data: ")), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["target_id"] != "T1" {
		t.Fatalf("wrong target_id: %v", payload)
	}
}

func TestSSERejectsMissingTarget(t *testing.T) {
	sess := NewSession()
	srv := httptest.NewServer(NewHTTPHandler(NewRegistry(), HTTPOpts{Session: sess}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestSSE204WhenSessionNil(t *testing.T) {
	srv := httptest.NewServer(NewHTTPHandler(NewRegistry(), HTTPOpts{}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("expected 204 stub, got %d", resp.StatusCode)
	}
}

func TestSSETokenViaQueryParam(t *testing.T) {
	sess := NewSession()
	sess.SetClient(&fakeSSESender{})
	if _, err := sess.Page(context.Background(), "T1"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHTTPHandler(NewRegistry(), HTTPOpts{Token: "S3CRET", Session: sess}))
	defer srv.Close()

	// Bare connection without auth → 401.
	resp, _ := http.Get(srv.URL + "/events?target_id=T1")
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 without token, got %d", resp.StatusCode)
	}

	// With ?token= → 200.
	resp, err := http.Get(srv.URL + "/events?target_id=T1&token=S3CRET")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 with ?token=, got %d", resp.StatusCode)
	}
}

func TestSSEUnknownTypesRejected(t *testing.T) {
	sess := NewSession()
	sess.SetClient(&fakeSSESender{})
	if _, err := sess.Page(context.Background(), "T1"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHTTPHandler(NewRegistry(), HTTPOpts{Session: sess}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/events?target_id=T1&types=garbage")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// Compile-time sanity: import browser to keep the dependency obvious.
var _ = browser.SnapshotAccessibility

// readLineWithin reads one \n-terminated line up to deadline.
func readLineWithin(r *bufio.Reader, d time.Duration) (string, error) {
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := r.ReadString('\n')
		ch <- res{strings.TrimRight(s, "\r\n"), err}
	}()
	select {
	case got := <-ch:
		return got.s, got.err
	case <-time.After(d):
		return "", io.EOF
	}
}

// waitForLine consumes lines until one starts with prefix, or deadline hits.
func waitForLine(r *bufio.Reader, prefix string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		left := time.Until(deadline)
		if left <= 0 {
			return io.EOF
		}
		line, err := readLineWithin(r, left)
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, prefix) {
			return nil
		}
	}
	return io.EOF
}
