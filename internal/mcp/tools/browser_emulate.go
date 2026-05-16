package tools

import (
	"context"
	"encoding/json"

	"github.com/mr-white-hat/netra-browser/internal/browser"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

// RegisterBrowserEmulate installs the emulation tool set: viewport, device
// presets, user-agent, geolocation, and offline mode. Every tool is a thin
// wrapper over the Emulation/Network CDP domains.
func RegisterBrowserEmulate(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_set_viewport", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TargetID          string  `json:"target_id"`
			Width             int     `json:"width"`
			Height            int     `json:"height"`
			DeviceScaleFactor float64 `json:"device_scale_factor"`
			Mobile            bool    `json:"mobile"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		page, terr := pageFor(ctx, sess, a.TargetID)
		if terr != nil {
			return terr.AsResult(), nil
		}
		if err := page.SetViewport(ctx, browser.ViewportSpec{
			Width:             a.Width,
			Height:            a.Height,
			DeviceScaleFactor: a.DeviceScaleFactor,
			Mobile:            a.Mobile,
		}); err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true}, nil
	})

	reg.Register("browser_emulate_device", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TargetID string `json:"target_id"`
			Device   string `json:"device"`
		}
		if err := json.Unmarshal(params, &a); err != nil || a.Device == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "device required"}.AsResult(), nil
		}
		page, terr := pageFor(ctx, sess, a.TargetID)
		if terr != nil {
			return terr.AsResult(), nil
		}
		if err := page.EmulateDevice(ctx, a.Device); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "device": a.Device}, nil
	})

	reg.Register("browser_list_device_presets", func(ctx context.Context, _ json.RawMessage) (any, error) {
		names := make([]string, 0, len(browser.DevicePresets))
		for k := range browser.DevicePresets {
			names = append(names, k)
		}
		return map[string]any{"ok": true, "devices": names}, nil
	})

	reg.Register("browser_set_user_agent", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TargetID  string `json:"target_id"`
			UserAgent string `json:"user_agent"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		page, terr := pageFor(ctx, sess, a.TargetID)
		if terr != nil {
			return terr.AsResult(), nil
		}
		if err := page.SetUserAgent(ctx, a.UserAgent); err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true}, nil
	})

	reg.Register("browser_set_geolocation", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TargetID  string  `json:"target_id"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Accuracy  float64 `json:"accuracy"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		page, terr := pageFor(ctx, sess, a.TargetID)
		if terr != nil {
			return terr.AsResult(), nil
		}
		if err := page.SetGeolocation(ctx, browser.GeoSpec{
			Latitude:  a.Latitude,
			Longitude: a.Longitude,
			Accuracy:  a.Accuracy,
		}); err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true}, nil
	})

	reg.Register("browser_set_offline", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TargetID string `json:"target_id"`
			Offline  bool   `json:"offline"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		page, terr := pageFor(ctx, sess, a.TargetID)
		if terr != nil {
			return terr.AsResult(), nil
		}
		if err := page.SetOffline(ctx, a.Offline); err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "offline": a.Offline}, nil
	})
}

// pageFor consolidates the target-id resolution + page lookup boilerplate
// that every browser_* tool needs. Returns a typed ToolError so the caller
// can return its AsResult directly.
func pageFor(ctx context.Context, sess *mcp.Session, requestedID string) (*browser.Page, *mcp.ToolError) {
	tid := sess.ResolveTabID(requestedID)
	if tid == "" {
		tid = sess.ActiveTarget()
	}
	if tid == "" {
		return nil, &mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target"}
	}
	page, err := sess.Page(ctx, tid)
	if err != nil {
		return nil, &mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}
	}
	return page, nil
}
