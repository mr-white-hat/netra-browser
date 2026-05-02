package browser

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
)

func TestCaptureHARProducesValidJSON(t *testing.T) {
	p := newPumpable()
	for _, m := range collectedEvents {
		p.events[m] = make(chan cdp.BufferedEvent, 16)
	}
	page, _ := NewPage(context.Background(), p, "T1")

	go func() {
		time.Sleep(50 * time.Millisecond)
		p.events["Network.requestWillBeSent"] <- cdp.BufferedEvent{
			Method: "Network.requestWillBeSent",
			Params: json.RawMessage(`{"requestId":"R1","request":{"url":"https://example.com/x","method":"GET","headers":{}},"timestamp":1234.0}`),
			At:     time.Now(),
		}
		time.Sleep(20 * time.Millisecond)
		p.events["Network.responseReceived"] <- cdp.BufferedEvent{
			Method: "Network.responseReceived",
			Params: json.RawMessage(`{"requestId":"R1","response":{"url":"https://example.com/x","status":200,"headers":{}},"timestamp":1234.5}`),
			At:     time.Now(),
		}
	}()

	path, err := page.CaptureHAR(context.Background(), 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var har map[string]any
	if err := json.Unmarshal(b, &har); err != nil {
		t.Fatalf("invalid HAR JSON: %v", err)
	}
	log, ok := har["log"].(map[string]any)
	if !ok {
		t.Fatal("missing log")
	}
	entries, _ := log["entries"].([]any)
	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}
}
