package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/browser"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

// RegisterBrowserDiagnose installs browser_diagnose, the composite "is anything wrong?"
// tool. Bundles the chain agents previously had to send as 5+ round-trips
// (health → list_tabs → screenshot → snapshot → recent_events) into one call.
func RegisterBrowserDiagnose(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_diagnose", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TargetID             string `json:"target_id"`
			RecentEventsWindowMs int64  `json:"recent_events_window_ms"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &a)
		}
		windowMs := a.RecentEventsWindowMs
		if windowMs <= 0 {
			windowMs = 5000
		}

		out := map[string]any{
			"ok":            true,
			"chrome_alive":  false,
			"ws_alive":      false,
			"target_exists": false,
		}

		client := sess.Client()
		if client == nil {
			out["error"] = "not attached"
			return out, nil
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if _, err := client.Send(probeCtx, "Browser.getVersion", nil); err == nil {
			out["chrome_alive"] = true
			out["ws_alive"] = true
		} else {
			out["error"] = err.Error()
			return out, nil
		}

		tid := a.TargetID
		if tid == "" {
			tid = sess.ActiveTarget()
		}
		if tid == "" {
			out["error"] = "no target"
			return out, nil
		}
		exists, err := targetExists(ctx, client, tid)
		if err != nil {
			out["error"] = err.Error()
			return out, nil
		}
		out["target_exists"] = exists
		out["target_id"] = tid
		if !exists {
			return out, nil
		}

		page, err := sess.Page(ctx, tid)
		if err != nil {
			out["error"] = err.Error()
			return out, nil
		}

		// Best-effort: each sub-call may fail independently — we still return what
		// we can so agents always get a partial diagnostic.
		if shot, err := page.Screenshot(ctx, browser.ScreenshotOpts{}); err == nil {
			out["screenshot_png_base64"] = shot
		}
		if snap, err := page.Snapshot(ctx, browser.SnapshotAccessibility); err == nil {
			out["snapshot"] = snap.Nodes
		}
		since := time.Now().Add(-time.Duration(windowMs) * time.Millisecond)
		evs := page.RecentEvents(since, nil)
		recent := make([]map[string]any, 0, len(evs))
		for _, e := range evs {
			recent = append(recent, map[string]any{
				"event":  reverseEventName(e.Method),
				"at_ms":  e.At.UnixMilli(),
				"params": json.RawMessage(e.Params),
			})
		}
		out["recent_events"] = recent
		return out, nil
	})
}
