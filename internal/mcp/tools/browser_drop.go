package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/browser"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

// RegisterBrowserDrop installs browser_drop_files — composite for drag-drop
// file uploads. Auto-detects hidden file input (fast path); falls back to
// CDP Input.dispatchDragEvent for pure drop zones. Optional verify arg waits
// for an upload-rendered marker before reporting success.
func RegisterBrowserDrop(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_drop_files", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Locator   browser.Locator `json:"locator"`
			FilePaths []string        `json:"file_paths"`
			TargetID  string          `json:"target_id"`
			Verify    *struct {
				Locator   *browser.Locator `json:"locator,omitempty"`
				Text      string           `json:"text,omitempty"`
				TimeoutMs int              `json:"timeout_ms,omitempty"`
			} `json:"verify,omitempty"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		if len(a.FilePaths) == 0 {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "file_paths required"}.AsResult(), nil
		}
		tid := a.TargetID
		if tid == "" {
			tid = sess.ActiveTarget()
		}
		if tid == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target"}.AsResult(), nil
		}
		page, err := sess.Page(ctx, tid)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult(), nil
		}

		mode, err := page.DropFiles(ctx, a.Locator, a.FilePaths)
		if err != nil {
			ec := mcp.ErrChromeDisconnected
			msg := err.Error()
			if strings.HasPrefix(msg, "ambiguous_locator") {
				ec = mcp.ErrAmbiguousLocator
			} else if strings.HasPrefix(msg, "not_found") {
				ec = mcp.ErrNotFound
			}
			return mcp.ToolError{Code: ec, Message: msg}.AsResult(), nil
		}
		out := map[string]any{"ok": true, "mode": string(mode)}
		if a.Verify != nil {
			timeout := time.Duration(a.Verify.TimeoutMs) * time.Millisecond
			if timeout <= 0 {
				timeout = 10 * time.Second
			}
			verified := waitForUploadMarker(ctx, page, a.Verify.Locator, a.Verify.Text, timeout)
			out["verified"] = verified
			if !verified {
				out["error"] = "verify timeout: marker not observed within " + timeout.String()
			}
		}
		return out, nil
	})
}

// waitForUploadMarker polls the page for either:
//   - an element matching `loc` becoming resolvable, OR
//   - `text` appearing in document.body.innerText
//
// Returns true on first match within `timeout`.
func waitForUploadMarker(ctx context.Context, page *browser.Page, loc *browser.Locator, text string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if loc != nil {
			if _, err := page.Resolve(ctx, *loc); err == nil {
				return true
			}
		}
		if text != "" {
			if v, err := page.Eval(ctx, "(document.body && document.body.innerText) || ''"); err == nil {
				if s, ok := v.(string); ok && strings.Contains(s, text) {
					return true
				}
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
	return false
}
