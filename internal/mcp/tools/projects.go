package tools

import (
	"context"
	"encoding/json"

	"github.com/mr-white-hat/netra-browser/internal/mcp"
	"github.com/mr-white-hat/netra-browser/internal/profile"
)

// RegisterProjects registers diagnostic tools that read the project sidecar directory.
//
// dir: where project sidecars live (e.g. ~/.config/netra-browser/projects).
// active: name of *this* bridge's project (may be empty when --project unset).
func RegisterProjects(reg *mcp.Registry, dir, active string) {
	reg.Register("browser_list_projects", func(ctx context.Context, _ json.RawMessage) (any, error) {
		projects, err := profile.ListProjects(dir)
		if err != nil {
			return mcp.ToolError{Code: "io_error", Message: err.Error()}.AsResult(), nil
		}
		out := make([]map[string]any, 0, len(projects))
		for _, p := range projects {
			out = append(out, map[string]any{
				"name":             p.Name,
				"owner_pid":        p.OwnerPID,
				"owned_target_ids": p.OwnedTargetIDs,
				"created_at":       p.CreatedAt,
				"is_self":          p.Name == active,
			})
		}
		return map[string]any{"ok": true, "projects": out, "active": active}, nil
	})
}
