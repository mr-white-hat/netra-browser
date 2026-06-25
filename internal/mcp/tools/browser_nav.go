package tools

import (
	"context"
	"encoding/json"

	"github.com/mr-white-hat/netra-browser/internal/browser"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func RegisterBrowserNav(reg *mcp.Registry, sess *mcp.Session) {
	type navArgs struct {
		URL       string `json:"url"`
		TargetID  string `json:"target_id"`
		GroupID   string `json:"group_id"`
		WaitUntil string `json:"wait_until"`
	}
	reg.Register("browser_navigate", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a navArgs
		if len(params) > 0 {
			if err := json.Unmarshal(params, &a); err != nil {
				return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
			}
		}
		if a.URL == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "url required"}.AsResult(), nil
		}
		tid, errR := resolveGroupTarget(sess, a.GroupID, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		// Validate target still exists — active-target IDs go stale when a tab closes,
		// and explicit target_ids may be invented by callers.
		if ok, err := targetExists(ctx, sess.Client(), tid); err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		} else if !ok {
			if a.TargetID == "" {
				if a.GroupID != "" {
					sess.SetGroupActive(a.GroupID, "")
				} else {
					sess.SetActiveTarget("")
				}
				return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no active target"}.AsResult(), nil
			}
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "target_id not found: " + tid}.AsResult(), nil
		}
		page, err := sess.Page(ctx, tid)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult(), nil
		}
		wait := a.WaitUntil
		if wait == "" {
			wait = GetDefaults().WaitUntil
		}
		opts := browser.NavigateOpts{URL: a.URL, WaitUntil: browser.WaitUntil(wait)}
		res, err := page.Navigate(ctx, opts)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		// Always return snapshot for navigate per spec.
		snap, _ := page.Snapshot(ctx, browser.SnapshotAccessibility)
		out := map[string]any{
			"ok":    true,
			"url":   res.URL,
			"title": "",
		}
		if snap != nil {
			out["snapshot"] = snap.Nodes
		}
		return out, nil
	})

	for _, name := range []string{"browser_go_back", "browser_go_forward", "browser_reload"} {
		name := name
		reg.Register(name, func(ctx context.Context, params json.RawMessage) (any, error) {
			var a struct {
				TargetID       string `json:"target_id"`
				GroupID        string `json:"group_id"`
				ReturnSnapshot bool   `json:"return_snapshot"`
			}
			if len(params) > 0 {
				_ = json.Unmarshal(params, &a)
			}
			tid, errR := resolveGroupTarget(sess, a.GroupID, a.TargetID)
			if errR != nil {
				return *errR, nil
			}
			page, err := sess.Page(ctx, tid)
			if err != nil {
				return mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult(), nil
			}
			switch name {
			case "browser_go_back":
				err = page.GoBack(ctx)
			case "browser_go_forward":
				err = page.GoForward(ctx)
			case "browser_reload":
				err = page.Reload(ctx)
			}
			if err != nil {
				return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
			}
			out := map[string]any{"ok": true}
			if a.ReturnSnapshot {
				snap, _ := page.Snapshot(ctx, browser.SnapshotAccessibility)
				if snap != nil {
					out["snapshot"] = snap.Nodes
				}
			}
			return out, nil
		})
	}
}
