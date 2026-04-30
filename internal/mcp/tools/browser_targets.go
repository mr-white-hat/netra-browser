package tools

import (
	"context"
	"encoding/json"

	"github.com/pavankumar2138/netra-browser/internal/mcp"
)

func RegisterBrowserTargets(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_list_tabs", func(ctx context.Context, _ json.RawMessage) (any, error) {
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
		out := []map[string]any{}
		for _, ti := range resp.TargetInfos {
			if ti.Type != "page" {
				continue
			}
			out = append(out, map[string]any{
				"target_id": ti.TargetID,
				"url":       ti.URL,
				"title":     ti.Title,
				"active":    ti.TargetID == active,
			})
		}
		return map[string]any{"ok": true, "tabs": out}, nil
	})

	reg.Register("browser_new_tab", func(ctx context.Context, params json.RawMessage) (any, error) {
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		var args struct {
			URL string `json:"url"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &args)
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
		sess.SetActiveTarget(resp.TargetID)
		return map[string]any{"ok": true, "target_id": resp.TargetID}, nil
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
		return map[string]any{"ok": true}, nil
	})
}
