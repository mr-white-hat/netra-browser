package tools

import (
	"context"
	"encoding/json"

	"github.com/pavankumar2138/netra-browser/internal/mcp"
	"github.com/pavankumar2138/netra-browser/internal/profile"
)

func RegisterSessionTasks(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("task_save_session", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		bs, ok := client.(profile.BrowserSender)
		if !ok {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: "client lacks Send"}.AsResult(), nil
		}
		if err := profile.SaveSession(ctx, bs, a.Name); err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		path, _ := profile.SessionPath(a.Name)
		return map[string]any{"ok": true, "session_path": path}, nil
	})

	reg.Register("task_load_session", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		client := sess.Client()
		if client == nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: "call meta_attach first"}.AsResult(), nil
		}
		bs, ok := client.(profile.BrowserSender)
		if !ok {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: "client lacks Send"}.AsResult(), nil
		}
		if err := profile.LoadSession(ctx, bs, a.Name); err != nil {
			return mcp.ToolError{Code: mcp.ErrNotFound, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true}, nil
	})
}
