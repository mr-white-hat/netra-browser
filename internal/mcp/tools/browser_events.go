package tools

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/cdp"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

func RegisterBrowserEvents(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_wait_for", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Event     string         `json:"event"`
			Predicate map[string]any `json:"predicate"`
			TimeoutMS int            `json:"timeout_ms"`
			TargetID  string         `json:"target_id"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		method := mapEventName(a.Event)
		if method == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "unknown event " + a.Event}.AsResult(), nil
		}
		tid := a.TargetID
		if tid == "" {
			tid = sess.ActiveTarget()
		}
		if tid == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target"}.AsResult(), nil
		}
		page, err := sess.Page(ctx, tid)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult(), nil
		}
		timeout := time.Duration(a.TimeoutMS) * time.Millisecond
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		predicate := buildPredicate(a.Predicate)
		ev, err := page.WaitFor(ctx, method, predicate, timeout)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrTimeout, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{
			"ok":     true,
			"event":  a.Event,
			"params": json.RawMessage(ev.Params),
		}, nil
	})

	reg.Register("browser_get_recent_events", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Since    int64    `json:"since"` // milliseconds since epoch
			Types    []string `json:"types"`
			TargetID string   `json:"target_id"`
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &a)
		}
		tid := a.TargetID
		if tid == "" {
			tid = sess.ActiveTarget()
		}
		if tid == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target"}.AsResult(), nil
		}
		page, err := sess.Page(ctx, tid)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult(), nil
		}
		var since time.Time
		if a.Since > 0 {
			since = time.UnixMilli(a.Since)
		}
		methods := make([]string, 0, len(a.Types))
		for _, t := range a.Types {
			if m := mapEventName(t); m != "" {
				methods = append(methods, m)
			}
		}
		evs := page.RecentEvents(since, methods)
		out := make([]map[string]any, 0, len(evs))
		for _, e := range evs {
			out = append(out, map[string]any{
				"event":  reverseEventName(e.Method),
				"at_ms":  e.At.UnixMilli(),
				"params": json.RawMessage(e.Params),
			})
		}
		return map[string]any{"ok": true, "events": out}, nil
	})

	reg.Register("browser_handle_dialog", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			Action   string `json:"action"`
			Text     string `json:"text"`
			TargetID string `json:"target_id"`
		}
		if err := json.Unmarshal(params, &a); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		tid := a.TargetID
		if tid == "" {
			tid = sess.ActiveTarget()
		}
		if tid == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "no target"}.AsResult(), nil
		}
		page, err := sess.Page(ctx, tid)
		if err != nil {
			return mcp.ToolError{Code: mcp.ErrNotAttached, Message: err.Error()}.AsResult(), nil
		}
		if err := page.HandleDialog(ctx, a.Action, a.Text); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		return map[string]any{"ok": true}, nil
	})
}

func mapEventName(s string) string {
	switch s {
	case "navigation":
		return "Page.frameNavigated"
	case "network_request":
		return "Network.requestWillBeSent"
	case "network_response":
		return "Network.responseReceived"
	case "console":
		return "Runtime.consoleAPICalled"
	case "dialog":
		return "Page.javascriptDialogOpening"
	case "load":
		return "Page.loadEventFired"
	case "domcontentloaded":
		return "Page.domContentEventFired"
	}
	return ""
}

func reverseEventName(m string) string {
	switch m {
	case "Page.frameNavigated":
		return "navigation"
	case "Network.requestWillBeSent":
		return "network_request"
	case "Network.responseReceived":
		return "network_response"
	case "Runtime.consoleAPICalled":
		return "console"
	case "Page.javascriptDialogOpening":
		return "dialog"
	case "Page.loadEventFired":
		return "load"
	case "Page.domContentEventFired":
		return "domcontentloaded"
	}
	return m
}

func buildPredicate(p map[string]any) func(cdp.BufferedEvent) bool {
	if len(p) == 0 {
		return nil
	}
	return func(e cdp.BufferedEvent) bool {
		var got map[string]any
		if err := json.Unmarshal(e.Params, &got); err != nil {
			return false
		}
		for k, want := range p {
			if !matchSubKey(got, k, want) {
				return false
			}
		}
		return true
	}
}

func matchSubKey(m map[string]any, dottedKey string, want any) bool {
	parts := strings.Split(dottedKey, ".")
	var cur any = m
	for _, p := range parts {
		mm, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		cur, ok = mm[p]
		if !ok {
			return false
		}
	}
	wb, _ := json.Marshal(want)
	gb, _ := json.Marshal(cur)
	return string(wb) == string(gb)
}
