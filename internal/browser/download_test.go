package browser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/pavankumar2138/netra-browser/internal/cdp"
)

func TestWaitForDownloadCompletes(t *testing.T) {
	p := newPumpable()
	for _, m := range collectedEvents {
		p.events[m] = make(chan cdp.BufferedEvent, 16)
	}
	p.events["Browser.downloadWillBegin"] = make(chan cdp.BufferedEvent, 4)
	p.events["Browser.downloadProgress"] = make(chan cdp.BufferedEvent, 4)
	page, _ := NewPage(context.Background(), p, "T1")

	tmp := t.TempDir()
	go func() {
		time.Sleep(80 * time.Millisecond)
		p.events["Browser.downloadWillBegin"] <- cdp.BufferedEvent{
			Method: "Browser.downloadWillBegin",
			Params: json.RawMessage(`{"guid":"G1","suggestedFilename":"hello.txt","url":"https://example.com/x"}`),
		}
		time.Sleep(50 * time.Millisecond)
		p.events["Browser.downloadProgress"] <- cdp.BufferedEvent{
			Method: "Browser.downloadProgress",
			Params: json.RawMessage(`{"guid":"G1","totalBytes":100,"receivedBytes":100,"state":"completed"}`),
		}
		_ = os.WriteFile(tmp+"/G1", []byte("x"), 0o644)
	}()

	res, err := page.WaitForDownload(context.Background(), nil, tmp, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.FilePath == "" {
		t.Fatal("empty file path")
	}
	if res.Size != 100 {
		t.Fatalf("size: %d", res.Size)
	}
}

func TestWaitForDownloadTimeout(t *testing.T) {
	p := newPumpable()
	for _, m := range collectedEvents {
		p.events[m] = make(chan cdp.BufferedEvent, 16)
	}
	p.events["Browser.downloadWillBegin"] = make(chan cdp.BufferedEvent, 4)
	p.events["Browser.downloadProgress"] = make(chan cdp.BufferedEvent, 4)
	page, _ := NewPage(context.Background(), p, "T1")

	_, err := page.WaitForDownload(context.Background(), nil, t.TempDir(), 100*time.Millisecond)
	if err == nil || !errors.Is(err, ErrDownloadTimeout) {
		t.Fatalf("expected ErrDownloadTimeout, got %v", err)
	}
}
