package tools

import (
	"context"
	"encoding/json"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

// RegisterBrowserVitals installs browser_get_vitals: Core Web Vitals
// (LCP, CLS, FCP, TTFB, INP) collected via PerformanceObserver. The first
// call on a page installs the observer; subsequent calls just read state.
// Pass wait_ms to give observers time to record (e.g. 1500 after navigation).
func RegisterBrowserVitals(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_get_vitals", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TargetID string `json:"target_id"`
			WaitMS   int    `json:"wait_ms"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &a)
		}
		page, terr := pageFor(ctx, sess, a.TargetID)
		if terr != nil {
			return terr.AsResult(), nil
		}
		v, err := page.GetVitals(ctx, a.WaitMS)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "vitals": v}, nil
	})
}
