package tools

import (
	"context"
	"encoding/json"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func RegisterBrowserTargets(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_list_tabs", func(ctx context.Context, params json.RawMessage) (any, error) {
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		raw, err := client.Send(ctx, "Target.getTargets", nil)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		var resp struct {
			TargetInfos []struct {
				TargetID string `json:"targetId"`
				URL      string `json:"url"`
				Title    string `json:"title"`
				Type     string `json:"type"`
				Attached bool   `json:"attached"`
			} `json:"targetInfos"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return mcp.ToolError{Code: "decode_error", Message: err.Error()}.AsResult(), nil
		}
		active := sess.ActiveTarget()
		var args struct {
			IncludeAll bool `json:"include_all"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &args)
		}
		proj := sess.Project()
		out := []map[string]any{}
		for _, ti := range resp.TargetInfos {
			if ti.Type != "page" {
				continue
			}
			owned := proj != nil && proj.Owns(ti.TargetID)
			if proj != nil && !args.IncludeAll && !owned {
				continue
			}
			row := map[string]any{
				"target_id": ti.TargetID,
				"tab_id":    sess.TabIDFor(ti.TargetID),
				"url":       ti.URL,
				"title":     ti.Title,
				"active":    ti.TargetID == active,
			}
			if proj != nil {
				row["owned"] = owned
			}
			out = append(out, row)
		}
		return map[string]any{"ok": true, "tabs": out}, nil
	})

	reg.Register("browser_new_tab", func(ctx context.Context, params json.RawMessage) (any, error) {
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		var args struct {
			URL     string `json:"url"`
			GroupID string `json:"group_id"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &args)
		}
		if args.GroupID != "" && !sess.GroupExists(args.GroupID) {
			return mcp.ToolError{Code: mcp.ErrUnknownGroup, Message: "unknown group_id: " + args.GroupID}.AsResult(), nil
		}
		if args.URL == "" {
			args.URL = "about:blank"
		}
		raw, err := client.Send(ctx, "Target.createTarget", map[string]any{"url": args.URL})
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		var resp struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(raw, &resp)
		// A grouped tab belongs to that agent's lane and becomes its active tab;
		// it does NOT touch the shared session active target. An ungrouped tab
		// keeps the legacy behavior of setting the session-wide active target.
		if args.GroupID != "" {
			sess.SetGroupActive(args.GroupID, resp.TargetID)
		} else {
			sess.SetActiveTarget(resp.TargetID)
		}
		if proj := sess.Project(); proj != nil && resp.TargetID != "" {
			_ = proj.Adopt(resp.TargetID)
		}
		out := map[string]any{
			"ok":        true,
			"target_id": resp.TargetID,
			"tab_id":    sess.TabIDFor(resp.TargetID),
		}
		if args.GroupID != "" {
			out["group_id"] = args.GroupID
		}
		return out, nil
	})

	reg.Register("browser_select_tab", func(ctx context.Context, params json.RawMessage) (any, error) {
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		var args struct {
			TargetID string `json:"target_id"`
		}
		if err := json.Unmarshal(params, &args); err != nil || args.TargetID == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "target_id required"}.AsResult(), nil
		}
		args.TargetID = sess.ResolveTabID(args.TargetID)
		_, err := client.Send(ctx, "Target.activateTarget", map[string]any{"targetId": args.TargetID})
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		sess.SetActiveTarget(args.TargetID)
		return map[string]any{"ok": true}, nil
	})

	reg.Register("browser_close_tab", func(ctx context.Context, params json.RawMessage) (any, error) {
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		var args struct {
			TargetID string `json:"target_id"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &args)
		}
		args.TargetID = sess.ResolveTabID(args.TargetID)
		if args.TargetID == "" {
			args.TargetID = sess.ActiveTarget()
		}
		if args.TargetID == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target_id and no active target"}.AsResult(), nil
		}
		_, err := client.Send(ctx, "Target.closeTarget", map[string]any{"targetId": args.TargetID})
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		if sess.ActiveTarget() == args.TargetID {
			sess.SetActiveTarget("")
		}
		if proj := sess.Project(); proj != nil {
			_ = proj.Release(args.TargetID)
		}
		// Tear down the Page (and its collector goroutines + CDP subs) so the
		// closed tab does not leak. Tabs closed by the user — not via this
		// tool — get reaped by the Target.targetDestroyed listener installed
		// in cmd/netra-browser.
		sess.DropPage(args.TargetID)
		return map[string]any{"ok": true}, nil
	})

	reg.Register("browser_adopt_tab", func(ctx context.Context, params json.RawMessage) (any, error) {
		proj := sess.Project()
		if proj == nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no project scope (start bridge with --project)"}.AsResult(), nil
		}
		var args struct {
			TargetID string `json:"target_id"`
		}
		if err := json.Unmarshal(params, &args); err != nil || args.TargetID == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "target_id required"}.AsResult(), nil
		}
		if ok, err := targetExists(ctx, sess.Client(), args.TargetID); err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		} else if !ok {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "target_id not found: " + args.TargetID}.AsResult(), nil
		}
		if err := proj.Adopt(args.TargetID); err != nil {
			return mcp.ToolError{Code: "adopt_failed", Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true}, nil
	})

	reg.Register("browser_release_tab", func(ctx context.Context, params json.RawMessage) (any, error) {
		proj := sess.Project()
		if proj == nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no project scope (start bridge with --project)"}.AsResult(), nil
		}
		var args struct {
			TargetID string `json:"target_id"`
		}
		if err := json.Unmarshal(params, &args); err != nil || args.TargetID == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "target_id required"}.AsResult(), nil
		}
		if err := proj.Release(args.TargetID); err != nil {
			return mcp.ToolError{Code: "release_failed", Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true}, nil
	})
}
