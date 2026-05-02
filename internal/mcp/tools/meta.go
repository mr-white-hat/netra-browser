// Package tools registers the MCP tools exposed by netra-browser.
package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

// MetaDeps holds dependencies the meta_* tools may need.
// AttachFunc opens a CDP client to the running Chrome (Task 13 fills it in via main.go wiring).
type MetaDeps struct {
	StartedAt  time.Time
	AttachFunc func(ctx context.Context, debugURL string) (mcp.CDPSender, string /*chromeVersion*/, int /*targetCount*/, error)
}

// RegisterMeta registers meta_attach, meta_detach, meta_health.
func RegisterMeta(reg *mcp.Registry, sess *mcp.Session, deps MetaDeps) {
	if deps.StartedAt.IsZero() {
		deps.StartedAt = time.Now()
	}

	reg.Register("meta_health", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{
			"ok":           true,
			"chrome_alive": sess.IsAttached(),
			"ws_alive":     sess.IsAttached(),
			"uptime_ms":    time.Since(deps.StartedAt).Milliseconds(),
		}, nil
	})

	reg.Register("meta_detach", func(ctx context.Context, _ json.RawMessage) (any, error) {
		sess.Clear()
		return map[string]any{"ok": true}, nil
	})

	type attachArgs struct {
		DebugURL string `json:"debug_url"`
	}
	reg.Register("meta_attach", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a attachArgs
		if len(params) > 0 {
			if err := json.Unmarshal(params, &a); err != nil {
				return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
			}
		}
		if a.DebugURL == "" {
			a.DebugURL = "http://127.0.0.1:9222"
		}
		if deps.AttachFunc == nil {
			return mcp.ToolError{Code: "not_implemented", Message: "AttachFunc not wired"}.AsResult(), nil
		}
		client, version, targetCount, err := deps.AttachFunc(ctx, a.DebugURL)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDead, Message: err.Error()}.AsResult(), nil
		}
		sess.SetClient(client)
		return map[string]any{
			"ok":             true,
			"chrome_version": version,
			"target_count":   targetCount,
		}, nil
	})
}
