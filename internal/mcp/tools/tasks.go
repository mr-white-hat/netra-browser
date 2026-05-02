package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/browser"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func RegisterHighLevelTasks(reg *mcp.Registry, sess *mcp.Session) {
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

	reg.Register("task_capture_har", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			URL        string `json:"url"`
			DurationMS int    `json:"duration_ms"`
			TargetID   string `json:"target_id"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &a)
		}
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		if a.URL != "" {
			if _, err := page.Navigate(ctx, browser.NavigateOpts{URL: a.URL, WaitUntil: browser.WaitLoad}); err != nil {
				return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
			}
		}
		dur := time.Duration(a.DurationMS) * time.Millisecond
		if dur == 0 {
			dur = 5 * time.Second
		}
		path, err := page.CaptureHAR(ctx, dur)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "har_path": path}, nil
	})

	reg.Register("task_render_pdf", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			URL      string `json:"url"`
			TargetID string `json:"target_id"`
			Format   string `json:"format"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &a)
		}
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		if a.URL != "" {
			if _, err := page.Navigate(ctx, browser.NavigateOpts{URL: a.URL, WaitUntil: browser.WaitLoad}); err != nil {
				return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
			}
		}
		path, err := page.RenderPDF(ctx, browser.PDFOpts{Format: a.Format})
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrChromeDisconnected, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "pdf_path": path}, nil
	})

	reg.Register("task_wait_for_download", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			TriggerAction *struct {
				Tool string          `json:"tool"`
				Args json.RawMessage `json:"args"`
			} `json:"trigger_action"`
			SaveTo    string `json:"save_to"`
			TimeoutMS int    `json:"timeout_ms"`
			TargetID  string `json:"target_id"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		if a.SaveTo == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "save_to required"}.AsResult(), nil
		}
		page, errR := resolvePage(ctx, a.TargetID)
		if errR != nil {
			return *errR, nil
		}
		var trigger func() error
		if a.TriggerAction != nil {
			trigger = func() error {
				_, err := reg.Invoke(ctx, a.TriggerAction.Tool, a.TriggerAction.Args)
				return err
			}
		}
		timeout := time.Duration(a.TimeoutMS) * time.Millisecond
		res, err := page.WaitForDownload(ctx, trigger, a.SaveTo, timeout)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrTimeout, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true, "file_path": res.FilePath, "size": res.Size}, nil
	})

	reg.Register("task_run_with_proxy", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			ProxyURL  string `json:"proxy_url"`
			ToolCalls []struct {
				Tool string          `json:"tool"`
				Args json.RawMessage `json:"args"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		if a.ProxyURL == "" || len(a.ToolCalls) == 0 {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "proxy_url and tool_calls required"}.AsResult(), nil
		}
		// v1: stubbed. Run a separate netra-browser instance with --launch + --proxy-server in extra args.
		return mcp.ToolError{
			Code:    "not_implemented_v1",
			Message: "task_run_with_proxy is reserved; launch a separate netra-browser instance with proxy in extra args",
		}.AsResult(), nil
	})
}
