package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mr-white-hat/netra-browser/internal/browser"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func RegisterBrowserInteract(reg *mcp.Registry, sess *mcp.Session) {
	type baseArgs struct {
		Locator        browser.Locator `json:"locator"`
		TargetID       string          `json:"target_id"`
		ReturnSnapshot bool            `json:"return_snapshot"`
	}
	resolvePage := func(ctx context.Context, tid string) (*browser.Page, *map[string]any) {
		if tid == "" {
			tid = sess.ActiveTarget()
		}
		if tid == "" {
			r := mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target"}.AsResult()
			return nil, &r
		}
		page, err := sess.Page(ctx, tid)
		if err != nil {
			r := mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult()
			return nil, &r
		}
		return page, nil
	}
	wrap := func(page *browser.Page, ctx context.Context, ret bool, err error) any {
		if err != nil {
			ec := mcp.ErrChromeDisconnected
			msg := err.Error()
			if strings.HasPrefix(msg, "ambiguous_locator") {
				ec = mcp.ErrAmbiguousLocator
			} else if strings.HasPrefix(msg, "not_found") {
				ec = mcp.ErrNotFound
			}
			return mcp.ToolError{Code: ec, Message: msg}.AsResult()
		}
		out := map[string]any{"ok": true}
		if ret {
			snap, _ := page.Snapshot(ctx, browser.SnapshotAccessibility)
			if snap != nil {
				out["snapshot"] = snap.Nodes
			}
		}
		return out
	}

	reg.Register("browser_click", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a baseArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		return wrap(page, ctx, a.ReturnSnapshot, page.Click(ctx, a.Locator)), nil
	})

	type fillArgs struct {
		baseArgs
		Value string `json:"value"`
	}
	reg.Register("browser_fill", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a fillArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		return wrap(page, ctx, a.ReturnSnapshot, page.Fill(ctx, a.Locator, a.Value)), nil
	})

	reg.Register("browser_hover", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a baseArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		return wrap(page, ctx, false, page.Hover(ctx, a.Locator)), nil
	})

	type selArgs struct {
		baseArgs
		Values []string `json:"values"`
	}
	reg.Register("browser_select_option", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a selArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		return wrap(page, ctx, false, page.SelectOption(ctx, a.Locator, a.Values)), nil
	})

	type keyArgs struct {
		Key      string `json:"key"`
		TargetID string `json:"target_id"`
	}
	reg.Register("browser_press_key", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a keyArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		return wrap(page, ctx, false, page.PressKey(ctx, a.Key)), nil
	})

	type uploadArgs struct {
		baseArgs
		FilePath string `json:"file_path"`
	}
	reg.Register("browser_upload_file", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a uploadArgs
		_ = json.Unmarshal(params, &a)
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		return wrap(page, ctx, false, page.UploadFile(ctx, a.Locator, a.FilePath)), nil
	})
}
