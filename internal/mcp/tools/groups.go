package tools

import (
	"context"
	"encoding/json"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
)

// resolveGroupTarget is the shared, group-aware target resolver every page-level
// tool funnels through. It returns the effective target_id, or a ready-to-return
// error result (unknown_group / cross_group / no-active-tab).
func resolveGroupTarget(sess *mcp.Session, groupID, targetID string) (string, *map[string]any) {
	tid, terr := sess.ResolveActive(groupID, targetID)
	if terr != nil {
		r := terr.AsResult()
		return "", &r
	}
	return tid, nil
}

// RegisterBrowserGroups installs the per-agent tab-group lifecycle tools. Each
// concurrent Claude agent calls browser_create_group once and then passes the
// returned group_id on its page-level calls, so agents never collide on a
// shared "active tab" and cannot drive each other's tabs.
func RegisterBrowserGroups(reg *mcp.Registry, sess *mcp.Session) {
	reg.Register("browser_create_group", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			URL     string `json:"url"`
			OpenTab *bool  `json:"open_tab"` // default true
		}
		if len(params) > 0 {
			_ = json.Unmarshal(params, &a)
		}
		openTab := a.OpenTab == nil || *a.OpenTab

		gid := sess.CreateGroup()
		out := map[string]any{"ok": true, "group_id": gid}

		if !openTab {
			return out, nil
		}

		client := sess.Client()
		if client == nil {
			// Group is created but we cannot open a tab without an attach.
			out["warning"] = "group created but not attached; call meta_attach then browser_new_tab with this group_id"
			return out, nil
		}
		url := a.URL
		if url == "" {
			url = "about:blank"
		}
		raw, err := client.Send(ctx, "Target.createTarget", map[string]any{"url": url})
		if err != nil {
			out["warning"] = "group created but tab open failed: " + err.Error()
			return out, nil
		}
		var resp struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(raw, &resp)
		if resp.TargetID != "" {
			sess.SetGroupActive(gid, resp.TargetID)
			if proj := sess.Project(); proj != nil {
				_ = proj.Adopt(resp.TargetID)
			}
			out["target_id"] = resp.TargetID
			out["tab_id"] = sess.TabIDFor(resp.TargetID)
		}
		return out, nil
	})

	reg.Register("browser_list_groups", func(ctx context.Context, params json.RawMessage) (any, error) {
		groups := sess.Groups()
		rows := make([]map[string]any, 0, len(groups))
		for _, g := range groups {
			tabs := make([]map[string]any, 0, len(g.Tabs))
			for _, t := range g.Tabs {
				tabs = append(tabs, map[string]any{
					"target_id": t,
					"tab_id":    sess.TabIDFor(t),
					"active":    t == g.Active,
				})
			}
			rows = append(rows, map[string]any{
				"group_id":      g.ID,
				"active_target": g.Active,
				"tabs":          tabs,
			})
		}
		return map[string]any{"ok": true, "groups": rows}, nil
	})

	reg.Register("browser_close_group", func(ctx context.Context, params json.RawMessage) (any, error) {
		var a struct {
			GroupID   string `json:"group_id"`
			CloseTabs bool   `json:"close_tabs"` // default false: release tabs, leave them open
		}
		if err := json.Unmarshal(params, &a); err != nil || a.GroupID == "" {
			return mcp.ToolError{Code: mcp.ErrInvalidArgs, Message: "group_id required"}.AsResult(), nil
		}
		targets, ok := sess.CloseGroup(a.GroupID)
		if !ok {
			return mcp.ToolError{Code: mcp.ErrUnknownGroup, Message: "unknown group_id: " + a.GroupID}.AsResult(), nil
		}
		released := make([]string, 0, len(targets))
		closed := make([]string, 0, len(targets))
		client := sess.Client()
		proj := sess.Project()
		for _, t := range targets {
			if proj != nil {
				_ = proj.Release(t)
			}
			if a.CloseTabs && client != nil {
				if _, err := client.Send(ctx, "Target.closeTarget", map[string]any{"targetId": t}); err == nil {
					sess.DropPage(t)
					closed = append(closed, t)
					continue
				}
			}
			released = append(released, t)
		}
		return map[string]any{
			"ok":       true,
			"group_id": a.GroupID,
			"released": released,
			"closed":   closed,
		}, nil
	})
}
