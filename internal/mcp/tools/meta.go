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
		client := sess.Client()
		out := map[string]any{
			"ok":           true,
			"chrome_alive": false,
			"ws_alive":     false,
			"uptime_ms":    time.Since(deps.StartedAt).Milliseconds(),
		}
		if client == nil {
			return out, nil
		}
		// Probe with Browser.getVersion — answers iff CDP is responsive.
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if _, err := client.Send(probeCtx, "Browser.getVersion", nil); err == nil {
			out["chrome_alive"] = true
			out["ws_alive"] = true
		}
		return out, nil
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
		// Install a target-lifecycle watcher when the client supports it (the
		// real *cdp.Client does; test fakes don't). Tabs the user closes in
		// Chrome's UI emit Target.targetDestroyed; this watcher reaps the
		// matching Page so its collector goroutines + ring buffer don't leak.
		if w, ok := client.(targetWatcher); ok {
			if stop, werr := w.WatchTargets(context.Background(), sess.DropPage); werr == nil {
				sess.SetClientCloser(stop)
			}
		}
		return map[string]any{
			"ok":             true,
			"chrome_version": version,
			"target_count":   targetCount,
		}, nil
	})
}

// targetWatcher is what meta_attach needs from a CDP client to wire up target
// lifecycle cleanup. *cdp.Client satisfies it; test fakes do not, so the
// reaping is a no-op under tests (which never produce real targetDestroyed
// events anyway).
type targetWatcher interface {
	WatchTargets(ctx context.Context, onDestroyed func(targetID string)) (func(), error)
}
