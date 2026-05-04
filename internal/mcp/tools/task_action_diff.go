package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/mr-white-hat/netra-browser/internal/browser"
	"github.com/mr-white-hat/netra-browser/internal/cdp"
	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

// RegisterTaskActionDiff installs task_action_diff: snapshot state before, run an
// action via the registry, snapshot state after, return the diff.
//
// Removes the "what just happened?" reasoning turn agents otherwise need
// after each significant action.
func RegisterTaskActionDiff(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("task_action_diff", func(ctx context.Context, params json.RawMessage) (any, error) {
		var args struct {
			Action struct {
				Tool string          `json:"tool"`
				Args json.RawMessage `json:"args"`
			} `json:"action"`
			TargetID string   `json:"target_id"`
			Capture  []string `json:"capture"`
		}
		if err := json.Unmarshal(params, &args); err != nil {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: err.Error()}.AsResult(), nil
		}
		if args.Action.Tool == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "action.tool required"}.AsResult(), nil
		}
		if len(args.Capture) == 0 {
			args.Capture = []string{"url", "cookies", "console", "network", "dom_summary"}
		}
		captureSet := map[string]bool{}
		for _, c := range args.Capture {
			captureSet[c] = true
		}

		tid := args.TargetID
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

		startTime := time.Now()
		before := captureState(ctx, page, captureSet)

		// Execute the action by re-invoking the registry.
		actionParams := args.Action.Args
		if len(actionParams) == 0 {
			actionParams = json.RawMessage("{}")
		}
		actionResult, err := reg.Invoke(ctx, args.Action.Tool, actionParams)
		if err != nil {
			return mcp.ToolError{Code: "action_failed", Message: err.Error()}.AsResult(), nil
		}

		// Brief settle window so post-action navigation/network has a chance to land.
		time.Sleep(50 * time.Millisecond)
		after := captureState(ctx, page, captureSet)

		out := map[string]any{
			"ok":            true,
			"action_result": actionResult,
		}
		if captureSet["url"] {
			out["url_before"] = before.URL
			out["url_after"] = after.URL
			out["url_changed"] = before.URL != after.URL
		}
		if captureSet["cookies"] {
			added, removed := diffCookies(before.Cookies, after.Cookies)
			out["new_cookies"] = added
			out["removed_cookies"] = removed
		}
		if captureSet["console"] {
			out["new_console_messages"] = newSinceTime(after.RecentEvents, "Runtime.consoleAPICalled", startTime)
		}
		if captureSet["network"] {
			out["new_network_requests"] = newSinceTime(after.RecentEvents, "Network.requestWillBeSent", startTime)
		}
		if captureSet["dom_summary"] {
			out["dom_summary_changed"] = before.DOMSummary != after.DOMSummary
			out["dom_summary_before"] = before.DOMSummary
			out["dom_summary_after"] = after.DOMSummary
		}
		return out, nil
	})
}

type stateSnapshot struct {
	URL          string
	Cookies      []map[string]any
	RecentEvents []cdp.BufferedEvent
	DOMSummary   string
}

func captureState(ctx context.Context, page *browser.Page, want map[string]bool) stateSnapshot {
	var s stateSnapshot
	if want["url"] || want["dom_summary"] {
		if v, err := page.Eval(ctx, "location.href"); err == nil {
			if str, ok := v.(string); ok {
				s.URL = str
			}
		}
	}
	if want["cookies"] {
		if cs, err := page.GetCookies(ctx, nil); err == nil {
			for _, c := range cs {
				s.Cookies = append(s.Cookies, c)
			}
		}
	}
	if want["console"] || want["network"] {
		s.RecentEvents = page.RecentEvents(time.Time{}, nil)
	}
	if want["dom_summary"] {
		s.DOMSummary = domSummaryHash(ctx, page)
	}
	return s
}

func domSummaryHash(ctx context.Context, page *browser.Page) string {
	snap, err := page.Snapshot(ctx, browser.SnapshotAccessibility)
	if err != nil || snap == nil {
		return ""
	}
	h := sha256.New()
	walkSnapshot(snap.Nodes, h)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func walkSnapshot(nodes []browser.SnapshotNode, h interface {
	Write(p []byte) (int, error)
}) {
	for _, n := range nodes {
		_, _ = h.Write([]byte(n.Role))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(n.Name))
		_, _ = h.Write([]byte{0})
		walkSnapshot(n.Children, h)
	}
}

func diffCookies(before, after []map[string]any) (added, removed []map[string]any) {
	idx := func(c map[string]any) string {
		dom, _ := c["domain"].(string)
		name, _ := c["name"].(string)
		return dom + "|" + name
	}
	beforeSet := map[string]map[string]any{}
	for _, c := range before {
		beforeSet[idx(c)] = c
	}
	afterSet := map[string]map[string]any{}
	for _, c := range after {
		afterSet[idx(c)] = c
	}
	for k, v := range afterSet {
		if _, ok := beforeSet[k]; !ok {
			added = append(added, v)
		}
	}
	for k, v := range beforeSet {
		if _, ok := afterSet[k]; !ok {
			removed = append(removed, v)
		}
	}
	sort.Slice(added, func(i, j int) bool { return idx(added[i]) < idx(added[j]) })
	sort.Slice(removed, func(i, j int) bool { return idx(removed[i]) < idx(removed[j]) })
	return added, removed
}

func newSinceTime(events []cdp.BufferedEvent, method string, since time.Time) []map[string]any {
	out := []map[string]any{}
	for _, e := range events {
		if e.Method != method || e.At.Before(since) {
			continue
		}
		out = append(out, map[string]any{
			"event":  reverseEventName(e.Method),
			"at_ms":  e.At.UnixMilli(),
			"params": json.RawMessage(e.Params),
		})
	}
	return out
}
