package tools

import (
	"context"
	"encoding/json"

	"github.com/pavankumar2138/netra-browser/internal/browser"
	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

func RegisterBrowserInspect(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_snapshot", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TargetID string `json:"target_id"`
			Mode     string `json:"mode"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &a)
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
		mode := browser.SnapshotMode(a.Mode)
		if mode == "" {
			mode = browser.SnapshotAccessibility
		}
		snap, err := page.Snapshot(ctx, mode)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "snapshot": snap.Nodes}, nil
	})

	reg.Register("browser_screenshot", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TargetID string           `json:"target_id"`
			Locator  *browser.Locator `json:"locator,omitempty"`
			FullPage bool             `json:"full_page"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &a)
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
		data, err := page.Screenshot(ctx, browser.ScreenshotOpts{Locator: a.Locator, FullPage: a.FullPage})
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "png_base64": data}, nil
	})
}
