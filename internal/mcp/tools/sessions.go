package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/browser"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
	"github.com/mr-white-hat/netra-browser/internal/profile"
)

func RegisterSessionTasks(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("task_save_session", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Name             string `json:"name"`
			SkipLocalStorage bool   `json:"skip_local_storage"`
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
		// Cookies first (always). LS optional, best-effort.
		if err := profile.SaveSession(ctx, bs, a.Name); err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		var lsOrigins []string
		if !a.SkipLocalStorage {
			ls, origins, _ := captureLocalStorage(ctx, sess, bs)
			if len(ls) > 0 {
				sf, err := profile.ReadSessionFile(a.Name)
				if err == nil {
					sf.LocalStorage = ls
					_ = profile.WriteSessionFile(a.Name, sf)
				}
				lsOrigins = origins
			}
		}
		path, _ := profile.SessionPath(a.Name)
		return map[string]any{"ok": true, "session_path": path, "local_storage_origins": lsOrigins}, nil
	})

	reg.Register("task_load_session", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Name           string `json:"name"`
			SkipNavigation bool   `json:"skip_navigation"`
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
		// Cookies first.
		if err := profile.LoadSession(ctx, bs, a.Name); err != nil {
			return mcp.ToolError{Code: mcp.ErrNotFound, Message: err.Error()}.AsResult(), nil
		}
		// LS: open a tab on each saved origin, write entries via DOMStorage.
		// skip_navigation=true → caller is responsible for having tabs already
		// open on each origin (we won't open tabs ourselves).
		sf, _ := profile.ReadSessionFile(a.Name)
		applied := applyLocalStorage(ctx, sess, bs, sf.LocalStorage, !a.SkipNavigation)
		return map[string]any{"ok": true, "local_storage_origins_applied": applied}, nil
	})
}

// captureLocalStorage walks open page-targets, attaches to each via sess.Page,
// enables DOMStorage, and reads localStorage for that target's origin.
// Returns (origin → entries) plus the list of origins that contributed.
func captureLocalStorage(ctx context.Context, sess *mcp.Session, bs profile.BrowserSender) (map[string]map[string]string, []string, error) {
	raw, err := bs.Send(ctx, "Target.getTargets", nil)
	if err != nil {
		return nil, nil, err
	}
	var resp struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			URL      string `json:"url"`
		} `json:"targetInfos"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, nil, err
	}
	out := map[string]map[string]string{}
	var origins []string
	seenOrigin := map[string]bool{}
	for _, ti := range resp.TargetInfos {
		if ti.Type != "page" {
			continue
		}
		origin := profile.OriginFromURL(ti.URL)
		if origin == "" || seenOrigin[origin] {
			continue
		}
		page, err := sess.Page(ctx, ti.TargetID)
		if err != nil {
			continue
		}
		entries, err := readPageLocalStorage(ctx, page, origin)
		if err != nil || len(entries) == 0 {
			continue
		}
		out[origin] = entries
		origins = append(origins, origin)
		seenOrigin[origin] = true
	}
	return out, origins, nil
}

func readPageLocalStorage(ctx context.Context, page *browser.Page, origin string) (map[string]string, error) {
	// DOMStorage.enable is per-target, idempotent — safe to call repeatedly.
	if _, err := page.SendCDP(ctx, "DOMStorage.enable", nil); err != nil {
		return nil, err
	}
	raw, err := page.SendCDP(ctx, "DOMStorage.getDOMStorageItems", map[string]any{
		"storageId": map[string]any{
			"securityOrigin": origin,
			"isLocalStorage": true,
		},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Entries [][]string `json:"entries"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, kv := range resp.Entries {
		if len(kv) >= 2 {
			out[kv[0]] = kv[1]
		}
	}
	return out, nil
}

// applyLocalStorage opens (or reuses) a tab on each saved origin and writes
// every (k, v) pair via DOMStorage.setDOMStorageItem on that target session.
// Returns the list of origins where application succeeded.
//
// If openIfMissing is false, only origins that already have an open tab are
// applied. Origins with no open tab are silently skipped — caller can navigate
// first and re-call task_load_session with skip_navigation=true.
func applyLocalStorage(ctx context.Context, sess *mcp.Session, bs profile.BrowserSender, ls map[string]map[string]string, openIfMissing bool) []string {
	if len(ls) == 0 {
		return nil
	}
	openOrigins, _ := openOriginIndex(ctx, bs)
	applied := []string{}
	for origin, entries := range ls {
		tid, ok := openOrigins[origin]
		if !ok {
			if !openIfMissing {
				continue
			}
			// Open a tab on this origin's root, wait for navigation to settle.
			created, err := bs.Send(ctx, "Target.createTarget", map[string]any{"url": origin + "/"})
			if err != nil {
				continue
			}
			var cresp struct {
				TargetID string `json:"targetId"`
			}
			if err := json.Unmarshal(created, &cresp); err != nil || cresp.TargetID == "" {
				continue
			}
			// Brief settle window for the frame to attach to its origin.
			time.Sleep(800 * time.Millisecond)
			tid = cresp.TargetID
		}
		page, err := sess.Page(ctx, tid)
		if err != nil {
			continue
		}
		if err := writePageLocalStorage(ctx, page, origin, entries); err != nil {
			// The page's frame likely isn't on `origin` (e.g. the origin's root
			// URL redirected to a different host). Skip — the user can fix by
			// navigating manually then re-calling with skip_navigation=true.
			continue
		}
		applied = append(applied, origin)
	}
	return applied
}

func openOriginIndex(ctx context.Context, bs profile.BrowserSender) (map[string]string, error) {
	raw, err := bs.Send(ctx, "Target.getTargets", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			URL      string `json:"url"`
		} `json:"targetInfos"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, ti := range resp.TargetInfos {
		if ti.Type != "page" {
			continue
		}
		o := profile.OriginFromURL(ti.URL)
		if o == "" {
			continue
		}
		if _, dup := out[o]; dup {
			continue
		}
		out[o] = ti.TargetID
	}
	return out, nil
}

// writePageLocalStorage writes every entry; returns nil only if at least one
// entry made it. Returns an error if every write failed (typical signal: the
// page's frame isn't on `origin` — the origin redirected elsewhere on load).
func writePageLocalStorage(ctx context.Context, page *browser.Page, origin string, entries map[string]string) error {
	if _, err := page.SendCDP(ctx, "DOMStorage.enable", nil); err != nil {
		return err
	}
	wrote := 0
	var lastErr error
	for k, v := range entries {
		if _, err := page.SendCDP(ctx, "DOMStorage.setDOMStorageItem", map[string]any{
			"storageId": map[string]any{
				"securityOrigin": origin,
				"isLocalStorage": true,
			},
			"key":   k,
			"value": v,
		}); err != nil {
			lastErr = err
		} else {
			wrote++
		}
	}
	if wrote == 0 && lastErr != nil {
		return lastErr
	}
	return nil
}
