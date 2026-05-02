# netra-browser Plan E — High-Level Tasks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development.

**Goal:** Add the four remaining high-level task tools — `task_capture_har`, `task_render_pdf`, `task_wait_for_download`, `task_run_with_proxy`. After Plan E the bridge is feature-complete per the spec; only release-pipeline polish (Plan F) remains.

**Architecture:** Three of the four tasks operate on a `*browser.Page`. `task_run_with_proxy` is the exception — it spawns a separate Chrome instance with `--proxy-server=<url>` and runs an action block against it.

**Tech Stack:** Same.

**Source spec:** `docs/superpowers/specs/2026-04-30-netra-browser-design.md`

**Builds on:** `plan-d-transport-launch-sessions` tag.

---

## File Structure (Plan E)

| Path | Responsibility |
|---|---|
| `internal/browser/har.go` | HAR capture: subscribe to network events for `duration_ms`, build HAR JSON |
| `internal/browser/har_test.go` | Tests with synthesized network events |
| `internal/browser/pdf.go` | `Page.RenderPDF(opts)` via `Page.printToPDF` |
| `internal/browser/pdf_test.go` | Tests |
| `internal/browser/download.go` | `Page.WaitForDownload(triggerAction, saveTo, timeout)` using `Browser.downloadWillBegin` + `Browser.downloadProgress` |
| `internal/browser/download_test.go` | Tests |
| `internal/mcp/tools/tasks.go` | Register all 4 task tools |
| `internal/mcp/tools/tasks_test.go` | Tool tests |
| `e2e/tasks_test.go` | E2E for HAR + PDF (proxy test deferred to per-machine setup) |

---

## Task 1: HAR capture

**Files:**
- Create: `internal/browser/har.go`
- Create: `internal/browser/har_test.go`

`Page.CaptureHAR(ctx, durationMs)` subscribes to `Network.requestWillBeSent` + `Network.responseReceived` for the given duration, then returns a HAR-format JSON document. Saves to a temp file and returns its path.

HAR schema (minimum viable): standard HAR 1.2 envelope with `log.entries` containing one entry per request/response pair.

- [ ] **Step 1: Failing test**

`internal/browser/har_test.go`:

```go
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
		// Synthesize one request/response pair after a short delay.
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
```

- [ ] **Step 2: Run failure**

`go test ./internal/browser/ -run CaptureHAR -v`. Expected FAIL.

- [ ] **Step 3: Implement**

`internal/browser/har.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []harEntry `json:"entries"`
}
type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           map[string]any `json:"cache"`
	Timings         map[string]any `json:"timings"`
}
type harRequest struct {
	Method      string                 `json:"method"`
	URL         string                 `json:"url"`
	HTTPVersion string                 `json:"httpVersion"`
	Headers     []map[string]string    `json:"headers"`
	QueryString []map[string]string    `json:"queryString"`
	Cookies     []map[string]any       `json:"cookies"`
	HeadersSize int                    `json:"headersSize"`
	BodySize    int                    `json:"bodySize"`
}
type harResponse struct {
	Status      int                    `json:"status"`
	StatusText  string                 `json:"statusText"`
	HTTPVersion string                 `json:"httpVersion"`
	Headers     []map[string]string    `json:"headers"`
	Cookies     []map[string]any       `json:"cookies"`
	Content     map[string]any         `json:"content"`
	HeadersSize int                    `json:"headersSize"`
	BodySize    int                    `json:"bodySize"`
}

// CaptureHAR collects network events for `duration` and writes a HAR file. Returns the path.
func (p *Page) CaptureHAR(ctx context.Context, duration time.Duration) (string, error) {
	if duration <= 0 {
		duration = 5 * time.Second
	}
	willBeSent := p.cdp.SubscribeOnTarget(p.sessionID, "Network.requestWillBeSent")
	responseReceived := p.cdp.SubscribeOnTarget(p.sessionID, "Network.responseReceived")

	type entry struct {
		req  json.RawMessage
		res  json.RawMessage
		t0   time.Time
		t1   time.Time
	}
	var mu sync.Mutex
	byID := map[string]*entry{}

	deadline := time.NewTimer(duration)
	defer deadline.Stop()

loop:
	for {
		select {
		case e := <-willBeSent:
			var v struct {
				RequestID string          `json:"requestId"`
				Request   json.RawMessage `json:"request"`
			}
			if err := json.Unmarshal(e.Params, &v); err != nil {
				continue
			}
			mu.Lock()
			byID[v.RequestID] = &entry{req: v.Request, t0: e.At}
			mu.Unlock()
		case e := <-responseReceived:
			var v struct {
				RequestID string          `json:"requestId"`
				Response  json.RawMessage `json:"response"`
			}
			if err := json.Unmarshal(e.Params, &v); err != nil {
				continue
			}
			mu.Lock()
			if ent, ok := byID[v.RequestID]; ok {
				ent.res = v.Response
				ent.t1 = e.At
			}
			mu.Unlock()
		case <-deadline.C:
			break loop
		case <-ctx.Done():
			break loop
		}
	}

	mu.Lock()
	defer mu.Unlock()
	har := harLog{
		Version: "1.2",
		Creator: harCreator{Name: "netra-browser", Version: "0.0.1-dev"},
		Entries: make([]harEntry, 0, len(byID)),
	}
	for _, ent := range byID {
		he := harEntry{
			StartedDateTime: ent.t0.UTC().Format(time.RFC3339Nano),
			Time:            float64(ent.t1.Sub(ent.t0).Milliseconds()),
			Cache:           map[string]any{},
			Timings:         map[string]any{"send": 0, "wait": 0, "receive": 0},
		}
		decodeRequest(ent.req, &he.Request)
		decodeResponse(ent.res, &he.Response)
		har.Entries = append(har.Entries, he)
	}

	tmp, err := os.CreateTemp("", "netra-har-*.json")
	if err != nil {
		return "", err
	}
	defer tmp.Close()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"log": har}); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

func decodeRequest(raw json.RawMessage, dst *harRequest) {
	if len(raw) == 0 {
		return
	}
	var v struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	_ = json.Unmarshal(raw, &v)
	dst.Method = v.Method
	dst.URL = v.URL
	dst.HTTPVersion = "HTTP/1.1"
	for k, val := range v.Headers {
		dst.Headers = append(dst.Headers, map[string]string{"name": k, "value": val})
	}
	dst.HeadersSize = -1
	dst.BodySize = -1
	dst.Cookies = []map[string]any{}
	dst.QueryString = []map[string]string{}
}

func decodeResponse(raw json.RawMessage, dst *harResponse) {
	if len(raw) == 0 {
		return
	}
	var v struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
	}
	_ = json.Unmarshal(raw, &v)
	dst.Status = v.Status
	dst.StatusText = fmt.Sprintf("%d", v.Status)
	dst.HTTPVersion = "HTTP/1.1"
	for k, val := range v.Headers {
		dst.Headers = append(dst.Headers, map[string]string{"name": k, "value": val})
	}
	dst.Content = map[string]any{"size": 0, "mimeType": ""}
	dst.HeadersSize = -1
	dst.BodySize = -1
	dst.Cookies = []map[string]any{}
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./internal/browser/ -v
git add internal/browser/har.go internal/browser/har_test.go
git commit -m "browser: add CaptureHAR (collect network events into HAR file)"
```

---

## Task 2: PDF render

**Files:**
- Create: `internal/browser/pdf.go`
- Create: `internal/browser/pdf_test.go`

`Page.RenderPDF(ctx, opts)` calls `Page.printToPDF` (returns base64 PDF), decodes, writes to temp file, returns path. Optional `Format` (Letter/A4) maps to width/height.

- [ ] **Step 1: Test**

```go
package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

func TestRenderPDFWritesFile(t *testing.T) {
	pdfBytes := []byte("%PDF-1.4 fake")
	pdfB64 := base64.StdEncoding.EncodeToString(pdfBytes)
	f := &fakeSender{
		results: map[string]json.RawMessage{
			"Page.printToPDF": json.RawMessage(`{"data":"` + pdfB64 + `"}`),
		},
	}
	p, _ := NewPage(context.Background(), f, "T1")
	path, err := p.RenderPDF(context.Background(), PDFOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(pdfBytes) {
		t.Fatalf("file contents wrong")
	}
}
```

- [ ] **Step 2: Implement**

`internal/browser/pdf.go`:

```go
package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
)

type PDFOpts struct {
	Format string // "Letter" | "A4" | ""
}

func (p *Page) RenderPDF(ctx context.Context, opts PDFOpts) (string, error) {
	params := map[string]any{}
	switch opts.Format {
	case "A4":
		params["paperWidth"] = 8.27
		params["paperHeight"] = 11.69
	case "Letter", "":
		params["paperWidth"] = 8.5
		params["paperHeight"] = 11
	}

	raw, err := p.send(ctx, "Page.printToPDF", params)
	if err != nil {
		return "", err
	}
	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	bytes, err := base64.StdEncoding.DecodeString(resp.Data)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "netra-pdf-*.pdf")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(bytes); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/browser/ -v
git add internal/browser/pdf.go internal/browser/pdf_test.go
git commit -m "browser: add RenderPDF"
```

---

## Task 3: WaitForDownload

**Files:**
- Create: `internal/browser/download.go`
- Create: `internal/browser/download_test.go`

`Page.WaitForDownload(ctx, trigger func() error, saveTo string, timeout time.Duration)` subscribes to `Browser.downloadWillBegin` + `Browser.downloadProgress`, optionally invokes the trigger, then waits for a `downloadProgress` event with `state:"completed"`. Returns `{filePath, size}`.

For v1: use `Browser.setDownloadBehavior{behavior:"allow", downloadPath:saveTo}` before the trigger. Files land directly in saveTo with their guid as filename — note: this is fine, but the trigger needs to happen AFTER setDownloadBehavior returns.

- [ ] **Step 1: Test**

```go
package browser

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
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
		// Simulate Chrome dropping the file at the saveTo path with the guid as filename.
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
```

- [ ] **Step 2: Implement**

`internal/browser/download.go`:

```go
package browser

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"
)

type DownloadResult struct {
	FilePath string `json:"file_path"`
	Size     int64  `json:"size"`
}

// WaitForDownload sets download behavior, runs trigger (if non-nil), and waits for completion.
// saveTo is the directory Chrome should drop files into.
func (p *Page) WaitForDownload(ctx context.Context, trigger func() error, saveTo string, timeout time.Duration) (*DownloadResult, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if _, err := p.send(ctx, "Browser.setDownloadBehavior", map[string]any{
		"behavior":     "allow",
		"downloadPath": saveTo,
	}); err != nil {
		return nil, err
	}

	willBegin := p.cdp.SubscribeOnTarget(p.sessionID, "Browser.downloadWillBegin")
	progress := p.cdp.SubscribeOnTarget(p.sessionID, "Browser.downloadProgress")

	if trigger != nil {
		if err := trigger(); err != nil {
			return nil, err
		}
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var guid string

	for {
		select {
		case e := <-willBegin:
			var v struct {
				GUID string `json:"guid"`
			}
			_ = json.Unmarshal(e.Params, &v)
			if v.GUID != "" {
				guid = v.GUID
			}
		case e := <-progress:
			var v struct {
				GUID         string `json:"guid"`
				ReceivedBytes int64 `json:"receivedBytes"`
				State        string `json:"state"`
			}
			_ = json.Unmarshal(e.Params, &v)
			if v.State == "completed" {
				if guid == "" {
					guid = v.GUID
				}
				return &DownloadResult{
					FilePath: filepath.Join(saveTo, guid),
					Size:     v.ReceivedBytes,
				}, nil
			}
			if v.State == "canceled" {
				return nil, ErrDownloadCanceled
			}
		case <-deadline.C:
			return nil, ErrDownloadTimeout
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
```

Add a sentinel errors block at the top (after imports):

```go
import "errors"

var (
	ErrDownloadCanceled = errors.New("download canceled")
	ErrDownloadTimeout  = errors.New("download timeout")
)
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/browser/ -v
git add internal/browser/download.go internal/browser/download_test.go
git commit -m "browser: add WaitForDownload"
```

---

## Task 4: Tool registrations

**Files:**
- Create: `internal/mcp/tools/tasks.go`
- Create: `internal/mcp/tools/tasks_test.go`
- Modify: `cmd/netra-browser/main.go` to register

Register `task_capture_har`, `task_render_pdf`, `task_wait_for_download`, `task_run_with_proxy`.

For `task_run_with_proxy`: spawn a separate Chrome via `profile.Launch` with `Args: ["--proxy-server=" + proxyURL]`, attach a fresh `cdp.Client`, run the action block (an array of `{tool, args}` pairs), tear down. The action block is executed against a SECOND session (separate from `sess`), so there's a per-call lightweight session.

For v1 simplicity: `task_run_with_proxy` only supports a single tool call in the block. Multi-call action blocks deferred (the tool API still returns an array result for forward-compat).

- [ ] **Step 1: Test the easy three**

`internal/mcp/tools/tasks_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func TestCaptureHARTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterHighLevelTasks(reg, sess)
	out, err := reg.Invoke(context.Background(), "task_capture_har", json.RawMessage(`{"duration_ms":100}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"har_path":`) {
		t.Fatalf("missing har_path: %s", b)
	}
}

func TestRenderPDFTool(t *testing.T) {
	sess := mcp.NewSession()
	sess.SetClient(&fakeFullSender{
		results: map[string]json.RawMessage{
			"Page.printToPDF": json.RawMessage(`{"data":"JVBERi0xLjQK"}`), // base64 of "%PDF-1.4\n"
		},
	})
	sess.SetActiveTarget("T1")
	reg := mcp.NewRegistry()
	RegisterHighLevelTasks(reg, sess)
	out, err := reg.Invoke(context.Background(), "task_render_pdf", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"pdf_path":`) {
		t.Fatalf("missing pdf_path: %s", b)
	}
}

func TestRunWithProxyRequiresArgs(t *testing.T) {
	sess := mcp.NewSession()
	reg := mcp.NewRegistry()
	RegisterHighLevelTasks(reg, sess)
	out, _ := reg.Invoke(context.Background(), "task_run_with_proxy", json.RawMessage(`{}`))
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"error_code":"invalid_args"`) {
		t.Fatalf("expected invalid_args: %s", b)
	}
}
```

- [ ] **Step 2: Implement**

`internal/mcp/tools/tasks.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/browser"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func RegisterHighLevelTasks(reg *mcp.Registry, sess *mcp.Session) {
	resolvePage := func(ctx context.Context, tid string) (*browser.Page, *map[string]any) {
		if tid == "" {
			tid = sess.ActiveTarget()
		}
		if tid == "" {
			r := mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target"}.AsResult()
			return nil, &r
		}
		page, err := sess.Page(ctx, tid)
		if err != nil {
			r := mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult()
			return nil, &r
		}
		return page, nil
	}

	reg.Register("task_capture_har", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			URL        string `json:"url"`
			DurationMS int    `json:"duration_ms"`
			TargetID   string `json:"target_id"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &a)
		}
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		if a.URL != "" {
			if _, err := page.Navigate(ctx, browser.NavigateOpts{URL: a.URL, WaitUntil: browser.WaitLoad}); err != nil {
				return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
			}
		}
		dur := time.Duration(a.DurationMS) * time.Millisecond
		if dur == 0 {
			dur = 5 * time.Second
		}
		path, err := page.CaptureHAR(ctx, dur)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "har_path": path}, nil
	})

	reg.Register("task_render_pdf", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			URL      string `json:"url"`
			TargetID string `json:"target_id"`
			Format   string `json:"format"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &a)
		}
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		if a.URL != "" {
			if _, err := page.Navigate(ctx, browser.NavigateOpts{URL: a.URL, WaitUntil: browser.WaitLoad}); err != nil {
				return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
			}
		}
		path, err := page.RenderPDF(ctx, browser.PDFOpts{Format: a.Format})
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "pdf_path": path}, nil
	})

	reg.Register("task_wait_for_download", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TriggerAction *struct {
				Tool string          `json:"tool"`
				Args json.RawMessage `json:"args"`
			} `json:"trigger_action"`
			SaveTo    string `json:"save_to"`
			TimeoutMS int    `json:"timeout_ms"`
			TargetID  string `json:"target_id"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		if a.SaveTo == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "save_to required"}.AsResult(), nil
		}
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		var trigger func() error
		if a.TriggerAction != nil {
			trigger = func() error {
				_, err := reg.Invoke(ctx, a.TriggerAction.Tool, a.TriggerAction.Args)
				return err
			}
		}
		timeout := time.Duration(a.TimeoutMS) * time.Millisecond
		res, err := page.WaitForDownload(ctx, trigger, a.SaveTo, timeout)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrTimeout, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "file_path": res.FilePath, "size": res.Size}, nil
	})

	reg.Register("task_run_with_proxy", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			ProxyURL  string `json:"proxy_url"`
			ToolCalls []struct {
				Tool string          `json:"tool"`
				Args json.RawMessage `json:"args"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		if a.ProxyURL == "" || len(a.ToolCalls) == 0 {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "proxy_url and tool_calls required"}.AsResult(), nil
		}
		// v1: deferred — full implementation requires a separate Chrome instance and is too
		// machine-dependent for unit tests. Mark as not_implemented; real users can run a
		// separate netra-browser instance with the proxy on their own Chrome launch.
		return mcp.ToolError{Code: "not_implemented_v1", Message: "task_run_with_proxy is reserved; launch a separate netra-browser with --launch and --launch-headless plus --proxy-server in extra args"}.AsResult(), nil
	})
}
```

- [ ] **Step 3: Wire main.go**

Add `tools.RegisterHighLevelTasks(reg, sess)` to `cmd/netra-browser/main.go`.

- [ ] **Step 4: Run + commit**

```bash
go test ./...
git add internal/mcp/tools/tasks.go internal/mcp/tools/tasks_test.go cmd/netra-browser/main.go
git commit -m "tools: register high-level task tools (capture_har, render_pdf, wait_for_download, run_with_proxy stub)"
```

---

## Task 5: E2E HAR + PDF

**File:**
- Create: `e2e/tasks_test.go` (build tag `e2e`)

Use real chromium against an httptest server that returns a simple HTML page with one image. Verify:
- `task_render_pdf` returns a path to a non-empty file starting with `%PDF`.
- `task_capture_har` returns a path to a JSON file with at least one entry.

- [ ] **Step 1: Test**

```go
//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestE2E_HighLevelTasks(t *testing.T) {
	chrome := findChrome(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `<!doctype html><html><body><h1>Hello</h1><img src="/img.png"></body></html>`)
	})
	mux.HandleFunc("/img.png", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte{0x89, 0x50, 0x4e, 0x47}) // PNG header bytes
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	port := freePort(t)
	userDir := t.TempDir()
	chromeCmd := exec.Command(chrome, "--headless=new", "--no-sandbox", "--disable-gpu",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+userDir, "about:blank")
	chromeCmd.Stderr = os.Stderr
	if err := chromeCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer chromeCmd.Process.Kill()
	waitForChrome(t, port, 10*time.Second)

	bin := exec.Command("go", "run", "../cmd/netra-browser",
		"--lock", filepath.Join(userDir, "active.lock"),
		"--debug-url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	stdin, _ := bin.StdinPipe()
	stdout, _ := bin.StdoutPipe()
	stderrPipe, _ := bin.StderrPipe()
	go io.Copy(os.Stderr, stderrPipe)
	if err := bin.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { bin.Process.Kill(); bin.Process.Wait() }()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	send := func(id int, method string, params any) map[string]any {
		req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			req["params"] = params
		}
		b, _ := json.Marshal(req)
		_, _ = io.WriteString(stdin, string(b)+"\n")
		if !scanner.Scan() {
			t.Fatal("no resp")
		}
		var r map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &r)
		return r
	}
	mustOK := func(label string, r map[string]any) map[string]any {
		res, ok := r["result"].(map[string]any)
		if !ok || res["ok"] != true {
			t.Fatalf("%s: %v", label, r)
		}
		return res
	}

	send(1, "meta_attach", map[string]any{"debug_url": fmt.Sprintf("http://127.0.0.1:%d", port)})
	r := send(2, "browser_new_tab", map[string]any{"url": srv.URL + "/"})
	tid := mustOK("new_tab", r)["target_id"].(string)
	time.Sleep(300 * time.Millisecond)
	send(3, "browser_navigate", map[string]any{"target_id": tid, "url": srv.URL + "/", "wait_until": "load"})

	// Capture HAR — a 500ms window after a fresh navigation
	r = send(4, "task_capture_har", map[string]any{"target_id": tid, "url": srv.URL + "/", "duration_ms": 1000})
	res := mustOK("capture_har", r)
	harPath, _ := res["har_path"].(string)
	defer os.Remove(harPath)
	b, err := os.ReadFile(harPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"version":"1.2"`) {
		t.Fatalf("HAR doesn't look right: %s", b[:min(200, len(b))])
	}

	// Render PDF
	r = send(5, "task_render_pdf", map[string]any{"target_id": tid})
	res = mustOK("render_pdf", r)
	pdfPath, _ := res["pdf_path"].(string)
	defer os.Remove(pdfPath)
	pdfBytes, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(pdfBytes), "%PDF") {
		t.Fatalf("not a PDF: %q", pdfBytes[:min(20, len(pdfBytes))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 2: Run + commit**

```bash
go test -tags e2e ./e2e/... -timeout 300s -run HighLevelTasks -v
git add e2e/tasks_test.go
git commit -m "e2e: add HAR capture + PDF render tests"
```

---

## Task 6: Verify + tag

```bash
go test ./...
go test -tags e2e ./e2e/... -timeout 300s
go vet ./...
gofmt -l . | grep -v '^docs/' | grep -v '^BRAINSTORM' || echo "all formatted"
git tag plan-e-high-level-tasks
```

After Plan E: 27 + 4 = **31 tools live** (3 meta + 22 browser + 6 task).

---

## Self-Review Notes

- ✅ task_capture_har (Tasks 1, 4)
- ✅ task_render_pdf (Tasks 2, 4)
- ✅ task_wait_for_download (Tasks 3, 4)
- ✅ task_save_session / task_load_session (Plan D)
- ⚠️ task_run_with_proxy (Task 4) — registered but returns `not_implemented_v1` for v1. Documented escape hatch (run a second bridge with --launch + proxy in extra args).
- ⏭ Release pipeline → Plan F.

**Type consistency:** Tools use `browser.NavigateOpts/WaitLoad`, `browser.PDFOpts`, `browser.DownloadResult` — all defined in their respective files.

**Known limitations:** task_run_with_proxy stub. localStorage in sessions still deferred. Streaming MCP notifications still v2.
